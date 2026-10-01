package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFixturesReportsAnIndexItCannotRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fixtures", "index.yaml"), 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := LoadFixtures(dir); err == nil || !strings.Contains(err.Error(), "index.yaml") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveReportsAFixturesDirectoryItCannotCreate(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures"), []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get pods", "x", false); err == nil {
		t.Fatal("fixtures is a file")
	}
}

// TestSaveMovesAHandAuthoredFixtureToItsOwnName: re-saving a signature whose hand-authored file has
// another name writes the conventional name and removes the old file, which nothing else uses.
func TestSaveMovesAHandAuthoredFixtureToItsOwnName(t *testing.T) {
	dir := t.TempDir()
	fixtures := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixtures, 0o755); err != nil {
		t.Fatal(err)
	}
	const sig = "kubectl get pods"
	files := map[string]string{"index.yaml": "fixtures:\n  - {signature: kubectl get pods, file: pods.txt}\n", "pods.txt": "old"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(fixtures, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sig, "new", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixtures, "pods.txt")); !os.IsNotExist(err) {
		t.Fatalf("the old file is removed: %v", err)
	}
	reloaded, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out, _, ok := reloaded.Lookup(sig); !ok || out != "new" || reloaded.Len() != 1 ||
		reloaded.Fixtures()[0].File != fixtureFileName(sig) {
		t.Fatalf("%q %v %+v", out, ok, reloaded.Fixtures())
	}
}
