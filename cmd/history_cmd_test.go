package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/ui"
)

// newHistory is a history manager in a fresh .taracode directory.
func newHistory(t *testing.T) *history.Manager {
	t.Helper()
	hm, err := history.NewManager(filepath.Join(t.TempDir(), ".taracode"), "s1")
	if err != nil {
		t.Fatal(err)
	}
	return hm
}

func record(t *testing.T, hm *history.Manager, op history.Operation) {
	t.Helper()
	if op.Timestamp.IsZero() {
		op.Timestamp = time.Date(2026, 10, 1, 9, 30, 15, 0, time.Local)
	}
	if op.Type == "" {
		op.Type = history.ToolToOperationType(op.Tool)
	}
	if err := hm.Record(op); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestHistoryCommandsNeedHistory(t *testing.T) {
	out := captureStdoutForTest(t, func() {
		handleHistory(nil, nil)
		handleUndo(nil, nil)
		handleDiff(nil, nil, t.TempDir())
	})
	if strings.Count(out, "History tracking not available.") != 3 {
		t.Fatalf("%q", out)
	}
}

func TestHistoryListsTheOperations(t *testing.T) {
	hm := newHistory(t)
	out := captureStdoutForTest(t, func() { handleHistory(hm, nil) })
	if !strings.Contains(out, "No operations recorded in this session.") {
		t.Fatalf("%q", out)
	}
	longTarget := "/srv/" + strings.Repeat("x", 40) + "/values.yaml"
	record(t, hm, history.Operation{Tool: "write_file", Target: "/srv/app.txt", Success: true, BackupPath: "/b/app.txt.1"})
	record(t, hm, history.Operation{Tool: "edit_file", Target: longTarget, Success: false})
	record(t, hm, history.Operation{Tool: "delete_file", Target: "/srv/old.txt", Success: true, Undone: true,
		BackupPath: "/b/old.txt.1"})

	out = captureStdoutForTest(t, func() { handleHistory(hm, nil) })
	for _, want := range []string{
		"Operation History",
		"#1   " + ui.IconSuccess + " write_file     /srv/app.txt", "\u2514\u2500 backup: app.txt.1",
		"#2   " + ui.IconError + " edit_file      ..." + longTarget[len(longTarget)-32:],
		"#3   \u21a9 delete_file    /srv/old.txt", // the undone mark
		"Total: 3  |  Undoable: 1  |  Undone: 1",
		"Use /undo to revert the last file modification.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/history lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old.txt.1") {
		t.Errorf("an undone operation shows no backup:\n%s", out)
	}

	out = captureStdoutForTest(t, func() { handleHistory(hm, []string{"1"}) })
	if strings.Contains(out, "#1 ") || strings.Contains(out, "#2 ") || !strings.Contains(out, "#3 ") {
		t.Errorf("/history 1 shows the last operation only:\n%s", out)
	}
	for _, args := range [][]string{{"all"}, {"bogus"}} {
		out = captureStdoutForTest(t, func() { handleHistory(hm, args) })
		if !strings.Contains(out, "#1 ") || !strings.Contains(out, "#3 ") {
			t.Errorf("/history %v shows every operation:\n%s", args, out)
		}
	}
}

func TestHistoryWithoutAnythingToUndo(t *testing.T) {
	hm := newHistory(t)
	record(t, hm, history.Operation{Tool: "read_file", Target: "/srv/a.txt", Success: true})
	out := captureStdoutForTest(t, func() { handleHistory(hm, nil) })
	if strings.Contains(out, "Use /undo") {
		t.Errorf("nothing undoable, no /undo hint:\n%s", out)
	}
}

// undoFixture records a write with a backup and a delete with captured content, both real files.
func undoFixture(t *testing.T) (*history.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	hm := newHistory(t)
	target, backup := filepath.Join(dir, "app.txt"), filepath.Join(dir, "app.txt.bak")
	writeFile(t, target, "new\n")
	writeFile(t, backup, "old\n")
	record(t, hm, history.Operation{Tool: "write_file", Target: target, BackupPath: backup, Success: true})
	record(t, hm, history.Operation{Tool: "delete_file", Target: filepath.Join(dir, "gone.txt"), DeletedContent: "bye\n",
		Success: true})
	return hm, dir
}

func TestUndoDryRunChangesNothing(t *testing.T) {
	hm := newHistory(t)
	out := captureStdoutForTest(t, func() { handleUndo(hm, []string{"--dry-run"}) })
	if !strings.Contains(out, "Nothing to undo: nothing to undo") {
		t.Fatalf("%q", out)
	}

	hm, dir := undoFixture(t)
	record(t, hm, history.Operation{Tool: "write_file", Target: filepath.Join(dir, "nobackup.txt"), Success: true})
	out = captureStdoutForTest(t, func() { handleUndo(hm, []string{"3", "--dry-run"}) })
	for _, want := range []string{
		"Dry run - would undo the following operations:",
		ui.IconError + " #3 write_file: Would restore " + filepath.Join(dir, "nobackup.txt") + " (no backup available)",
		ui.IconSuccess + " #2 delete_file: Would recreate " + filepath.Join(dir, "gone.txt"),
		ui.IconSuccess + " #1 write_file: Would restore " + filepath.Join(dir, "app.txt") + " from backup",
		"Run /undo without --dry-run to apply changes.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s", want, out)
		}
	}
	if got := readFile(t, filepath.Join(dir, "app.txt")); got != "new\n" {
		t.Fatalf("a dry run changed the file: %q", got)
	}
}

