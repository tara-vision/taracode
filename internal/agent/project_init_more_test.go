package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectcontext "github.com/tara-vision/taracode/internal/context"
)

// fakeGit puts a git on PATH that answers the four questions /init asks of a repository, as
// fakeKubeTools does for kubectl.
func fakeGit(t *testing.T) {
	t.Helper()
	script := "#!/bin/sh\ncase \"$3\" in\n" +
		"branch) echo main ;;\n" +
		"remote) echo https://github.com/example/shop.git ;;\n" +
		"status) echo ' M main.go' ;;\n" +
		"log) echo 'abc1234 initial commit' ;;\n" +
		"esac\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInitProjectDescribesARepository(t *testing.T) {
	fakeGit(t)
	dir := t.TempDir()
	for name, content := range map[string]string{
		"go.mod":                 "module github.com/example/shop\n\ngo 1.22\n",
		"main.go":                "package main\n\nfunc main() {}\n",
		"Dockerfile":             "FROM scratch\n",
		"Chart.yaml":             "name: shop\n",
		"deploy/web.yaml":        "kind: Deployment\n",
		"internal/api/server.go": "package api\n",
		"Makefile":               "build:\n\tgo build\ntest:\n\tgo test\n.PHONY:\n%.o:\n",
	} {
		writeProjectFile(t, dir, name, content)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := InitProject(dir, "3.2.0"); err != nil {
			t.Error(err)
		}
	})
	md, err := os.ReadFile(filepath.Join(dir, "TARACODE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"**Type:** Go project", "**Module:** github.com/example/shop",
		"\u251c\u2500\u2500 deploy/\n\u2502   \u2514\u2500\u2500 web.yaml\n", "\u2514\u2500\u2500 main.go\n",
		"internal/\n\u2502   \u2514\u2500\u2500 api/\n\u2502       \u2514\u2500\u2500 server.go\n",
		"## Build Commands", "make build\nmake test\n",
		"## Git Info", "- **Branch:** main", "- **Remote:** https://github.com/example/shop.git",
		"- **Last commit:** abc1234 initial commit",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("TARACODE.md lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(string(md), "make .PHONY") || strings.Contains(string(md), "make %.o") {
		t.Errorf("special targets are not build commands:\n%s", md)
	}
	for _, want := range []string{
		"  Type: Go (github.com/example/shop)", "  Frameworks: docker, kubernetes, helm", "  Build commands: 2",
		"  Git branch: main", "relevant tools for this project", "and 1 more (use /tools to see all)",
		".taracode/policy.yaml (starter policy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary lacks %q:\n%s", want, out)
		}
	}
}

func TestInitProjectReportsWhatItCouldNotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	unwritable := t.TempDir()
	if err := os.Chmod(unwritable, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unwritable, 0o755) })
	_ = captureStdout(t, func() {
		if err := InitProject(unwritable, "3.2.0"); err == nil || !strings.Contains(err.Error(), "failed to initialize storage") {
			t.Errorf("err = %v", err)
		}
	})

	dir := t.TempDir()
	for _, sub := range []string{".taracode/context/project.json", ".taracode/project.json", "TARACODE.md"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil { // directories where files go
			t.Fatal(err)
		}
	}
	out := captureStdout(t, func() {
		if err := InitProject(dir, "3.2.0"); err == nil || !strings.Contains(err.Error(), "failed to generate TARACODE.md") {
			t.Errorf("err = %v", err)
		}
	})
	for _, want := range []string{"Warning: Could not save project context", "Warning: Could not save project config"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestInitProjectWarnsWhenThePolicyCannotBeWritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	dir := t.TempDir()
	taracode := filepath.Join(dir, ".taracode")
	for _, sub := range []string{"context/summaries", "history", "plans/archive", "state", "backups"} {
		if err := os.MkdirAll(filepath.Join(taracode, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(taracode, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taracode, 0o755) })
	out := captureStdout(t, func() {
		if err := InitProject(dir, "3.2.0"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "Warning: Could not write .taracode/policy.yaml") {
		t.Fatalf("%s", out)
	}
}

func TestWriteStarterPolicyReportsAPathItCannotCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads whatever the mode says")
	}
	dir := t.TempDir()
	taracode := filepath.Join(dir, ".taracode")
	if err := os.Mkdir(taracode, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(taracode, 0o600); err != nil { // no search permission: the stat itself fails
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taracode, 0o755) })
	if written, err := writeStarterPolicyIfAbsent(dir); written || err == nil || os.IsNotExist(err) {
		t.Fatalf("written %v, err %v", written, err)
	}
}

func TestWriteTreeStructureOfNothing(t *testing.T) {
	var sb strings.Builder
	writeTreeStructure(&sb, nil, "", true)
	writeTreeStructure(&sb, &projectcontext.DirectoryTree{Path: "", IsDir: true}, "", true)
	if sb.Len() != 0 {
		t.Fatalf("%q", sb.String())
	}
}
