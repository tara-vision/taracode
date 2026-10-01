package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/ui"
)

// isolateHome points HOME at a fresh directory, so a test neither reads nor writes the developer's
// ~/.taracode (the global policy, the upgrade state, the readline history).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// captureStderrForTest runs fn with os.Stderr replaced by a pipe and returns everything it wrote.
func captureStderrForTest(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()

	func() {
		defer func() { os.Stderr = original }() // also when fn fails the test
		fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// captureOutput runs fn and returns what it wrote to stdout and to stderr.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	stdout = captureStdoutForTest(t, func() { stderr = captureStderrForTest(t, fn) })
	return stdout, stderr
}

// withStdin runs fn with os.Stdin replaced by a pipe that holds input: the y/N confirmations the
// handlers read with fmt.Scanln.
func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = original
		_ = reader.Close()
	}()
	fn()
}

// fakeOllama starts the scripted fake serving gemma4:12b with tool support.
func fakeOllama(t *testing.T, turns ...ollamatest.Turn) *ollamatest.Server {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"},
		ContextLength: 32768, Family: "gemma4", ParameterSize: "12B"}}
	srv.Turns = turns
	return srv
}

// replOn builds a repl the way testREPL does, on the given fake, with HOME isolated. An initialised
// repl (ephemeral false) gets its storage in dir/.taracode; enableProject is left to the test.
// configure adjusts the options before the assistant is built.
func replOn(t *testing.T, srv *ollamatest.Server, dir string, ephemeral bool, configure ...func(*agent.Options)) *repl {
	t.Helper()
	isolateHome(t)
	opts := agent.DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Ephemeral, opts.Spinner = srv.URL, "gemma4:12b", dir, ephemeral, false
	for _, c := range configure {
		c(&opts)
	}
	var asst *agent.Assistant
	var err error
	_ = captureStdoutForTest(t, func() { asst, err = agent.New(opts) })
	if err != nil {
		t.Fatal(err)
	}
	return &repl{asst: asst, opts: opts, renderer: ui.NewRenderer(), projectRoot: dir, absDir: dir, initialised: !ephemeral}
}

// projectREPL is an initialised repl on a fresh fake with its project managers (history, memory)
// enabled, the state /init leaves behind.
func projectREPL(t *testing.T, turns ...ollamatest.Turn) (*repl, *ollamatest.Server) {
	t.Helper()
	srv := fakeOllama(t, turns...)
	r := replOn(t, srv, t.TempDir(), false)
	_ = captureStdoutForTest(t, r.enableProject)
	if r.history == nil || r.memory == nil {
		t.Fatal("enableProject left a manager nil")
	}
	return r, srv
}

// newMemoryManager is a memory manager in a fresh .taracode directory.
func newMemoryManager(t *testing.T) *memory.Manager {
	t.Helper()
	mm, err := memory.NewManager(filepath.Join(t.TempDir(), ".taracode"))
	if err != nil {
		t.Fatal(err)
	}
	return mm
}

// skipIfRoot skips a test that needs a permission error: root writes whatever the mode says.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}
