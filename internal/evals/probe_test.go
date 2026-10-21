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
	return NewReplay(store, nil, runDir), runDir
}

// probe runs one line through probeLine as the replay does and returns what the model would read.
func probe(t *testing.T, r *Replay, runDir, line string) string {
	t.Helper()
	parts, ops, ok := splitChain(line)
	if !ok {
		t.Fatalf("%q does not split", line)
	}
	out, rc, handled := r.probeLine(context.Background(), runDir, line, "shell "+line, parts, ops)
	if !handled {
		t.Fatalf("%q is not taken as a probe", line)
	}
	if rc.Err != nil {
		t.Fatalf("%q: %v", line, rc.Err)
	}
	return out
}

// refused asserts that a line is left to the fixture store.
func refused(t *testing.T, r *Replay, runDir, line string) {
	t.Helper()
	parts, ops, ok := splitChain(line)
	if !ok {
		return // not even a chain: refused before any check
	}
	if _, _, handled := r.probeLine(context.Background(), runDir, line, "shell "+line, parts, ops); handled {
		t.Fatalf("%q was run as a probe", line)
	}
}

func TestProbePwdAnswersTheWorkingDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, "pwd"); out != runDir {
		t.Fatalf("pwd: %q", out)
	}
}

func TestProbeLsListsTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	out := probe(t, r, runDir, "ls -la")
	if !strings.Contains(out, "workdir") || !strings.Contains(out, "out") {
		t.Fatalf("ls: %q", out)
	}
	if out := probe(t, r, runDir, "ls --color=never workdir"); !strings.Contains(out, "main.tf") {
		t.Fatalf("ls with a long flag: %q", out)
	}
}

func TestProbeCatReadsAWorkdirFile(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, "cat workdir/notes.txt"); out != "hello" {
		t.Fatalf("cat: %q", out)
	}
	if out := probe(t, r, runDir, "cat "+filepath.Join(runDir, "workdir", "notes.txt")); out != "hello" {
		t.Fatalf("cat of the absolute path: %q", out)
	}
	if out := probe(t, r, runDir, "head -n 1 workdir/notes.txt && tail -1 workdir/main.tf"); out != "hello\nresource {}" {
		t.Fatalf("head and tail with their values: %q", out)
	}
}

func TestProbeResultReadsLikeTheShellTools(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, "true"); out != "(no output)" {
		t.Fatalf("true: %q", out)
	}
	out := probe(t, r, runDir, "cat workdir/missing")
	if !strings.HasSuffix(out, "\n[exit status 1]") || !strings.Contains(out, "missing") {
		t.Fatalf("a failing probe keeps its output and ends with its status: %q", out)
	}
}

func TestProbeRefusesAPathOutsideTheRunDirectory(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"cat /etc/hosts", "ls /", "ls " + filepath.Dir(runDir), "cat ../x", "ls workdir/../../x",
		"cat out/../etc/hosts", "ls -LR out/..", "ls out", "cat out/hosts"} {
		refused(t, r, runDir, line)
	}
}

func TestProbeRefusesWhatTheShellWouldExpand(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"ls ~", "cat $HOME/x", "echo $HOME", "echo ~", "echo *", "ls *.tf", "ls workdir/*.tf", "cat $KUBECONFIG"} {
		refused(t, r, runDir, line)
	}
}

func TestProbeRefusesAProgramOffTheList(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"uname -a", "git status", "env", "rm workdir/notes.txt", "cd workdir", "which ls"} {
		refused(t, r, runDir, line)
	}
}

func TestProbeRefusesAFlagTheRuleDoesNotName(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"ls -L out", "ls -H", "tail -f workdir/notes.txt", "tail -fn 1 workdir/notes.txt",
		"tail --follow=name workdir/notes.txt", "file -f/etc/hosts", "file --files-from=/etc/hosts", "file -C -m x",
		"file -L workdir", "wc --files0-from=x", "stat -L out", "cat --unknown", "head --lines", "head -n"} {
		refused(t, r, runDir, line)
	}
}

