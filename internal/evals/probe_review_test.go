package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The second review's lines: a probe answers as the shell tool would for the same line, or not at all.

func TestProbeRefusesWhatTheShellWouldExpandInAnyPosition(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"cat {workdir/main.tf,workdir/notes.txt}", "head -n $HOME workdir/notes.txt",
		"ls --color=$HOME", "find . -name *.tf", `find . -name "$X"`, "echo `id`", "cat ~/x", "ls workdir/*"} {
		refused(t, r, runDir, line)
	}
	if out := probe(t, r, runDir, "find . -name '*.tf'"); !strings.Contains(out, "main.tf") { // quoted: find's own pattern
		t.Fatalf("a quoted pattern: %q", out)
	}
	if out := probe(t, r, runDir, `find . -name "*.tf"`); !strings.Contains(out, "main.tf") {
		t.Fatalf("a double-quoted pattern: %q", out)
	}
}

func TestProbeEchoTakesPlainWordsOnly(t *testing.T) {
	r, runDir := probeRunDir(t)
	if out := probe(t, r, runDir, `echo hello "world"`); out != "hello world" {
		t.Fatalf("echo: %q", out)
	}
	for _, line := range []string{"echo -n hi", "echo -e 'a\\tb'", `echo "a\nb"`, `echo a\;b && echo c`, `cat workdir/notes.txt\ `} {
		refused(t, r, runDir, line)
	}
}

func TestProbeStatTakesNoValueForDashF(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "stat -f /etc")
	refused(t, r, runDir, "stat -f / workdir/notes.txt")
	if out := probe(t, r, runDir, "stat workdir/notes.txt"); !strings.Contains(out, "notes.txt") {
		t.Fatalf("stat: %q", out)
	}
}

func TestProbeFindNeedsAStartPoint(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "find -name x")
	refused(t, r, runDir, "find")
}

func TestProbeFileNeverTouchesTimestamps(t *testing.T) {
	r, runDir := probeRunDir(t)
	refused(t, r, runDir, "file -p workdir/notes.txt")
}

func TestProbePwdPhysicalResolvesSymlinks(t *testing.T) {
	r, runDir := probeRunDir(t)
	link := filepath.Join(filepath.Dir(runDir), "link-to-run")
	if err := os.Symlink(runDir, link); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if out := probe(t, r, link, "pwd"); out != link {
		t.Fatalf("pwd is the logical directory: %q", out)
	}
	if out := probe(t, r, link, "pwd -P"); out != physical {
		t.Fatalf("pwd -P is the physical one: %q, want %q", out, physical)
	}
}

func TestProbeChainJoinsOutputsAsTheShellDoes(t *testing.T) {
	r, runDir := probeRunDir(t)
	if err := os.WriteFile(filepath.Join(runDir, "workdir", "nolf.txt"), []byte("no newline"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := probe(t, r, runDir, "cat workdir/nolf.txt && echo done"); out != "no newlinedone" {
		t.Fatalf("a file without a trailing newline runs into the next command's output: %q", out)
	}
	if out := probe(t, r, runDir, "cat workdir/notes.txt && echo done"); out != "hello\ndone" {
		t.Fatalf("a file with one: %q", out)
	}
	if out := probe(t, r, runDir, "pwd && cat workdir/nolf.txt"); out != runDir+"\nno newline" {
		t.Fatalf("pwd prints its newline: %q", out)
	}
}

func TestProbeTakesATrailingSemicolonOrNewline(t *testing.T) {
	r, runDir := probeRunDir(t)
	for _, line := range []string{"pwd;", "pwd\n", "pwd && ls workdir\n", "pwd ; "} {
		if out := probe(t, r, runDir, line); !strings.HasPrefix(out, runDir) {
			t.Fatalf("%q: %q", line, out)
		}
	}
}

func TestProbeReportsAMissingProgramAsTheShellDoes(t *testing.T) {
	r, runDir := probeRunDir(t)
	t.Setenv("PATH", "")
	parts, ops, _ := splitChain("ls")
	out, rc, handled := r.probeLine(context.Background(), runDir, "ls", "shell ls", parts, ops)
	if !handled || rc.Err != nil || !strings.Contains(out, "ls: command not found") || !strings.HasSuffix(out, "[exit status 127]") {
		t.Fatalf("handled %v err %v out %q", handled, rc.Err, out)
	}
}
