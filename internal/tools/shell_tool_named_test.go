package tools

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
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
	inv = shell.Classify(map[string]any{"command": "echo .g?t | xargs rm -rf"}, wd)
	if !slices.Contains(inv.Targets.Named, filepath.Join(wd, ".git")) {
		t.Errorf("an opaque line's glob word: named %q", inv.Targets.Named)
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

// TestShellToolResolvesTheStartPointsOfAFind: a find narrowed by name reaches the policy with its start point
// resolved like any path, and as written for the paths find prints.
func TestShellToolResolvesTheStartPointsOfAFind(t *testing.T) {
	wd := t.TempDir()
	inv := ShellTool(io.Discard).Classify(map[string]any{"command": "find . sub -name config -delete"}, wd)
	want := []policy.Find{{Start: wd, Written: ".", Tests: []policy.NameTest{{Test: "-name", Pattern: "config"}}},
		{Start: filepath.Join(wd, "sub"), Written: "sub", Tests: []policy.NameTest{{Test: "-name", Pattern: "config"}}}}
	if !reflect.DeepEqual(inv.Targets.Finds, want) {
		t.Errorf("finds %+v, want %+v", inv.Targets.Finds, want)
	}
}

// TestShellToolKeepsALandingUnderADirectoryOnly: where a copy puts a source is a target when the destination is a
// directory that exists; a destination that is a file is checked by its own name, not as a directory.
func TestShellToolKeepsALandingUnderADirectoryOnly(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "infra"), 0o750); err != nil {
		t.Fatal(err)
	}
	shell := ShellTool(io.Discard)
	inv := shell.Classify(map[string]any{"command": "cp terraform.tfstate terraform.tfstate.backup"}, wd)
	if len(inv.Targets.Paths) != 0 {
		t.Errorf("a copy to a file: paths %q", inv.Targets.Paths)
	}
	for _, cmd := range []string{"cp /tmp/x.tfstate infra", "cp /tmp/x.tfstate inf*", "cp /tmp/x.tfstate in?ra/"} {
		inv = shell.Classify(map[string]any{"command": cmd}, wd)
		if !slices.Contains(inv.Targets.Paths, filepath.Join(wd, "infra", "x.tfstate")) {
			t.Errorf("%s: paths %q", cmd, inv.Targets.Paths)
		}
	}
}

// TestShellToolExpandsAGlobAsTheShellDoes: a * or ? does not match a leading dot unless the pattern's element
// starts with one or the line turns dotglob on, so chmod 644 * leaves .git alone.
func TestShellToolExpandsAGlobAsTheShellDoes(t *testing.T) {
	wd := t.TempDir()
	for _, dir := range []string{".git", "src"} {
		if err := os.MkdirAll(filepath.Join(wd, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	shell := ShellTool(io.Discard)
	cases := []struct {
		cmd       string
		git, srcs bool
	}{
		{"chmod 644 *", false, true},
		{"rm -rf .g*", true, false},
		{"shopt -s dotglob; chmod 644 *", true, true},
		{"setopt GLOB_DOTS; chmod 644 *", true, true},
		{"cp x */", false, true},
	}
	for _, c := range cases {
		paths := shell.Classify(map[string]any{"command": c.cmd}, wd).Targets.Paths
		hasGit := slices.ContainsFunc(paths, func(p string) bool { return strings.Contains(p, ".git") })
		hasSrc := slices.ContainsFunc(paths, func(p string) bool { return strings.Contains(p, "src") })
		if hasGit != c.git || hasSrc != c.srcs {
			t.Errorf("%s: paths %q", c.cmd, paths)
		}
	}
}

// TestShellToolBoundsTheGlobOfALandingDirectory: a destination directory with more than three wildcard levels is
// not globbed, which would read the whole tree; its landing is checked as written instead.
func TestShellToolBoundsTheGlobOfALandingDirectory(t *testing.T) {
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "a", "b", "c", "d"), 0o750); err != nil {
		t.Fatal(err)
	}
	paths := ShellTool(io.Discard).Classify(map[string]any{"command": "cp x.tfstate */*/*/*/"}, wd).Targets.Paths
	if !slices.Contains(paths, filepath.Join(wd, "*/*/*/*/x.tfstate")) || slices.Contains(paths,
		filepath.Join(wd, "a/b/c/d/x.tfstate")) {
		t.Errorf("paths %q", paths)
	}
	paths = ShellTool(io.Discard).Classify(map[string]any{"command": "cp x.tfstate */*/*/"}, wd).Targets.Paths
	if !slices.Contains(paths, filepath.Join(wd, "a/b/c/x.tfstate")) {
		t.Errorf("three levels: paths %q", paths)
	}
}
