package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manifoldco/promptui"
)

// answerPicker makes the promptui list return the item at index, or err, and keeps a copy of what
// the list was asked to show.
func answerPicker(t *testing.T, index int, err error) *promptui.Select {
	t.Helper()
	shown := &promptui.Select{}
	original := runSelect
	runSelect = func(s *promptui.Select) (int, string, error) {
		*shown = *s
		if err != nil {
			return 0, "", err
		}
		items, _ := s.Items.([]string)
		return index, items[index], nil
	}
	t.Cleanup(func() { runSelect = original })
	return shown
}

func TestSelectFileOffersOnlyFiles(t *testing.T) {
	if _, err := selectFile(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no files found in directory") {
		t.Fatalf("an empty directory: %v", err)
	}

	dir := t.TempDir()
	writeTree(t, dir, "README.md", "src/Main.go")
	shown := answerPicker(t, 1, nil)
	got, err := selectFile(dir)
	if err != nil || got != "src/Main.go" {
		t.Fatalf("selectFile() = %q, %v", got, err)
	}
	if items, _ := shown.Items.([]string); strings.Join(items, ",") != "README.md,src/Main.go" ||
		!shown.StartInSearchMode || shown.Label != "Select a file" {
		t.Fatalf("the picker showed %+v", shown)
	}
	if !shown.Searcher("main", 1) || !shown.Searcher("MAIN", 1) || shown.Searcher("main", 0) {
		t.Fatal("the search is a case-insensitive substring match on the path")
	}

	answerPicker(t, 0, promptui.ErrInterrupt)
	if _, err := selectFile(dir); !errors.Is(err, promptui.ErrInterrupt) {
		t.Fatalf("a cancelled picker: %v", err)
	}
}

func TestExpandDirectoryReference(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, "conf/a.yaml", "conf/LICENSE", "conf/locked.txt")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	skipIfRoot(t)
	locked := filepath.Join(root, "conf", "locked.txt")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })

	got, err := expandDirectoryReference("conf", root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"**Directory: `conf`** (3 files)",
		"**File: `conf/LICENSE`**\n```\ncontent of conf/LICENSE\n```",
		"**File: `conf/a.yaml`**\n```yaml\ncontent of conf/a.yaml\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expansion lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "locked.txt`**") {
		t.Errorf("an unreadable file is left out:\n%s", got)
	}

	got, err = expandDirectoryReference("empty", root)
	if err != nil || !strings.Contains(got, "**Directory: `empty`** (empty or no readable files)") {
		t.Fatalf("an empty directory: %q, %v", got, err)
	}
}

// TestExpandFileReferences covers the text-only expansion (unused by the REPL since images were
// added, but still compiled in): files, directories, punctuation after a path and missing paths.
func TestExpandFileReferences(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "main.go", "conf/a.yaml")
	tests := []struct {
		message string
		want    []string
		wantErr string
	}{
		{"no references here", []string{"no references here"}, ""},
		{"what does @main.go? do", []string{"what does \n\n**File: `main.go`**\n```go\ncontent of main.go\n```"}, ""},
		{"compare @main.go and @conf", []string{"**File: `main.go`**", " and \n\n**Directory: `conf`** (1 files)"}, ""},
		{"read @missing.txt", nil, "failed to access missing.txt"},
	}
	for _, tt := range tests {
		got, err := expandFileReferences(tt.message, dir)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%q: err = %v, want %q", tt.message, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.message, err)
			continue
		}
		for _, want := range tt.want {
			if !strings.Contains(got, want) {
				t.Errorf("%q: expansion lacks %q:\n%s", tt.message, want, got)
			}
		}
	}
}

// TestAStandaloneAtOpensThePicker covers "@" with no path after it, in both expansions: the picker
// chooses the file, or its cancellation becomes the error.
func TestAStandaloneAtOpensThePicker(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "picked.txt")
	answerPicker(t, 0, nil)
	var text string
	var expanded *ExpandedMessage
	var err, errImages error
	out := captureStdoutForTest(t, func() {
		text, err = expandFileReferences("explain @", dir)
		expanded, errImages = expandFileReferencesWithImages("explain @", dir)
	})
	if err != nil || errImages != nil {
		t.Fatalf("errors %v %v", err, errImages)
	}
	if !strings.Contains(text, "**File: `picked.txt`**") || !strings.Contains(expanded.Text, "content of picked.txt") {
		t.Fatalf("expansions %q and %q", text, expanded.Text)
	}
	if strings.Count(out, "Select a file (or use Tab after @ for completion)") != 2 {
		t.Fatalf("the picker prompt: %q", out)
	}

	answerPicker(t, 0, promptui.ErrInterrupt)
	_ = captureStdoutForTest(t, func() {
		_, err = expandFileReferences("explain @", dir)
		_, errImages = expandFileReferencesWithImages("explain @", dir)
	})
	for _, e := range []error{err, errImages} {
		if e == nil || !strings.Contains(e.Error(), "file selection cancelled") {
			t.Errorf("a cancelled picker: %v", e)
		}
	}
}

func TestExpandFileReferencesWithImagesHandlesEveryKind(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "notes.txt", "conf/a.yaml", "pic.png")
	var got *ExpandedMessage
	var err error
	out := captureStdoutForTest(t, func() {
		got, err = expandFileReferencesWithImages("see @notes.txt, @conf and @pic.png!", dir)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"see \n\n**File: `notes.txt`**\n```txt\ncontent of notes.txt\n```", "**Directory: `conf`**",
		"\n\n[Image: pic.png]\n"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("expansion lacks %q:\n%s", want, got.Text)
		}
	}
	if len(got.Images) != 1 || got.Images[0].MimeType != "image/png" || !strings.Contains(out, "Loaded image: pic.png") {
		t.Fatalf("images %+v, output %q", got.Images, out)
	}
	if plain, err := expandFileReferencesWithImages("no references", dir); err != nil || plain.Text != "no references" ||
		plain.Images != nil {
		t.Fatalf("a message without @ passes through: %+v, %v", plain, err)
	}
	if _, err := expandFileReferencesWithImages("see @gone.txt", dir); err == nil ||
		!strings.Contains(err.Error(), "failed to access gone.txt") {
		t.Fatalf("a missing file: %v", err)
	}
}

func TestExpandFileReferencesReportsUnreadableFiles(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	writeTree(t, dir, "secret.txt", "secret.png")
	for _, name := range []string{"secret.txt", "secret.png"} {
		path := filepath.Join(dir, name)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	}
	tests := []struct {
		name    string
		expand  func() error
		wantErr string
	}{
		{"text", func() error { _, err := expandFileReferences("@secret.txt", dir); return err }, "failed to read file secret.txt"},
		{"text with images", func() error { _, err := expandFileReferencesWithImages("@secret.txt", dir); return err },
			"failed to read file secret.txt"},
		{"image", func() error { _, err := expandFileReferencesWithImages("@secret.png", dir); return err },
			"failed to load image secret.png"},
	}
	for _, tt := range tests {
		if err := tt.expand(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}
