package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpandFileReferencesWithImagesWorksBeforeInit is the fix-round regression for a review
// finding: expandFileReferencesWithImages used to gate on isInitializedProject(projectRoot) and
// silently pass @refs through to the model unexpanded, with zero feedback, whenever the project had
// no .taracode/ at all. Reading a file (or an image) off disk needs no .taracode/, so the gate is
// gone; this proves both the text-file and the image-file expansion paths still work in a genuinely
// uninitialised directory. It calls expandFileReferencesWithImages directly (the function
// cmd.expandReferences calls), not through the REPL, since no assistant or readline instance is
// needed to exercise it.
func TestExpandFileReferencesWithImagesWorksBeforeInit(t *testing.T) {
	dir := t.TempDir() // genuinely uninitialised: no .taracode/ anywhere under dir
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello from disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	// LoadImage only reads and base64-encodes bytes (internal/agent/image.go); it does not
	// decode or validate image content, so arbitrary bytes with a recognised extension are enough
	// to exercise the image branch without a real PNG encoder.
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), []byte("not-real-png-bytes-are-enough"), 0o644); err != nil {
		t.Fatal(err)
	}

	textResult, err := expandFileReferencesWithImages("look at @hello.txt please", dir)
	if err != nil {
		t.Fatalf("expandFileReferencesWithImages() error = %v", err)
	}
	if !strings.Contains(textResult.Text, "hello from disk") {
		t.Fatalf("expanded text = %q, want the file's content even though %s has no .taracode/", textResult.Text, dir)
	}

	imgResult, err := expandFileReferencesWithImages("describe @pic.png", dir)
	if err != nil {
		t.Fatalf("expandFileReferencesWithImages() error = %v", err)
	}
	wantPath := filepath.Join(dir, "pic.png")
	if len(imgResult.Images) != 1 || imgResult.Images[0].Path != wantPath {
		t.Fatalf("images = %+v, want pic.png loaded even though %s has no .taracode/", imgResult.Images, dir)
	}
}

// TestFileCompleterDoWorksBeforeInit covers the Tab-completion gate the fix round also relaxed:
// FileCompleter.Do only needs to walk a real directory on disk, so it now offers @ completions
// before /init too, instead of returning nothing whenever .taracode/ was absent.
func TestFileCompleterDoWorksBeforeInit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fc := NewFileCompleter(dir)
	line := []rune("@hel")
	matches, _ := fc.Do(line, len(line))
	found := false
	for _, m := range matches {
		if strings.Contains(string(m), "hello.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Do() = %v, want a completion for hello.go even though %s has no .taracode/", matches, dir)
	}
}
