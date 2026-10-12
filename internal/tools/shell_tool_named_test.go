package tools

import (
	"io"
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
