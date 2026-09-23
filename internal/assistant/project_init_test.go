package assistant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestInitProjectWritesContextPolicyAndKeepsAnExistingPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\tgo build\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() {
		if err := InitProject(dir, "3.0.0-alpha.2"); err != nil {
			t.Fatal(err)
		}
	})
	md, err := os.ReadFile(filepath.Join(dir, "TARACODE.md"))
	if err != nil || !strings.Contains(string(md), "## Build Commands") || !strings.Contains(string(md), "make build") {
		t.Fatalf("TARACODE.md %q %v", md, err)
	}
	polPath := filepath.Join(dir, ".taracode", "policy.yaml")
	data, err := os.ReadFile(polPath)
	if err != nil || string(data) != policy.StarterYAML {
		t.Fatalf("policy %v", err)
	}
	if err := os.WriteFile(polPath, []byte("version: 1\nmode: operate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { _ = InitProject(dir, "3.0.0-alpha.2") })
	if data, _ := os.ReadFile(polPath); string(data) != "version: 1\nmode: operate\n" {
		t.Fatal("an existing policy must not be overwritten")
	}
	cfg, err := os.ReadFile(filepath.Join(dir, ".taracode", "project.json"))
	if err != nil || !strings.Contains(string(cfg), `"version": "3.0.0-alpha.2"`) {
		t.Fatalf("project.json %q %v", cfg, err)
	}
}
