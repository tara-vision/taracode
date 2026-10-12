package policy

import (
	"os"
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
		{"a file with nothing protected", "build/out.bin", true, ""},
		{"a path that is not there", "gone", true, ""},
		{"a tree package managers own is not walked", "node_modules", true, ""},
		{"nor is one nested in the target", "a", true, ""},
		{"the backend cache in .terraform is not walked", "b", true, ""},
		{"nor is .terraform itself", "b/.terraform", true, ""},
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

// TestALinkToADirectoryIsCheckedThroughItsTarget: rm -rf link/ removes what the link points to, and the trailing
// slash is gone by the time the policy sees the path, so a link to a directory is checked as that directory.
func TestALinkToADirectoryIsCheckedThroughItsTarget(t *testing.T) {
	wd := project(t)
	mustLink(t, "infra", filepath.Join(wd, "infra-link"))
	if v := Default().Evaluate(ModeOperate, shellRemoves(wd, "infra-link")); v.Allow || !strings.Contains(v.Reason,
		"x.tfstate") {
		t.Errorf("a link to a directory that holds a state file: %+v", v)
	}
}

func TestALiteralPatternMatchesTheDirectoryInAnyCase(t *testing.T) {
	wd := project(t)
	mustWrite(t, filepath.Join(wd, "ops", "prod", "main.tf"))
	if err := os.MkdirAll(filepath.Join(wd, "OPS"), 0o750); err != nil {
		t.Fatal(err)
	}
	p := Default()
	p.Protected.Paths = append(p.Protected.Paths, "ops/prod/**")
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "OPS")); v.Allow {
		t.Errorf("OPS against ops/prod/**: %+v", v)
	}
}

// TestABareNamePatternIsCheckedByTheWalk: a pattern without a slash names a base name anywhere, so a directory
// that holds one is found by the walk, not by a fixed path under the working directory.
func TestABareNamePatternIsCheckedByTheWalk(t *testing.T) {
	wd := project(t)
	mustWrite(t, filepath.Join(wd, "app", ".env"))
	p := Default()
	p.Protected.Paths = append(p.Protected.Paths, ".env")
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "app")); v.Allow || !strings.Contains(v.Reason, ".env") {
		t.Errorf("app holding .env: %+v", v)
	}
}

func TestAPolicyOfFixedPathsChecksADirectoryWithoutAWalk(t *testing.T) {
	wd := project(t)
	p := Default()
	p.Protected.Paths = []string{".git/**"}
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, "infra")); !v.Allow {
		t.Errorf("infra holds nothing this policy protects: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, shellRemoves(wd, ".")); v.Allow {
		t.Errorf("the project holds .git: %+v", v)
	}
}
