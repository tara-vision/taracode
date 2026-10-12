package policy

import (
	"path/filepath"
	"testing"
)

// TestAPathAnOpaqueCommandNamesIsMatchedButNotWalked: a path an opaque command names is protected like a
// target it writes, but a directory among them is not walked: the command may only work in it.
func TestAPathAnOpaqueCommandNamesIsMatchedButNotWalked(t *testing.T) {
	wd := project(t)
	named := func(paths ...string) Invocation {
		inv := Invocation{Tool: "shell", Classification: Mutate, Command: "python3 -c ...", WorkingDir: wd}
		for _, p := range paths {
			inv.Targets.Named = append(inv.Targets.Named, filepath.Join(wd, p))
		}
		return inv
	}
	p := Default()
	if v := p.Evaluate(ModeOperate, named("infra/x.tfstate")); v.Allow || v.Rule != "protected.paths" {
		t.Errorf("a named state file: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, named(".git")); v.Allow {
		t.Errorf("a named .git: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, named(".", "infra", "build")); !v.Allow {
		t.Errorf("named directories that hold protected paths were walked: %+v", v)
	}
}