func TestUndoRevertsTheLastOperation(t *testing.T) {
	hm, dir := undoFixture(t)
	out := captureStdoutForTest(t, func() { handleUndo(hm, nil) })
	if !strings.Contains(out, ui.IconSuccess+" Reverted delete_file on gone.txt") || strings.Contains(out, "Restored from") {
		t.Fatalf("%q", out)
	}
	if got := readFile(t, filepath.Join(dir, "gone.txt")); got != "bye\n" {
		t.Fatalf("the deleted file was not recreated: %q", got)
	}
	out = captureStdoutForTest(t, func() { handleUndo(hm, []string{"1"}) })
	if !strings.Contains(out, ui.IconSuccess+" Reverted write_file on app.txt") || !strings.Contains(out, "Restored from: app.txt.bak") {
		t.Fatalf("%q", out)
	}
	if got := readFile(t, filepath.Join(dir, "app.txt")); got != "old\n" {
		t.Fatalf("the file was not restored: %q", got)
	}
	out = captureStdoutForTest(t, func() { handleUndo(hm, nil) })
	if !strings.Contains(out, "Cannot undo: nothing to undo") {
		t.Fatalf("%q", out)
	}
}

func TestUndoSeveralOperations(t *testing.T) {
	hm := newHistory(t)
	out := captureStdoutForTest(t, func() { handleUndo(hm, []string{"2"}) })
	if !strings.Contains(out, "Cannot undo: nothing to undo") {
		t.Fatalf("%q", out)
	}

	hm, dir := undoFixture(t)
	out = captureStdoutForTest(t, func() { handleUndo(hm, []string{"5"}) })
	for _, want := range []string{
		"Undoing 2 operations:", "  " + ui.IconSuccess + " Reverted delete_file on gone.txt", "  " + ui.IconSuccess + " Reverted write_file on app.txt",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/undo 5 lacks %q:\n%s", want, out)
		}
	}
	if got := readFile(t, filepath.Join(dir, "app.txt")); got != "old\n" {
		t.Fatalf("the write was not reverted: %q", got)
	}
}

// TestUndoSeveralShowsAFailure checks how /undo shows a failed result.
func TestUndoSeveralShowsAFailure(t *testing.T) {
	hm, dir := undoFixture(t)
	missing := filepath.Join(dir, "nobackup.txt")
	record(t, hm, history.Operation{Tool: "write_file", Target: missing, Success: true})
	out := captureStdoutForTest(t, func() { handleUndo(hm, []string{"2"}) })
	if !strings.Contains(out, "Undoing 2 operations:") ||
		!strings.Contains(out, "  "+ui.IconError+" Failed: write_file - no backup available for "+missing) {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { handleUndo(hm, nil) })
	if !strings.Contains(out, "Cannot undo: no backup available for "+missing) {
		t.Fatalf("a single /undo reports the failure: %q", out)
	}
}