func TestProbeFindTakesItsGrammarAndNothingElse(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, "find . -name *.tf"); !strings.Contains(out, "main.tf") {
		t.Fatalf("find with a pattern value: %q", out)
	}
	if out := probe(t, r, runDir, "find -P workdir -type f -maxdepth 1 -print"); !strings.Contains(out, "notes.txt") {
		t.Fatalf("find with a leading option: %q", out)
	}
	for _, line := range []string{"find . -delete", "find . -exec rm {} ;", "find /etc -name hosts", "find -L /etc", "find -H /etc",
		"find -f /etc", "find . -newer /etc/passwd", "find . -follow", "find . -samefile out", "find . -files0-from x",
		"find . -fprint x", "find out -name hosts", "find . -name"} {
		refused(t, r, runDir, line)
	}
}

func TestSplitChainSplitsOnAndAndSemicolon(t *testing.T) {
	parts, ops, ok := splitChain("pwd && ls -la; echo hi")
	if !ok || len(parts) != 3 || parts[1][1] != "-la" || parts[2][1] != "hi" || strings.Join(ops, " ") != "&& ;" {
		t.Fatalf("parts %v ops %v %v", parts, ops, ok)
	}
	if _, ops, ok := splitChain("echo 'a && b' && echo c"); !ok || len(ops) != 1 {
		t.Fatalf("an operator inside quotes is a word: %v %v", ops, ok)
	}
}

func TestSplitChainRefusesPipesOrAndRedirects(t *testing.T) {
	for _, line := range []string{"ls | wc -l", "ls || true", "ls > out", "ls &", "echo $(id)", "f() { ls; }", "cat <<EOF\nx\nEOF", "", "ls & ls"} {
		if _, _, ok := splitChain(line); ok {
			t.Fatalf("%q was split as a chain of probes", line)
		}
	}
}

func TestProbeChainStopsAtAFailureOnAndAndGoesOnAtASemicolon(t *testing.T) {
	r, runDir := probeRunDir(t)
	out := probe(t, r, runDir, "cat workdir/missing && echo after")
	if strings.Contains(out, "after") || !strings.HasSuffix(out, "[exit status 1]") {
		t.Fatalf("the chain ran on: %q", out)
	}
	out = probe(t, r, runDir, "cat workdir/missing; echo after")
	if !strings.HasSuffix(out, "\nafter") || strings.Contains(out, "exit status") {
		t.Fatalf("a semicolon goes on and the line's status is the last command's: %q", out)
	}
	if out := probe(t, r, runDir, "echo one && echo two"); out != "one\ntwo" {
		t.Fatalf("a chain of two: %q", out)
	}
}

func TestProbeChainWithAnOffListPartIsNotAProbe(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "pwd && uname -a")
	refused(t, r, runDir, "pwd && cat /etc/hosts")
}

func TestProbeOutputIsCapped(t *testing.T) {
	r, runDir := probeRunDir(t)
	big := strings.Repeat("0123456789abcdef", 8192) // 128 KiB
	if err := os.WriteFile(filepath.Join(runDir, "workdir", "big"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	out := probe(t, r, runDir, "cat workdir/big")
	if len(out) > shellProbeOutputLimit+len(shellProbeTruncated) || !strings.HasSuffix(out, shellProbeTruncated) {
		t.Fatalf("len %d, suffix %q", len(out), out[max(0, len(out)-20):])
	}
}

func TestFixtureResultReadsTheShellToolsShapes(t *testing.T) {
	for text, want := range map[string]struct {
		out    string
		status int
	}{
		"(no output)":                            {"", 0},
		"/usr/local/bin/terraform":               {"/usr/local/bin/terraform", 0},
		"sh: helm: not found\n[exit status 127]": {"sh: helm: not found", 127},
		"[exit status 1]":                        {"", 1},
	} {
		if out, st := fixtureResult(text); out != want.out || st != want.status {
			t.Fatalf("%q: %q %d", text, out, st)
		}
	}
}
