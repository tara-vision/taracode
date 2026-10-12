package policy

import (
	"errors"
	"os"
	"path/filepath"
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

func TestMatchPathResolvesASymlinkedWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "project", ".git", "config"))
	mustLink(t, "project", filepath.Join(root, "link"))
	wd := filepath.Join(root, "link")
	if !matchPath(".git/**", filepath.Join(wd, ".git", "config"), wd) {
		t.Error("a protected path under a symlinked working directory did not match")
	}
}
