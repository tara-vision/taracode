package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchPathCoversTheDirectoryAPatternNames(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{".git/**", "/w/.git", true},
		{".git/**", "/w/.git/config", true},
		{".git/**", "/w/.github", false},
		{".git/**", "/w/.github/workflows/ci.yml", false},
		{"/w/infra/**", "/w/infra", true},
		{"**", "/w/anything", true},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.path, "/w"); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestMatchPathFoldsCase(t *testing.T) {
	cases := []struct{ pattern, path string }{
		{"**/*.tfstate", "/w/TERRAFORM.TFSTATE"},
		{".git/**", "/w/.GIT/config"},
		{"infra/prod/**", "/w/Infra/Prod/main.tf"},
	}
	for _, c := range cases {
		if !matchPath(c.pattern, c.path, "/w") {
			t.Errorf("matchPath(%q, %q) = false, want true", c.pattern, c.path)
		}
	}
}

func TestMatchPathFollowsSymlinks(t *testing.T) {
	wd := t.TempDir()
	mustWrite(t, filepath.Join(wd, "terraform.tfstate"))
	mustWrite(t, filepath.Join(wd, ".git", "config"))
	mustLink(t, "terraform.tfstate", filepath.Join(wd, "s.json"))
	mustLink(t, ".git", filepath.Join(wd, "repo"))
	cases := []struct{ pattern, path string }{
		{"**/*.tfstate", filepath.Join(wd, "s.json")},
		{".git/**", filepath.Join(wd, "repo", "config")},
		{".git/**", filepath.Join(wd, "repo", "a-file-not-yet-written")},
		{".git/**", filepath.Join(wd, "repo")},
	}
	for _, c := range cases {
		if !matchPath(c.pattern, c.path, wd) {
			t.Errorf("matchPath(%q, %q) = false, want true", c.pattern, c.path)
		}
	}
	if matchPath("**/*.tfstate", filepath.Join(wd, "plain.json"), wd) {
		t.Error("a path with no link to a protected file matched")
	}
}

func TestMatchPathKeepsTheWrittenPathWhenNothingResolves(t *testing.T) {
	saved := evalSymlinks
	evalSymlinks = func(string) (string, error) { return "", errors.New("no file system") }
	t.Cleanup(func() { evalSymlinks = saved })
	if !matchPath(".git/**", "/w/.git/config", "/w") {
		t.Error("the written path stopped matching")
	}
	if matchPath("**/*.tfstate", "/w/s.json", "/w") {
		t.Error("a path matched that neither its written nor a resolved form names")
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestMatchPathFoldsCaseInTheRelativeForm(t *testing.T) {
	if !matchPath(".git/**", "/x/PROJ/.git/config", "/x/proj") {
		t.Error("a relative pattern missed a path written in another case than the working directory")
	}
}

// TestMatchPathResolvesTheWorkingDirectoryOnItsOwn: a path already written in its resolved form still matches a
// relative pattern under a symlinked working directory.
func TestMatchPathResolvesTheWorkingDirectoryOnItsOwn(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project", ".git", "config"))
	mustLink(t, "project", filepath.Join(root, "link"))
	project, err := filepath.EvalSymlinks(filepath.Join(root, "project"))
	if err != nil {
		t.Fatal(err)
	}
	if !matchPath(".git/**", filepath.Join(project, ".git", "config"), filepath.Join(root, "link")) {
		t.Error("a resolved path under a symlinked working directory did not match")
	}
}

func TestResolvedGivesUpOnAVeryLongPath(t *testing.T) {
	calls := 0
	saved := evalSymlinks
	evalSymlinks = func(p string) (string, error) { calls++; return p, nil }
	t.Cleanup(func() { evalSymlinks = saved })
	long := "/" + strings.Repeat("a/", 300) + "x"
	if got, ok := resolved(long); ok || got != long || calls != 0 {
		t.Errorf("resolved of a %d-part path: %q %v after %d lookups", 301, got, ok, calls)
	}
}

func TestResolvedLeavesAnEmptyPathAlone(t *testing.T) {
	if got, ok := resolved(""); ok || got != "" {
		t.Errorf("resolved of an empty path: %q %v", got, ok)
	}
}
