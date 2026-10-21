package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSplitChainHonorsEscapes(t *testing.T) {
	parts, ops, ok := splitChain(`echo "a\"b" && echo c`)
	if !ok || len(parts) != 2 || len(ops) != 1 || parts[0][1] != `a"b` {
		t.Fatalf("an escaped quote inside quotes: %q %v %v", parts, ops, ok)
	}
	parts, ops, ok = splitChain(`echo a\;b && echo c`)
	if !ok || len(parts) != 2 || len(ops) != 1 || parts[0][1] != "a;b" {
		t.Fatalf("an escaped semicolon is a character: %q %v %v", parts, ops, ok)
	}
	// the parser splits such lines; the probe then refuses them (TestProbeEchoTakesPlainWordsOnly)
}

func TestProbeTakesADoubleDash(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, "cat -- workdir/notes.txt"); out != "hello" {
		t.Fatalf("cat --: %q", out)
	}
	refused(t, r, runDir, "cat -- /etc/hosts")
}

func TestProbeRefusesWhenTheRunDirectoryIsGone(t *testing.T) {
	store, err := LoadFixtures(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	r := NewReplay(store, nil, gone)
	refused(t, r, gone, "ls workdir")
	if err := probeErr(t, r, gone, "ls"); err == nil || !strings.Contains(err.Error(), "ls") { // starts nowhere: an error
		t.Fatalf("a probe that cannot start: %v", err)
	}
	if err := probeErr(t, r, gone, "pwd && ls"); err == nil || !strings.Contains(err.Error(), "ls") {
		t.Fatalf("the same inside a chain: %v", err)
	}
}

func TestProbeRefusesASymlinkLoop(t *testing.T) {
	r, runDir := probeRunDir(t)
	if err := os.Symlink("loop", filepath.Join(runDir, "loop")); err != nil {
		t.Fatal(err)
	}
	refused(t, r, runDir, "cat loop/x")
}

// probeErr runs one line as the replay does and returns the error the probe reported; the line must be taken.
func probeErr(t *testing.T, r *Replay, runDir, line string) error {
	t.Helper()
	parts, ops, ok := splitChain(line)
	if !ok {
		t.Fatalf("%q does not split", line)
	}
	_, rc, handled := r.probeLine(context.Background(), runDir, line, "shell "+line, parts, ops)
	if !handled {
		t.Fatalf("%q is not taken as a probe", line)
	}
	return rc.Err
}

func TestProbeTimesOut(t *testing.T) {
	r, runDir := probeRunDir(t)
	if err := syscall.Mkfifo(filepath.Join(runDir, "workdir", "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := shellProbeTimeout
	shellProbeTimeout = 200 * time.Millisecond
	t.Cleanup(func() { shellProbeTimeout = old })
	err := probeErr(t, r, runDir, "cat workdir/fifo") // blocks until a writer opens the pipe: none does
	if err == nil || !strings.Contains(err.Error(), "command timed out after") {
		t.Fatalf("err %v", err)
	}
}

func TestProbeReportsAProgramThatCannotStart(t *testing.T) {
	r, runDir := probeRunDir(t)
	t.Setenv("PATH", "")
	parts, ops, _ := splitChain("pwd && ls")
	out, rc, handled := r.probeLine(context.Background(), runDir, "pwd && ls", "shell pwd && ls", parts, ops)
	if !handled || rc.Err != nil || !strings.HasSuffix(out, "ls: command not found\n[exit status 127]") {
		t.Fatalf("in a chain: handled %v err %v out %q", handled, rc.Err, out)
	}
}

func TestProbeChainWithoutACommonStoreLeavesAFixturePartAlone(t *testing.T) {
	r, runDir := probeRunDir(t)
	if !shellReads("pwd && which ls", runDir) {
		t.Fatal("the classifier calls the line a read")
	}
	refused(t, r, runDir, "pwd && which ls")
}

func TestCappedBufferCutsALongWrite(t *testing.T) {
	b := &cappedBuffer{limit: 4}
	if n, err := b.Write([]byte("hello")); n != 5 || err != nil {
		t.Fatal(n, err)
	}
	if b.String() != "hell"+shellProbeTruncated {
		t.Fatalf("%q", b.String())
	}
}
