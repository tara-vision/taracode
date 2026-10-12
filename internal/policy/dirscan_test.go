package policy

import (
	"path/filepath"
	"strings"
	"testing"
)

// project builds a working directory with a state file under infra, a git directory, a build directory with
// nothing protected and a node_modules tree that holds a state file of its own.
func project(t *testing.T) string {
	t.Helper()
	wd := t.TempDir()
	mustWrite(t, filepath.Join(wd, "infra", "x.tfstate"))
	mustWrite(t, filepath.Join(wd, ".git", "config"))
	mustWrite(t, filepath.Join(wd, "build", "out.bin"))
	mustWrite(t, filepath.Join(wd, "node_modules", "pkg", "x.tfstate"))
	mustWrite(t, filepath.Join(wd, "a", "node_modules", "pkg", "x.tfstate"))
	mustWrite(t, filepath.Join(wd, "a", "src", "main.go"))
	mustWrite(t, filepath.Join(wd, "b", ".terraform", "terraform.tfstate"))
	return wd
}

func shellRemoves(wd string, paths ...string) Invocation {
	abs := make([]string, len(paths))
	for i, p := range paths {
		abs[i] = filepath.Join(wd, p)
	}
	return Invocation{Tool: "shell", Classification: Mutate, Command: "rm -rf " + strings.Join(paths, " "),
		WorkingDir: wd, Targets: Targets{Paths: abs}}
}

func TestADirectoryTargetIsCheckedForTheProtectedPathsItHolds(t *testing.T) {
	wd := project(t)
	p := Default()
	cases := []struct {
		name, path string
		allow      bool
		reason     string
	}{
		{"a directory holding a state file", "infra", false, "x.tfstate"},
		{"the project, which holds .git", ".", false, ".git"},
		{"a directory with nothing protected", "build", true, ""},
		{"a tree package managers own is not walked", "node_modules", true, ""},
		{"nor is one nested in the target", "a", true, ""},
		{".terraform is walked", "b", false, "terraform.tfstate"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := p.Evaluate(ModeOperate, shellRemoves(wd, c.path))
			if v.Allow != c.allow || !strings.Contains(v.Reason, c.reason) {
				t.Fatalf("got %+v, want allow %v with a reason naming %q", v, c.allow, c.reason)
			}
			if !c.allow && v.Rule != "protected.paths" {
				t.Fatalf("rule %q, want protected.paths", v.Rule)
			}
		})
	}
}

func TestALiteralPatternReachesInsideASkippedTree(t *testing.T) {
	wd := project(t)
	mustWrite(t, filepath.Join(wd, "node_modules", "keep", "index.js"))
	p := Default()
	p.Protected.Paths = append(p.Protected.Paths, "node_modules/keep/**", filepath.Join(wd, "build", "out.bin"))
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "node_modules")); v.Allow || !strings.Contains(v.Reason, "keep") {
		t.Errorf("node_modules/keep/** inside a skipped tree: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "build")); v.Allow || !strings.Contains(v.Reason, "out.bin") {
		t.Errorf("an absolute protected file under the target: %+v", v)
	}
	mustWrite(t, filepath.Join(wd, "c", "ok.txt"))
	p.Protected.Paths = append(Default().Protected.Paths, "c/missing/**")
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "c")); !v.Allow {
		t.Errorf("a literal part that is not on disk refused: %+v", v)
	}
}

func TestAWalkThatReachesItsBoundRefuses(t *testing.T) {
	wd := project(t)
	for _, name := range []string{"1", "2", "3", "4", "5"} {
		mustWrite(t, filepath.Join(wd, "many", name))
	}
	saved := dirWalkBound
	dirWalkBound = 3
	t.Cleanup(func() { dirWalkBound = saved })
	v := Default().Evaluate(ModeOperate, shellRemoves(wd, "many"))
	if v.Allow || v.Rule != "protected.paths" || !strings.Contains(v.Reason, "more than 3") {
		t.Fatalf("got %+v, want a refusal that names the bound", v)
	}
}

func TestOnlyShellDirectoryTargetsAreChecked(t *testing.T) {
	wd := project(t)
	inv := Invocation{Tool: "terraform", Verb: "apply", Classification: Mutate, Command: "terraform apply",
		WorkingDir: wd, Targets: Targets{Paths: []string{filepath.Join(wd, "infra")}}}
	if v := Default().Evaluate(ModeOperate, inv); !v.Allow {
		t.Errorf("terraform in a directory that holds its state: %+v", v)
	}
}

func TestASymlinkToADirectoryIsNotWalked(t *testing.T) {
	wd := project(t)
	mustLink(t, "infra", filepath.Join(wd, "infra-link"))
	if v := Default().Evaluate(ModeOperate, shellRemoves(wd, "infra-link")); !v.Allow {
		t.Errorf("rm -rf of a link removes the link, not what it points to: %+v", v)
	}
}
