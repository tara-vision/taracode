package policy

import (
	"path/filepath"
	"strings"
	"testing"
)

// findProject builds a working directory where a find's name tests can reach protected paths: a git directory,
// a state file under infra, and files that share names with them but are not protected.
func findProject(t *testing.T) string {
	t.Helper()
	wd := t.TempDir()
	for _, f := range []string{".git/config", ".git/HEAD", "a/x.pyc", "a/config", "infra/x.tfstate",
		"node_modules/pkg/config"} {
		mustWrite(t, filepath.Join(wd, f))
	}
	return wd
}

func findRemoves(wd, start string, tests ...NameTest) Invocation {
	return Invocation{Tool: "shell", Classification: Mutate, Command: "find", WorkingDir: wd,
		Targets: Targets{Finds: []Find{{Start: filepath.Join(wd, start), Written: start, Tests: tests}}}}
}

// TestAFindIsCheckedForTheProtectedPathsItsTestsMatch: a find narrowed by name acts on what its tests match under
// its start point, and on what a directory it matches holds; a protected path among them refuses it.
func TestAFindIsCheckedForTheProtectedPathsItsTestsMatch(t *testing.T) {
	wd := findProject(t)
	cases := []struct {
		name   string
		tests  []NameTest
		allow  bool
		reason string
	}{
		{"a name inside .git", []NameTest{{"-name", "config"}}, false, ".git"},
		{"a name in another case", []NameTest{{"-iname", "CONFIG"}}, false, ".git"},
		{"a name nothing protected has", []NameTest{{"-name", "*.pyc"}}, true, ""},
		{"a path pattern whose * crosses slashes", []NameTest{{"-path", "*/.git/*"}}, false, ".git"},
		{"the path find prints", []NameTest{{"-path", "./a/*"}}, true, ""},
		{"a regular expression", []NameTest{{"-regex", ".*/HEAD"}}, false, "HEAD"},
		{"a directory the tests match, with what it holds", []NameTest{{"-name", "infra"}}, false, "x.tfstate"},
		{"a directory the tests match that holds nothing protected", []NameTest{{"-name", "a"}}, true, ""},
		{"a bracket matches every entry", []NameTest{{"-name", "[c]onfig"}}, false, ".git"},
		{"a backslash matches every entry", []NameTest{{"-name", `conf\ig`}}, false, ".git"},
		{"a regular expression that does not compile matches every entry", []NameTest{{"-regex", "("}}, false, ".git"},
		{"an emacs group matches every entry", []NameTest{{"-regex", `.*\(HEAD\)`}}, false, ".git"},
		{"an emacs alternative matches every entry", []NameTest{{"-regex", `.*HEAD\|x`}}, false, ".git"},
		{"a tree package managers own is not walked", []NameTest{{"-name", "node_modules"}}, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Default().Evaluate(ModeOperate, findRemoves(wd, ".", c.tests...))
			if v.Allow != c.allow || !strings.Contains(v.Reason, c.reason) {
				t.Fatalf("got %+v, want allow %v with a reason naming %q", v, c.allow, c.reason)
			}
			if !c.allow && v.Rule != "protected.paths" {
				t.Fatalf("rule %q, want protected.paths", v.Rule)
			}
		})
	}
}

func TestAFindWalkThatReachesItsBoundRefuses(t *testing.T) {
	wd := findProject(t)
	saved := dirWalkBound
	dirWalkBound = 3
	t.Cleanup(func() { dirWalkBound = saved })
	v := Default().Evaluate(ModeOperate, findRemoves(wd, ".", NameTest{"-name", "*.pyc"}))
	if v.Allow || !strings.Contains(v.Reason, "more than 3") {
		t.Fatalf("got %+v, want a refusal that names the bound", v)
	}
}

func TestAFindFromAStartPointThatIsNoDirectoryWalksNothing(t *testing.T) {
	wd := findProject(t)
	for _, start := range []string{"a/x.pyc", "missing", "node_modules"} {
		if v := Default().Evaluate(ModeOperate, findRemoves(wd, start, NameTest{"-name", "*"})); !v.Allow {
			t.Errorf("%s: %+v", start, v)
		}
	}
	deep := findRemoves(wd, ".", NameTest{"-name", "x"})
	deep.Targets.Finds[0].Start = "/w/" + strings.Repeat("l/", 300)
	if v := Default().Evaluate(ModeOperate, deep); v.Allow || !strings.Contains(v.Reason, "more than 256 parts") {
		t.Errorf("a start point too deep to check: %+v", v)
	}
}
