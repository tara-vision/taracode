package evals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// waitForFile polls for a file a probe writes.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			return strings.TrimSpace(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was never written", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gone reports whether the process pid has ended, waiting a little for a kill to land.
func gone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// TestProbeKillsTheWholeProcessGroup: a probe that is stopped takes its children with it.
func TestProbeKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := probeGPU(ctx, `sleep 30 & echo $! > "$PIDFILE"; wait`,
			[]string{"PATH=" + os.Getenv("PATH"), "PIDFILE=" + pidFile})
		done <- err
	}()
	pid, err := strconv.Atoi(waitForFile(t, pidFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a stopped probe must be an error")
	}
	if !gone(pid) {
		t.Fatalf("the probe's child %d outlived the probe", pid)
	}
}

// TestProbeDropsStderr: only stdout is read; what the probe writes to stderr never reaches the parser.
func TestProbeDropsStderr(t *testing.T) {
	got, err := probeGPU(context.Background(), "echo /secret/path/on/the/host >&2; echo 100",
		[]string{"PATH=" + os.Getenv("PATH")})
	if err != nil || got != 100 {
		t.Fatalf("probeGPU = %d, %v; want 100", got, err)
	}
}

// TestProbeKeepsItsFigureWhenAChildHoldsTheOutputOpen: a probe that exits cleanly but leaves a
// background job holding its output still measured; the job is not left running, and it cannot hold
// the run.
func TestProbeKeepsItsFigureWhenAChildHoldsTheOutputOpen(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	start := time.Now()
	got, err := probeGPU(context.Background(), `sleep 30 & echo $! > "$PIDFILE"; echo 100`,
		[]string{"PATH=" + os.Getenv("PATH"), "PIDFILE=" + pidFile})
	pid, convErr := strconv.Atoi(waitForFile(t, pidFile))
	if convErr != nil {
		t.Fatal(convErr)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err != nil || got != 100 {
		t.Fatalf("probeGPU = %d, %v; want 100", got, err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the probe held the run for %s", elapsed)
	}
	if !gone(pid) {
		t.Fatalf("the probe's background job %d was left running", pid)
	}
}

// TestRunProbesAfterTheWarmUpAndBeforeTheFirstTask: the measurement is of the loaded model, so the
// probe runs once the warm-up's chat request has been answered and before any task's.
func TestRunProbesAfterTheWarmUpAndBeforeTheFirstTask(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	marker := filepath.Join(t.TempDir(), "probed")
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var mu sync.Mutex
	var probedAtChat []bool
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			_, statErr := os.Stat(marker)
			mu.Lock()
			probedAtChat = append(probedAtChat, statErr == nil)
			mu.Unlock()
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	opts := runOptions(srv, "")
	opts.Host = front.URL
	opts.GPUProbe = `: > "` + marker + `"; echo 23676`
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if res.GPUMemoryMiB != 23676 || len(probedAtChat) < 2 || probedAtChat[0] || !probedAtChat[1] {
		t.Fatalf("measured %d; probe done at each chat request: %v (want false, then true)", res.GPUMemoryMiB, probedAtChat)
	}
}

// TestRunProbesOnce: one measurement per run, whatever the number of tasks.
func TestRunProbesOnce(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := loadedGemma(t)
	counter := filepath.Join(t.TempDir(), "count")
	opts := runOptions(srv, "")
	opts.GPUProbe = `echo x >> "` + counter + `"; echo 23676`
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	if got := waitForFile(t, counter); got != "x" {
		t.Fatalf("the probe ran %d times", strings.Count(got, "x"))
	}
}
