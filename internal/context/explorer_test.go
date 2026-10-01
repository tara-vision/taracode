package context

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeFiles creates each file (parents included) under root with content "x".
func writeFiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// treePaths lists every node's path in the tree, directories with a trailing slash, sorted.
func treePaths(tree *DirectoryTree) []string {
	var out []string
	var walk func(n *DirectoryTree)
	walk = func(n *DirectoryTree) {
		if n.Path != "" {
			p := n.Path
			if n.IsDir {
				p += "/"
			}
			out = append(out, p)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)
	sort.Strings(out)
	return out
}

func TestExploreProjectSkipsWhatItShould(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "main.go", "go.sum", "app.log", ".env", ".git/HEAD", "node_modules/x/index.js",
		"internal/tool/tool.go", "docs/guide.md")
	tree, err := ExploreProject(root, DefaultExplorerOptions())
	if err != nil {
		t.Fatal(err)
	}
	if tree.Path != "" || !tree.IsDir || tree.Name != filepath.Base(root) {
		t.Fatalf("root node %+v", tree)
	}
	want := "docs/,docs/guide.md,internal/,internal/tool/,internal/tool/tool.go,main.go"
	if got := strings.Join(treePaths(tree), ","); got != want {
		t.Fatalf("tree %s, want %s", got, want)
	}
	if CountFiles(tree) != 3 || CountDirs(tree) != 4 || GetMaxDepth(tree) != 3 {
		t.Fatalf("files %d dirs %d depth %d", CountFiles(tree), CountDirs(tree), GetMaxDepth(tree))
	}
	for _, c := range tree.Children {
		if c.Path == "main.go" && (c.FileType != "go" || c.Size != 1) {
			t.Fatalf("main.go node %+v", c)
		}
	}
}

func TestExploreProjectWithHiddenFilesToADepth(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "app.log", ".env", ".git/HEAD", "node_modules/x/index.js")
	shallow, err := ExploreProject(root, ExplorerOptions{MaxDepth: 1, IncludeHidden: true, ExcludeDirs: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(treePaths(shallow), ","); got != ".env,.git/,app.log,node_modules/" {
		t.Fatalf("depth 1 with hidden files and no exclusions: %s", got)
	}
}

func TestExploreProjectKeepsAnUnreadableDirectoryEmpty(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads whatever the mode says")
	}
	root := t.TempDir()
	writeFiles(t, root, "locked/secret.txt")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	tree, err := ExploreProject(root, DefaultExplorerOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(treePaths(tree), ","); got != "locked/" {
		t.Fatalf("tree %s", got)
	}
}

func TestExploreProjectOfAMissingDirectory(t *testing.T) {
	if _, err := ExploreProject(filepath.Join(t.TempDir(), "absent"), DefaultExplorerOptions()); err == nil {
		t.Fatal("a missing root is an error")
	}
}

func TestTreeCountsOfNothing(t *testing.T) {
	file := &DirectoryTree{Name: "a.go"}
	if CountFiles(nil) != 0 || CountDirs(nil) != 0 || GetMaxDepth(nil) != 0 {
		t.Fatal("a nil tree counts nothing")
	}
	if CountFiles(file) != 1 || CountDirs(file) != 0 || GetMaxDepth(file) != 0 {
		t.Fatal("a single file")
	}
	if GetMaxDepth(&DirectoryTree{IsDir: true}) != 0 {
		t.Fatal("an empty directory has no depth")
	}
}

func TestDetectFileType(t *testing.T) {
	tests := map[string]string{
		"Makefile": "makefile", "GNUmakefile": "makefile", "Dockerfile": "dockerfile", "Vagrantfile": "ruby",
		"Rakefile": "ruby", "Gemfile": "ruby", "Procfile": "procfile", "CMakeLists.txt": "cmake",
		"a.go": "go", "a.jsx": "javascript", "a.mjs": "javascript", "a.tsx": "typescript", "a.py": "python",
		"a.rs": "rust", "a.rb": "ruby", "a.java": "java", "a.kts": "kotlin", "a.scala": "scala", "a.c": "c",
		"a.cc": "cpp", "a.hpp": "header", "a.cs": "csharp", "a.swift": "swift", "a.mm": "objc", "a.php": "php",
		"a.lua": "lua", "a.pm": "perl", "a.zsh": "shell", "a.psm1": "powershell", "a.R": "r", "a.sql": "sql",
		"a.markdown": "markdown", "a.rst": "rst", "a.txt": "text", "a.yml": "yaml", "a.json": "json",
		"a.toml": "toml", "a.xml": "xml", "a.htm": "html", "a.scss": "css", "a.vue": "vue", "a.svelte": "svelte",
		"a.proto": "protobuf", "a.gql": "graphql", "a.tfvars": "terraform", "a.hcl": "hcl", "a.zig": "zig",
		"a.nim": "nim", "a.exs": "elixir", "a.hrl": "erlang", "a.cljs": "clojure", "a.lhs": "haskell",
		"a.mli": "ocaml", "a.fsx": "fsharp", "a.weird": "weird", "LICENSE": "",
	}
	for name, want := range tests {
		if got := DetectFileType(name); got != want {
			t.Errorf("DetectFileType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestMatchesExcludePattern(t *testing.T) {
	if !matchesExcludePattern("app.min.js", DefaultExcludePatterns) || matchesExcludePattern("app.js", DefaultExcludePatterns) {
		t.Fatal("*.min.js is excluded, a plain .js file is not")
	}
}
