package upgrade

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// The fake command is the test binary itself, run with fakeCommandEnv set: it prints its command
// line and fails when the line contains one of fakeFailEnv's comma-separated parts, or prints
// nothing when it contains one of fakeSilentEnv's.
const (
	fakeCommandEnv = "taracode_upgrade_fake_command"
	fakeFailEnv    = "taracode_upgrade_fake_fail"
	fakeSilentEnv  = "taracode_upgrade_fake_silent"
)

// TestFakeCommandProcess is not a test: as a child with fakeCommandEnv set it stands in for brew,
// go, sudo or a downloaded binary.
func TestFakeCommandProcess(_ *testing.T) {
	if os.Getenv(fakeCommandEnv) == "" {
		return
	}
	var line []string
	for i, a := range os.Args {
		if a == "--" {
			line = os.Args[i+1:]
			break
		}
	}
	joined := strings.Join(line, " ")
	if !matchesAny(joined, os.Getenv(fakeSilentEnv)) {
		fmt.Printf("fake %s\n", joined)
	}
	if matchesAny(joined, os.Getenv(fakeFailEnv)) {
		os.Exit(3)
	}
	os.Exit(0)
}

func matchesAny(line, parts string) bool {
	for _, p := range strings.Split(parts, ",") {
		if p != "" && strings.Contains(line, p) {
			return true
		}
	}
	return false
}

// commandLog is what fakeCommands saw.
type commandLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *commandLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// fakeCommands routes execCommand to the fake for the rest of the test. fail and silent are the
// fakeFailEnv and fakeSilentEnv values. The settings travel in the environment, which the go
// install path copies from os.Environ, so they are set on this process.
func fakeCommands(t *testing.T, fail, silent string) *commandLog {
	t.Helper()
	t.Setenv(fakeCommandEnv, "1")
	t.Setenv(fakeFailEnv, fail)
	t.Setenv(fakeSilentEnv, silent)
	t.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
	log := &commandLog{}
	original := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		log.mu.Lock()
		log.lines = append(log.lines, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		log.mu.Unlock()
		return exec.Command(os.Args[0], append([]string{"-test.run=^TestFakeCommandProcess$", "--", name}, args...)...)
	}
	t.Cleanup(func() { execCommand = original })
	return log
}

// fakeLookPath makes lookPath find (or not find) every program for the rest of the test.
func fakeLookPath(t *testing.T, found bool) {
	t.Helper()
	original := lookPath
	lookPath = func(file string) (string, error) {
		if found {
			return "/opt/fake/bin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = original })
}

// fakeExecutable makes executable report path (or err) for the rest of the test.
func fakeExecutable(t *testing.T, path string, err error) {
	t.Helper()
	original := executable
	executable = func() (string, error) { return path, err }
	t.Cleanup(func() { executable = original })
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// clientTo is a client that sends every request to handler, whatever host it names; the handler
// sees the original host in r.Host.
func clientTo(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		out := r.Clone(r.Context())
		out.Host = r.URL.Host
		out.URL.Scheme, out.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(out)
	})}
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns everything it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()
	func() {
		defer func() { os.Stdout = original }() // also when fn fails the test
		fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}
