package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/history"
)

// diffFixture records one change of every kind /diff renders, on real files in dir.
func diffFixture(t *testing.T) (*history.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	hm := newHistory(t)
	modified, backup := filepath.Join(dir, "app.txt"), filepath.Join(dir, "app.txt.bak")
	writeFile(t, modified, "a\nB\nc\n")
	writeFile(t, backup, "a\nb\nc\n")
	created := filepath.Join(dir, "new.txt")
	writeFile(t, created, "fresh\n")
	record(t, hm, history.Operation{Tool: "edit_file", Target: modified, BackupPath: backup, Success: true})
	record(t, hm, history.Operation{Tool: "write_file", Target: created, Success: true})
	record(t, hm, history.Operation{Tool: "delete_file", Target: filepath.Join(dir, "gone.txt"), DeletedContent: "bye\n",
		Success: true})
	record(t, hm, history.Operation{Tool: "move_file", Target: filepath.Join(dir, "moved.txt"),
		OriginalPath: filepath.Join(dir, "orig.txt"), Success: true})
	record(t, hm, history.Operation{Tool: "copy_file", Target: filepath.Join(dir, "copy.txt"),
		OriginalPath: filepath.Join(dir, "src.txt"), Success: true})
	return hm, dir
}

func TestDiffShowsEveryChange(t *testing.T) {
	hm := newHistory(t)
	out := captureStdoutForTest(t, func() { handleDiff(hm, nil, t.TempDir()) })
	if !strings.Contains(out, "No file changes in this session.") {
		t.Fatalf("%q", out)
	}

	hm, dir := diffFixture(t)
	out = captureStdoutForTest(t, func() { handleDiff(hm, nil, dir) })
	app := filepath.Join(dir, "app.txt")
	for _, want := range []string{
		"File changes in session (5 files):",
		"\033[33m[modified]\033[0m " + app, "\033[1m--- a/" + app + "\033[0m", "\033[1m+++ b/" + app + "\033[0m",
		"\033[36m@@ -2,2 +2,2 @@\033[0m", "\033[31m-b\033[0m", "\033[32m+B\033[0m", "\n c\n",
		"\033[32m[created]\033[0m " + filepath.Join(dir, "new.txt"), "\033[32m+fresh\033[0m",
		"\033[31m[deleted]\033[0m " + filepath.Join(dir, "gone.txt"), "\033[31m-bye\033[0m",
		"\033[34m[moved]\033[0m " + filepath.Join(dir, "moved.txt"), "  (from " + filepath.Join(dir, "orig.txt") + ")",
		"rename from " + filepath.Join(dir, "orig.txt"),
		"\033[34m[copied]\033[0m " + filepath.Join(dir, "copy.txt"),
		"Total: 5 file(s) changed", "Export with: /diff export",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/diff lacks %q:\n%s", want, out)
		}
	}
}

func TestDiffExportWritesAPatch(t *testing.T) {
	empty := newHistory(t)
	out := captureStdoutForTest(t, func() { handleDiff(empty, []string{"export"}, t.TempDir()) })
	if !strings.Contains(out, "No file changes to export.") {
		t.Fatalf("%q", out)
	}

	dir := t.TempDir()
	hm := newHistory(t)
	modified, backup := filepath.Join(dir, "app.txt"), filepath.Join(dir, "app.txt.bak")
	writeFile(t, modified, "a\nB\nc\n")
	writeFile(t, backup, "a\nb\nc\n")
	record(t, hm, history.Operation{Tool: "edit_file", Target: modified, BackupPath: backup, Success: true})
	record(t, hm, history.Operation{Tool: "delete_file", Target: filepath.Join(dir, "gone.txt"), DeletedContent: "bye\n",
		Success: true})
	out = captureStdoutForTest(t, func() { handleDiff(hm, []string{"export"}, dir) })
	patches, err := filepath.Glob(filepath.Join(dir, "session-*.patch"))
	if err != nil || len(patches) != 1 {
		t.Fatalf("patch files %v err=%v", patches, err)
	}
	if !strings.Contains(out, "Exported 2 file change(s) to: "+filepath.Base(patches[0])) {
		t.Fatalf("%q", out)
	}
	patch := readFile(t, patches[0])
	for _, want := range []string{"# modified: " + modified, "-b\n+B\n", "# deleted: " + filepath.Join(dir, "gone.txt")} {
		if !strings.Contains(patch, want) {
			t.Errorf("patch lacks %q:\n%s", want, patch)
		}
	}

	out = captureStdoutForTest(t, func() { handleDiff(hm, []string{"export"}, filepath.Join(dir, "missing")) })
	if !strings.Contains(out, "Error writing patch file:") {
		t.Fatalf("%q", out)
	}
}

// TestFileCommandsRunOnTheREPLHistory goes through dispatch: /history, /undo and /diff use the
// project's history manager, and /diff export writes into the current directory.
func TestFileCommandsRunOnTheREPLHistory(t *testing.T) {
	r, _ := projectREPL(t)
	target := filepath.Join(r.projectRoot, "app.txt")
	backup := filepath.Join(r.projectRoot, "app.txt.bak")
	writeFile(t, target, "two\n")
	writeFile(t, backup, "one\n")
	record(t, r.history, history.Operation{Tool: "write_file", Target: target, BackupPath: backup, Success: true})

	if out := captureStdoutForTest(t, func() { r.dispatch("/history") }); !strings.Contains(out, "write_file") {
		t.Fatalf("/history: %q", out)
	}
	if out := captureStdoutForTest(t, func() { r.dispatch("/diff") }); !strings.Contains(out, "\033[32m+two\033[0m") {
		t.Fatalf("/diff: %q", out)
	}
	_ = captureStdoutForTest(t, func() { r.dispatch("/diff export") })
	if patches, _ := filepath.Glob(filepath.Join(r.absDir, "session-*.patch")); len(patches) != 1 {
		t.Fatalf("/diff export wrote %v into %s", patches, r.absDir)
	}
	if out := captureStdoutForTest(t, func() { r.dispatch("/undo") }); !strings.Contains(out, "Reverted write_file") {
		t.Fatalf("/undo: %q", out)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "one\n" {
		t.Fatalf("after /undo: %q %v", data, err)
	}
}
