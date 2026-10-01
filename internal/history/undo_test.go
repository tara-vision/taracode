package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// undoFixture records a write with a backup and a delete with its content, both on real files.
func undoFixture(t *testing.T) (*Manager, string) {
	t.Helper()
	m, _ := newTestManager(t)
	dir := t.TempDir()
	target, backup := filepath.Join(dir, "app.txt"), filepath.Join(dir, "app.txt.bak")
	writeTestFile(t, target, "new\n")
	writeTestFile(t, backup, "old\n")
	for _, op := range []Operation{
		{Tool: "write_file", Type: OpTypeWrite, Target: target, BackupPath: backup, Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Target: filepath.Join(dir, "sub", "gone.txt"), DeletedContent: "bye\n",
			Success: true},
	} {
		if err := m.Record(op); err != nil {
			t.Fatal(err)
		}
	}
	return m, dir
}

func TestUndoRevertsTheMostRecentOperation(t *testing.T) {
	m, dir := undoFixture(t)
	res, err := m.Undo()
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "sub", "gone.txt")
	if !res.Success || res.OperationID != 2 || res.Tool != "delete_file" || res.Message != "Reverted delete_file on "+gone {
		t.Fatalf("result %+v", res)
	}
	if got := readTestFile(t, gone); got != "bye\n" {
		t.Fatalf("recreated %q", got)
	}
	res, err = m.Undo()
	if err != nil || res.OperationID != 1 || res.RestoredFrom != filepath.Join(dir, "app.txt.bak") {
		t.Fatalf("result %+v err=%v", res, err)
	}
	if got := readTestFile(t, filepath.Join(dir, "app.txt")); got != "old\n" {
		t.Fatalf("restored %q", got)
	}
	ops := m.GetAllHistory()
	if !ops[0].Undone || ops[0].UndoneAt == nil || !ops[1].Undone {
		t.Fatalf("both operations are marked undone: %+v", ops)
	}
	reloaded, err := NewManager(m.rootDir, m.sessionID)
	if err != nil || !reloaded.GetAllHistory()[0].Undone {
		t.Fatalf("the undo is saved: %v", err)
	}
	if _, err := m.Undo(); err == nil || err.Error() != "nothing to undo" {
		t.Fatalf("err = %v", err)
	}
}

func TestUndoReportsAFailure(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.RecordOperation("write_file", nil, filepath.Join(t.TempDir(), "x.txt"), true, "ok", ""); err != nil {
		t.Fatal(err)
	}
	res, err := m.Undo()
	if err == nil || res == nil || res.Success || !strings.Contains(res.Message, "no backup available for") {
		t.Fatalf("result %+v err=%v", res, err)
	}
	if m.GetAllHistory()[0].Undone {
		t.Fatal("a failed undo does not mark the operation undone")
	}
}

func TestUndoNRevertsSeveral(t *testing.T) {
	m, dir := undoFixture(t)
	results, err := m.UndoN(5)
	if err != nil || len(results) != 2 || results[0].Tool != "delete_file" || results[1].Tool != "write_file" {
		t.Fatalf("results %+v err=%v", results, err)
	}
	if got := readTestFile(t, filepath.Join(dir, "app.txt")); got != "old\n" {
		t.Fatalf("restored %q", got)
	}
	if _, err := m.UndoN(1); err == nil || err.Error() != "nothing to undo" {
		t.Fatalf("err = %v", err)
	}
}

// TestUndoNReportsAFailureInItsResults: a failed undo is a result, not an error.
func TestUndoNReportsAFailureInItsResults(t *testing.T) {
	m, _ := newTestManager(t)
	target := filepath.Join(t.TempDir(), "x.txt")
	if err := m.RecordOperation("write_file", nil, target, true, "ok", ""); err != nil {
		t.Fatal(err)
	}
	results, err := m.UndoN(1)
	if err != nil || len(results) != 1 || results[0].Success || results[0].OperationID != 1 ||
		results[0].Message != "no backup available for "+target {
		t.Fatalf("results %+v err=%v", results, err)
	}
}

