package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeTree creates every file of files (relative paths, parents created) under root.
func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("content of "+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// unreadableDir creates root/name with no permissions, restored when the test ends.
func unreadableDir(t *testing.T, root, name string) {
	t.Helper()
	skipIfRoot(t)
	dir := filepath.Join(root, name)
	writeTree(t, root, filepath.Join(name, "inside.txt"))
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern, candidate string
		want               int
	}{
		{"", "anything", 1},
		{"MA", "main.go", 1002},
		{"lib", "src/lib.go", 903},
		{"ib", "src/lib.go", 500},
		{"mgo", "main.go", 103},
		{"xyz", "main.go", 0},
	}
	for _, tt := range tests {
		if got := fuzzyMatch(tt.pattern, tt.candidate); got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %d, want %d", tt.pattern, tt.candidate, got, tt.want)
		}
	}
}

func TestIsInitializedProject(t *testing.T) {
	dir := t.TempDir()
	if isInitializedProject(dir) {
		t.Fatal("a directory without .taracode/ is not initialised")
	}
	if err := os.Mkdir(filepath.Join(dir, ".taracode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isInitializedProject(dir) {
		t.Fatal("a directory with .taracode/ is initialised")
	}
}

const testGitignore = `# build output

*.log
!keep.log
build/
/secret.txt
docs/*.md
**/tmp
vendored
`

func TestLoadGitignoreParsesTheRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(testGitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	fc := NewFileCompleter(dir)
	want := []gitignorePattern{
		{pattern: "*.log"}, {pattern: "keep.log", isNegate: true}, {pattern: "build", isDir: true},
		{pattern: "/secret.txt"}, {pattern: "docs/*.md"}, {pattern: "**/tmp"}, {pattern: "vendored"},
	}
	if fmt.Sprint(fc.gitignoreRules) != fmt.Sprint(want) {
		t.Fatalf("rules %+v, want %+v", fc.gitignoreRules, want)
	}
}

func TestIsIgnoredAppliesTheRulesInOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(testGitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	fc := NewFileCompleter(dir)
	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"logs/app.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"build", false, false},
		{"secret.txt", false, true},
		{"sub/secret.txt", false, false},
		{"docs/a.md", false, true},
		{"x/docs/a.md", false, false},
		{"a/tmp", true, true},
		{"a/tmpfile", false, false},
		{"src/vendored/x.go", false, true},
		{"main.go", false, false},
	}
	for _, tt := range tests {
		if got := fc.isIgnored(tt.path, tt.isDir); got != tt.want {
			t.Errorf("isIgnored(%q, dir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
	if NewFileCompleter(t.TempDir()).isIgnored("app.log", false) {
		t.Error("without a .gitignore nothing is ignored")
	}
}

// deepDirs is d1/d2/.../dn.
func deepDirs(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("d%d", i+1)
	}
	return filepath.Join(parts...)
}

func TestGetFilesWithGitignoreWalksTheProject(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, ".gitignore", "main.go", "app.log", "keep.log", "build/out.bin", "secret.txt", "docs/a.md",
		"docs/readme.txt", ".hidden/x.txt", ".env", "node_modules/pkg.js", "src/lib.go", "work/tmp/scratch.txt",
		filepath.Join(deepDirs(10), "too-deep.go"), filepath.Join(deepDirs(11), "deeper.go"))
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(testGitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadableDir(t, dir, "locked")

	files, err := NewFileCompleter(dir).getFilesWithGitignore()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	for _, want := range []string{"main.go", "keep.log", "docs/", "docs/readme.txt", "src/", "src/lib.go", "work/",
		deepDirs(10) + "/"} {
		if !got[want] {
			t.Errorf("listing lacks %q: %v", want, files)
		}
	}
	for _, absent := range []string{"app.log", "build/", "build/out.bin", "secret.txt", "docs/a.md", ".hidden/",
		".env", "node_modules/", "node_modules/pkg.js", filepath.Join(deepDirs(10), "too-deep.go"), deepDirs(11) + "/",
		"locked/", "locked/inside.txt", "work/tmp/", "work/tmp/scratch.txt"} {
		if got[absent] {
			t.Errorf("listing has %q: %v", absent, files)
		}
	}
}

func TestFileCompleterDoRanksTheMatches(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "lib.go", "src/lib.go", "calibrate.txt", "l_i_b.txt", "zzz.txt")
	fc := NewFileCompleter(dir)
	line := []rune("look at @lib")
	matches, length := fc.Do(line, len(line))
	var got []string
	for _, m := range matches {
		got = append(got, string(m))
	}
	if strings.Join(got, ",") != "lib.go,src/lib.go,calibrate.txt,l_i_b.txt" || length != 3 {
		t.Fatalf("Do() = %v, %d", got, length)
	}

	for _, input := range []string{"no reference", "@nothing-like-it"} {
		line := []rune(input)
		if matches, length := fc.Do(line, len(line)); matches != nil || length != 0 {
			t.Errorf("Do(%q) = %v, %d, want nothing", input, matches, length)
		}
	}
	empty := NewFileCompleter(t.TempDir())
	if matches, _ := empty.Do([]rune("@"), 1); matches != nil {
		t.Errorf("an empty directory offers %v", matches)
	}
}

func TestFileCompleterDoOffersAtMostTwenty(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 25; i++ {
		writeTree(t, dir, fmt.Sprintf("f%02d.txt", i))
	}
	matches, _ := NewFileCompleter(dir).Do([]rune("@f"), 2)
	if len(matches) != maxCompletionResults {
		t.Fatalf("%d candidates, want %d", len(matches), maxCompletionResults)
	}
}

func TestUpdateWorkingDirReloadsTheGitignore(t *testing.T) {
	ignoring, plain := t.TempDir(), t.TempDir()
	writeTree(t, ignoring, "x.log")
	writeTree(t, plain, "x.log")
	if err := os.WriteFile(filepath.Join(ignoring, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sc := NewSlashCompleter(ignoring)
	if matches, _ := sc.Do([]rune("@x"), 2); matches != nil {
		t.Fatalf("x.log is ignored in %s: %v", ignoring, matches)
	}
	sc.UpdateWorkingDir(plain)
	if matches, _ := sc.Do([]rune("@x"), 2); len(matches) != 1 || string(matches[0]) != "x.log" {
		t.Fatalf("after the move x.log is offered: %v", matches)
	}
}

func TestGetFilesRecursiveSkipsHiddenAndVendoredDirectories(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "main.go", "pkg/a.go", ".git/HEAD", ".env", "vendor/v.go", "dist/app.js")
	unreadableDir(t, dir, "locked")
	files, err := getFilesRecursive(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if got := strings.Join(files, ","); got != "main.go,pkg/,pkg/a.go" { // the unreadable directory is left out
		t.Fatalf("getFilesRecursive() = %s", got)
	}
}

func TestGetFilesInDirectory(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "conf/a.yaml", "conf/nested/b.yaml", "conf/.secret", "conf/.git/config", "conf/node_modules/m.js",
		"other.txt")
	unreadableDir(t, root, "conf/locked")
	conf := filepath.Join(root, "conf")

	flat, err := getFilesInDirectory(conf, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(flat, ",") != "conf/a.yaml" {
		t.Fatalf("non-recursive: %v", flat)
	}
	deep, err := getFilesInDirectory(conf, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deep, ",") != "conf/a.yaml,conf/nested/b.yaml" {
		t.Fatalf("recursive: %v", deep)
	}
}
