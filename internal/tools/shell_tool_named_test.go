package tools

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestShellToolNamesThePathsOfAnOpaqueMutation: an opaque mutating line's words are the paths it names; a
// line the classifier can read names none beyond its targets, and the deny lines reach the invocation.
func TestShellToolNamesThePathsOfAnOpaqueMutation(t *testing.T) {
	wd := t.TempDir()
	shell := ShellTool(io.Discard)
	inv := shell.Classify(map[string]any{"command": `python3 -c "open('terraform.tfstate','w').write('{}')"`}, wd)
	if !slices.Contains(inv.Targets.Named, filepath.Join(wd, "terraform.tfstate")) {
		t.Errorf("python3 -c: named %q", inv.Targets.Named)
	}
	if slices.ContainsFunc(inv.Targets.Named, func(p string) bool { return filepath.Base(p) == "-c" }) {
		t.Errorf("an option became a path: %q", inv.Targets.Named)
	}
	if inv := shell.Classify(map[string]any{"command": "rm -rf build"}, wd); len(inv.Targets.Named) != 0 {
		t.Errorf("rm -rf build names %q", inv.Targets.Named)
	}
	if inv := shell.Classify(map[string]any{"command": "cat terraform.tfstate"}, wd); len(inv.Targets.Named) != 0 {
		t.Errorf("a read names %q", inv.Targets.Named)
	}
	if inv := shell.Classify(map[string]any{"command": `sh -c "terraform destroy"`}, wd); !slices.Contains(inv.Lines,
		"terraform destroy") {
		t.Errorf("sh -c: lines %q", inv.Lines)
	}
}

// TestShellToolNamesWhatTheClassifierNamesAndTakesWordsAsWritten: the paths a line acts on by name reach the
// invocation, and the words of an opaque line are matched as written, so a * in a program's text is no glob.
func TestShellToolNamesWhatTheClassifierNamesAndTakesWordsAsWritten(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	shell := ShellTool(io.Discard)
	inv := shell.Classify(map[string]any{"command": `python3 -c "print(2 * 3)"`}, wd)
	if slices.Contains(inv.Targets.Named, filepath.Join(wd, ".git")) || !slices.Contains(inv.Targets.Named,
		filepath.Join(wd, "*")) {
		t.Errorf("python3 -c with a *: named %q", inv.Targets.Named)
	}
	inv = shell.Classify(map[string]any{"command": "git checkout -- terraform.tfstate"}, wd)
	if !slices.Contains(inv.Targets.Named, filepath.Join(wd, "terraform.tfstate")) {
		t.Errorf("git checkout: named %q", inv.Targets.Named)
	}
	inv = shell.Classify(map[string]any{"command": "find . -name '*.pyc' -delete"}, wd)
	if !slices.Contains(inv.Targets.Named, wd) || slices.Contains(inv.Targets.Paths, wd) {
		t.Errorf("find -name: named %q, paths %q", inv.Targets.Named, inv.Targets.Paths)
	}
}