func TestUndoDryRunDescribesWithoutChanging(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.UndoDryRun(1); err == nil || err.Error() != "nothing to undo" {
		t.Fatalf("err = %v", err)
	}
	for _, op := range []Operation{
		{Tool: "write_file", Type: OpTypeWrite, Target: "/w-backup", BackupPath: "/b/w.1", Success: true},
		{Tool: "edit_file", Type: OpTypeEdit, Target: "/e-nobackup", Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Target: "/d-content", DeletedContent: "x", Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Target: "/d-empty", Success: true},
		{Tool: "move_file", Type: OpTypeMove, Target: "/m-new", OriginalPath: "/m-old", Success: true},
		{Tool: "copy_file", Type: OpTypeCopy, Target: "/c-copy", CreatedPath: "/c-created", Success: true},
		{Tool: "execute_command", Type: OpTypeExecute, Target: "ls", Success: true},
	} {
		if err := m.Record(op); err != nil {
			t.Fatal(err)
		}
	}
	results, err := m.UndoDryRun(10)
	if err != nil || len(results) != 6 {
		t.Fatalf("results %+v err=%v", results, err)
	}
	want := []struct {
		message string
		success bool
	}{
		{"Would delete copied file /c-created", true},
		{"Would move /m-new back to /m-old", true},
		{"Would recreate /d-empty (content not captured)", false},
		{"Would recreate /d-content", true},
		{"Would restore /e-nobackup (no backup available)", false},
		{"Would restore /w-backup from backup", true},
	}
	for i, w := range want {
		if results[i].Message != w.message || results[i].Success != w.success {
			t.Errorf("result %d = %+v, want %q success=%v", i, results[i], w.message, w.success)
		}
	}
	if results[5].RestoredFrom != "/b/w.1" {
		t.Errorf("the restore source: %+v", results[5])
	}
	for _, op := range m.GetAllHistory() {
		if op.Undone {
			t.Fatalf("a dry run marked %+v undone", op)
		}
	}
}

func TestUndoMoveAndCopy(t *testing.T) {
	m, _ := newTestManager(t)
	dir := t.TempDir()
	moved, original := filepath.Join(dir, "moved.txt"), filepath.Join(dir, "back", "original.txt")
	writeTestFile(t, moved, "moved content")
	copied, created := filepath.Join(dir, "copy.txt"), filepath.Join(dir, "created.txt")
	writeTestFile(t, copied, "copy")
	writeTestFile(t, created, "created")
	for _, op := range []Operation{
		{Tool: "move_file", Type: OpTypeMove, Target: moved, OriginalPath: original, Success: true},
		{Tool: "copy_file", Type: OpTypeCopy, Target: copied, Success: true},
		{Tool: "copy_file", Type: OpTypeCopy, Target: copied, CreatedPath: created, Success: true},
		{Tool: "copy_file", Type: OpTypeCopy, Target: filepath.Join(dir, "already-gone.txt"), Success: true},
	} {
		if err := m.Record(op); err != nil {
			t.Fatal(err)
		}
	}
	results, err := m.UndoN(4)
	if err != nil || len(results) != 4 {
		t.Fatalf("results %+v err=%v", results, err)
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("undo failed: %+v", r)
		}
	}
	for _, path := range []string{created, copied, moved} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", path, err)
		}
	}
	if got := readTestFile(t, original); got != "moved content" {
		t.Fatalf("moved back: %q", got)
	}
}

