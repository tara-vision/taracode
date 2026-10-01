package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestManager is a manager on a fresh .taracode directory.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".taracode")
	m, err := NewManager(root, "test-session")
	if err != nil {
		t.Fatal(err)
	}
	return m, root
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

// chmod sets mode on path and restores 0o755 when the test ends.
func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

func TestNewManagerFailsWhenTheHistoryDirectoryCannotBeMade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "file")
	writeTestFile(t, root, "not a directory")
	if _, err := NewManager(root, "s"); err == nil || !strings.Contains(err.Error(), "failed to create history directory") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewManagerFailsOnAnUnreadableHistory(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".taracode")
	// The history file is a directory: reading it fails with something other than "not found".
	if err := os.MkdirAll(filepath.Join(root, "history", "operations_s.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root, "s"); err == nil || !strings.Contains(err.Error(), "failed to read history file") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewManagerStartsOverOnACorruptHistory(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".taracode")
	if err := os.MkdirAll(filepath.Join(root, "history"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "history", "operations_s.json"), "{not json")
	m, err := NewManager(root, "s")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RecordOperation("write_file", nil, "/a", true, "ok", ""); err != nil {
		t.Fatal(err)
	}
	if ops := m.GetAllHistory(); len(ops) != 1 || ops[0].ID != 1 {
		t.Fatalf("a corrupt file starts a fresh history at ID 1: %+v", ops)
	}
}

func TestRecordReportsASaveError(t *testing.T) {
	m, _ := newTestManager(t)
	err := m.Record(Operation{Tool: "write_file", Params: map[string]interface{}{"bad": make(chan int)}})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal history") {
		t.Fatalf("err = %v", err)
	}

	skipIfRoot(t)
	m, root := newTestManager(t)
	chmod(t, filepath.Join(root, "history"), 0o500)
	if err := m.RecordOperation("write_file", nil, "/a", true, "ok", ""); err == nil {
		t.Fatal("a history that cannot be written must fail the record")
	}
}

func TestGetHistoryWithoutALimitReturnsEverything(t *testing.T) {
	m, _ := newTestManager(t)
	for i := 0; i < 3; i++ {
		if err := m.RecordOperation("write_file", nil, "/a", true, "ok", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{0, -1, 10} {
		if ops := m.GetHistory(limit); len(ops) != 3 || ops[0].ID != 1 {
			t.Errorf("GetHistory(%d) = %+v", limit, ops)
		}
	}
}

func TestGetStatsCountsUndoneOperations(t *testing.T) {
	m, _ := newTestManager(t)
	for _, op := range []Operation{
		{Tool: "write_file", Type: OpTypeWrite, Success: true},
		{Tool: "delete_file", Type: OpTypeDelete, Success: true, Undone: true},
		{Tool: "execute_command", Type: OpTypeExecute, Success: true},
	} {
		if err := m.Record(op); err != nil {
			t.Fatal(err)
		}
	}
	stats := m.GetStats()
	if stats["total"] != 3 || stats["undone"] != 1 || stats["undoable"] != 1 || stats["mutations"] != 2 {
		t.Fatalf("stats %v", stats)
	}
}

func TestCreateBackupCopiesTheFile(t *testing.T) {
	m, root := newTestManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "app.yaml")
	writeTestFile(t, target, "replicas: 2\n")
	backup, err := m.CreateBackup(target)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(backup) != filepath.Join(root, "backups") || !strings.HasPrefix(filepath.Base(backup), "app.yaml.") {
		t.Fatalf("backup path %s", backup)
	}
	if got := readTestFile(t, backup); got != "replicas: 2\n" {
		t.Fatalf("backup content %q", got)
	}

	if backup, err := m.CreateBackup(filepath.Join(dir, "new.yaml")); err != nil || backup != "" {
		t.Fatalf("a file that does not exist yet needs no backup: %q %v", backup, err)
	}
	big := filepath.Join(dir, "big.bin")
	writeTestFile(t, big, "")
	if err := os.Truncate(big, MaxBackupSize+1); err != nil {
		t.Fatal(err)
	}
	if backup, err := m.CreateBackup(big); err != nil || backup != "" {
		t.Fatalf("a file over the limit is not backed up: %q %v", backup, err)
	}
}

func TestCreateBackupReportsFileErrors(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	writeTestFile(t, notADir, "x")
	unreadable := filepath.Join(dir, "secret.txt")
	writeTestFile(t, unreadable, "x")
	chmod(t, unreadable, 0)
	readable := filepath.Join(dir, "plain.txt")
	writeTestFile(t, readable, "x")

	m, _ := newTestManager(t)
	if _, err := m.CreateBackup(filepath.Join(notADir, "child")); err == nil || !strings.Contains(err.Error(), "failed to stat file") {
		t.Errorf("a path through a file: %v", err)
	}
	if _, err := m.CreateBackup(unreadable); err == nil || !strings.Contains(err.Error(), "failed to read file for backup") {
		t.Errorf("an unreadable file: %v", err)
	}

	blocked, root := newTestManager(t)
	writeTestFile(t, filepath.Join(root, "backups"), "a file where the directory goes")
	if _, err := blocked.CreateBackup(readable); err == nil || !strings.Contains(err.Error(), "failed to create backup directory") {
		t.Errorf("no backup directory: %v", err)
	}

	readOnly, root := newTestManager(t)
	if err := os.Mkdir(filepath.Join(root, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	chmod(t, filepath.Join(root, "backups"), 0o500)
	if _, err := readOnly.CreateBackup(readable); err == nil || !strings.Contains(err.Error(), "failed to write backup") {
		t.Errorf("a read-only backup directory: %v", err)
	}
}

func TestCaptureDeletedContent(t *testing.T) {
	m, _ := newTestManager(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "notes.txt")
	writeTestFile(t, target, "keep this")
	if got, err := m.CaptureDeletedContent(target); err != nil || got != "keep this" {
		t.Fatalf("CaptureDeletedContent() = %q, %v", got, err)
	}
	if _, err := m.CaptureDeletedContent(filepath.Join(dir, "absent.txt")); err == nil {
		t.Fatal("a missing file is an error")
	}
	big := filepath.Join(dir, "big.bin")
	writeTestFile(t, big, "")
	if err := os.Truncate(big, MaxBackupSize+1); err != nil {
		t.Fatal(err)
	}
	if got, err := m.CaptureDeletedContent(big); err != nil || got != "" {
		t.Fatalf("a file over the limit is not captured: %q %v", got, err)
	}
	skipIfRoot(t)
	chmod(t, target, 0)
	if _, err := m.CaptureDeletedContent(target); err == nil {
		t.Fatal("an unreadable file is an error")
	}
}
