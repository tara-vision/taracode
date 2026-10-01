package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxedPathRefusesASymlinkOutOfTheProject(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, "out")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SandboxedPath("out", "", project); err == nil ||
		!strings.Contains(err.Error(), "path escapes project root (symlink detected)") {
		t.Fatalf("err = %v", err)
	}
}

func TestSandboxedPathReportsASymlinkLoop(t *testing.T) {
	project := t.TempDir()
	for _, link := range [][2]string{{"a", "b"}, {"b", "a"}} {
		if err := os.Symlink(filepath.Join(project, link[1]), filepath.Join(project, link[0])); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := SandboxedPath("a", "", project); err == nil || !strings.Contains(err.Error(), "cannot access path") {
		t.Fatalf("err = %v", err)
	}
}

func TestPromptsShortenADeepDirectory(t *testing.T) {
	deep := filepath.Join("services", "payments", "internal", "handlers")
	want := "[..." + string(filepath.Separator) + filepath.Join("internal", "handlers") + "]"
	for _, prompt := range []string{FormatPromptWithContext(deep, 0, 0), FormatPromptWithMode(deep, 0, 0, false)} {
		if !strings.Contains(prompt, want) || strings.Contains(prompt, "payments") {
			t.Errorf("prompt %q, want %q", prompt, want)
		}
	}
}

func TestPromptWithModeColoursTheBudget(t *testing.T) {
	tests := []struct {
		used int
		want string
	}{
		{30000, "\033[31m[30k/33k]"},
		{25000, "\033[33m[25k/33k]"},
		{5000, "\033[36m[5.0k/33k]"},
	}
	for _, tt := range tests {
		if got := FormatPromptWithMode("", tt.used, 32768, true); !strings.Contains(got, tt.want) {
			t.Errorf("FormatPromptWithMode(%d) = %q, want %q", tt.used, got, tt.want)
		}
	}
}

func TestSandboxedPathBackToTheRootIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{".", "src/.."} {
		rel, abs, err := SandboxedPath(target, "", root)
		if err != nil || rel != "" || abs != root {
			t.Errorf("SandboxedPath(%q) = %q, %q, %v; want the root", target, rel, abs, err)
		}
	}
}