func TestUndoReportsEachKindOfFailure(t *testing.T) {
	dir := t.TempDir()
	aFile := filepath.Join(dir, "a-file")
	writeTestFile(t, aFile, "x")
	backup := filepath.Join(dir, "backup.txt")
	writeTestFile(t, backup, "old")
	aDir := filepath.Join(dir, "a-dir")
	if err := os.MkdirAll(filepath.Join(aDir, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		op      Operation
		wantErr string
	}{
		{"backup unreadable", Operation{Tool: "write_file", Type: OpTypeWrite, Target: aFile,
			BackupPath: filepath.Join(dir, "missing.bak")}, "failed to read backup"},
		{"restore fails", Operation{Tool: "edit_file", Type: OpTypeEdit, Target: filepath.Join(dir, "no-dir", "x.txt"),
			BackupPath: backup}, "failed to restore"},
		{"delete without content", Operation{Tool: "delete_file", Type: OpTypeDelete, Target: aFile},
			"deleted content not captured"},
		{"delete parent is a file", Operation{Tool: "delete_file", Type: OpTypeDelete, Target: filepath.Join(aFile, "x.txt"),
			DeletedContent: "x"}, "failed to create directory"},
		{"delete target is a directory", Operation{Tool: "delete_file", Type: OpTypeDelete, Target: aDir,
			DeletedContent: "x"}, "failed to recreate"},
		{"move without original", Operation{Tool: "move_file", Type: OpTypeMove, Target: aFile},
			"original path not recorded"},
		{"move parent is a file", Operation{Tool: "move_file", Type: OpTypeMove, Target: aFile,
			OriginalPath: filepath.Join(aFile, "x.txt")}, "failed to create directory"},
		{"move source gone", Operation{Tool: "move_file", Type: OpTypeMove, Target: filepath.Join(dir, "gone.txt"),
			OriginalPath: filepath.Join(dir, "back.txt")}, "failed to move"},
		{"copy is a full directory", Operation{Tool: "copy_file", Type: OpTypeCopy, Target: aDir},
			"failed to remove copied file"},
	}
	for _, tt := range tests {
		m, _ := newTestManager(t)
		tt.op.Success = true
		if err := m.Record(tt.op); err != nil {
			t.Fatal(err)
		}
		res, err := m.Undo()
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) || res == nil || res.Success {
			t.Errorf("%s: result %+v err=%v, want %q", tt.name, res, err, tt.wantErr)
		}
	}
}

func TestUndoReportsAHistoryThatCannotBeSaved(t *testing.T) {
	skipIfRoot(t)
	m, root := newTestManager(t)
	target := filepath.Join(t.TempDir(), "gone.txt")
	if err := m.Record(Operation{Tool: "delete_file", Type: OpTypeDelete, Target: target, DeletedContent: "bye",
		Success: true}); err != nil {
		t.Fatal(err)
	}
	chmod(t, filepath.Join(root, "history", "operations_test-session.json"), 0o400)
	if res, err := m.Undo(); err == nil || !strings.Contains(err.Error(), "failed to save history after undo") || res != nil {
		t.Fatalf("result %+v err=%v", res, err)
	}
	if got := readTestFile(t, target); got != "bye" {
		t.Fatalf("the file itself was recreated: %q", got)
	}
}

// TestUndoPassesOverAnOperationThatFailed: an operation recorded as failed changed nothing, so the
// dry run, Undo and UndoN all pass over it to the operations that succeeded.
func TestUndoPassesOverAnOperationThatFailed(t *testing.T) {
	m, dir := undoFixture(t)
	failed, backup := filepath.Join(dir, "failed.txt"), filepath.Join(dir, "failed.txt.bak")
	writeTestFile(t, failed, "untouched\n")
	writeTestFile(t, backup, "from the backup\n")
	if err := m.Record(Operation{Tool: "write_file", Type: OpTypeWrite, Target: failed, BackupPath: backup,
		Success: false}); err != nil {
		t.Fatal(err)
	}
	if preview, err := m.UndoDryRun(1); err != nil || len(preview) != 1 || preview[0].OperationID != 2 {
		t.Fatalf("dry run %+v err=%v", preview, err)
	}
	if res, err := m.Undo(); err != nil || res.OperationID != 2 {
		t.Fatalf("undo %+v err=%v", res, err)
	}
	if results, err := m.UndoN(2); err != nil || len(results) != 1 || results[0].OperationID != 1 {
		t.Fatalf("undo of two with one left %+v err=%v", results, err)
	}
	if got := readTestFile(t, failed); got != "untouched\n" || m.GetAllHistory()[2].Undone {
		t.Fatalf("the failed operation was undone: %q", got)
	}
}
