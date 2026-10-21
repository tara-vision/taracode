package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeRunDir is a run directory as an eval makes one, with a workdir copy and a symlink that points outside.
func probeRunDir(t *testing.T) (*Replay, string) {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "taracode-eval-123")
	if err := os.MkdirAll(filepath.Join(runDir, "workdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"workdir/main.tf": "resource {}\n", "workdir/notes.txt": "hello\n"} {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc", filepath.Join(runDir, "out")); err != nil {
		t.Fatal(err)
	}
	store, err := LoadFixtures(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewReplay(store, runDir), runDir
}

func probe(t *testing.T, r *Replay, runDir string, argv ...string) (string, error) {
	t.Helper()
	argvs, ok := r.probeArgvs(runDir, [][]string{argv})
	if !ok {
		t.Fatalf("%v is not taken as a probe", argv)
	}
	return r.probeChain(context.Background(), runDir, argvs)
}

func refused(t *testing.T, r *Replay, runDir string, argv ...string) {
	t.Helper()
	if _, ok := r.probeArgvs(runDir, [][]string{argv}); ok {
		t.Fatalf("%v was taken as a probe", argv)
	}
}

func TestProbePwdAnswersTheWorkingDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out, err := probe(t, r, runDir, "pwd"); err != nil || strings.TrimSpace(out) != runDir {
		t.Fatalf("pwd: %q %v", out, err)
	}
}

func TestProbeLsListsTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	out, err := probe(t, r, runDir, "ls", "-la")
	if err != nil || !strings.Contains(out, "workdir") || !strings.Contains(out, "out") {
		t.Fatalf("ls: %q %v", out, err)
	}
}

func TestProbeCatReadsAWorkdirFile(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out, err := probe(t, r, runDir, "cat", "workdir/notes.txt"); err != nil || out != "hello\n" {
		t.Fatalf("cat: %q %v", out, err)
	}
	if out, err := probe(t, r, runDir, "cat", filepath.Join(runDir, "workdir", "notes.txt")); err != nil || out != "hello\n" {
		t.Fatalf("cat of the absolute path: %q %v", out, err)
	}
}

func TestProbeRefusesAPathOutsideTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "cat", "/etc/hosts")
	refused(t, r, runDir, "ls", "/")
	refused(t, r, runDir, "ls", filepath.Dir(runDir))
}

func TestProbeRefusesASymlinkOutOfTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "ls", "out")
	refused(t, r, runDir, "cat", "out/hosts")
}

func TestProbeRefusesDotDotAndTildeAndDollar(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "cat", "../x")
	refused(t, r, runDir, "ls", "~")
	refused(t, r, runDir, "cat", "$HOME/x")
	refused(t, r, runDir, "ls", "workdir/../../x")
}

func TestProbeRefusesAProgramOffTheList(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "uname", "-a")
	refused(t, r, runDir, "git", "status")
	refused(t, r, runDir, "env")
	refused(t, r, runDir, "rm", "workdir/notes.txt")
}

func TestProbeRefusesFindWithExecOrDelete(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "find", ".", "-delete")
	refused(t, r, runDir, "find", ".", "-exec", "rm", "{}", ";")
	refused(t, r, runDir, "find", "/etc", "-name", "hosts")
	if out, err := probe(t, r, runDir, "find", ".", "-name", "*.tf"); err != nil || !strings.Contains(out, "main.tf") {
		t.Fatalf("find: %q %v", out, err)
	}
}

func TestProbeRefusesTailFollow(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "tail", "-f", "workdir/notes.txt")
	refused(t, r, runDir, "tail", "--follow=name", "workdir/notes.txt")
	if out, err := probe(t, r, runDir, "tail", "-n", "1", "workdir/notes.txt"); err != nil || out != "hello\n" {
		t.Fatalf("tail: %q %v", out, err)
	}
}

func TestProbeExpandsAGlobInsideTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out, err := probe(t, r, runDir, "ls", "workdir/*.tf"); err != nil || strings.TrimSpace(out) != "workdir/main.tf" {
		t.Fatalf("a glob: %q %v", out, err)
	}
	if _, err := probe(t, r, runDir, "ls", "workdir/*.none"); err == nil {
		t.Fatal("a glob with no match keeps the word, and ls says it does not exist")
	}
}

func TestSplitChainSplitsOnAndAndSemicolon(t *testing.T) {
	parts, ok := splitChain("pwd && ls -la; echo hi")
	if !ok || len(parts) != 3 || parts[1][1] != "-la" || parts[2][1] != "hi" {
		t.Fatalf("parts %v %v", parts, ok)
	}
}

func TestSplitChainRefusesPipesOrAndRedirects(t *testing.T) {
	for _, line := range []string{"ls | wc -l", "ls || true", "ls > out", "ls &", "echo $(id)", "f() { ls; }", "cat <<EOF\nx\nEOF", ""} {
		if _, ok := splitChain(line); ok {
			t.Fatalf("%q was split as a chain of probes", line)
		}
	}
}

func TestProbeChainStopsAtTheFirstFailure(t *testing.T) {
	r, runDir := probeRunDir(t)
	argvs, ok := r.probeArgvs(runDir, [][]string{{"cat", "missing"}, {"echo", "after"}})
	if !ok {
		t.Fatal("not a chain of probes")
	}
	out, err := r.probeChain(context.Background(), runDir, argvs)
	if err == nil || strings.Contains(out, "after") {
		t.Fatalf("the chain ran on: %q %v", out, err)
	}
	argvs, _ = r.probeArgvs(runDir, [][]string{{"echo", "one"}, {"echo", "two"}})
	if out, err := r.probeChain(context.Background(), runDir, argvs); err != nil || out != "one\ntwo\n" {
		t.Fatalf("a chain of two: %q %v", out, err)
	}
}

func TestProbeChainWithAnOffListPartIsNotAProbe(t *testing.T) {
	r, runDir := probeRunDir(t)
	if _, ok := r.probeArgvs(runDir, [][]string{{"pwd"}, {"uname", "-a"}}); ok {
		t.Fatal("a chain with a part off the list is not a probe")
	}
}

func TestProbeOutputIsCapped(t *testing.T) {
	r, runDir := probeRunDir(t)
	big := strings.Repeat("0123456789abcdef", 8192) // 128 KiB
	if err := os.WriteFile(filepath.Join(runDir, "workdir", "big"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := probe(t, r, runDir, "cat", "workdir/big")
	if err != nil || len(out) > shellProbeOutputLimit+len(shellProbeTruncated) || !strings.HasSuffix(out, shellProbeTruncated) {
		t.Fatalf("len %d, suffix %q, %v", len(out), out[max(0, len(out)-20):], err)
	}
}
