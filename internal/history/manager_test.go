package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}

	// Verify history directory was created
	historyDir := filepath.Join(tmpDir, "history")
	if _, err := os.Stat(historyDir); os.IsNotExist(err) {
		t.Errorf("History directory was not created")
	}
}

func TestManager_Record(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	op := Operation{
		Timestamp: time.Now(),
		Tool:      "write_file",
		Type:      OpTypeWrite,
		Params:    map[string]interface{}{"file_path": "/test.txt"},
		Target:    "/test.txt",
		Success:   true,
		Result:    "success",
	}

	err = manager.Record(op)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// Verify operation was recorded
	ops := manager.GetHistory(10)
	if len(ops) != 1 {
		t.Errorf("GetHistory() returned %d operations, want 1", len(ops))
	}

	if ops[0].Tool != "write_file" {
		t.Errorf("Recorded operation tool = %v, want write_file", ops[0].Tool)
	}

	if ops[0].ID != 1 {
		t.Errorf("Recorded operation ID = %v, want 1", ops[0].ID)
	}
}

func TestManager_RecordOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.RecordOperation(
		"edit_file",
		map[string]interface{}{"file_path": "/test.txt", "old": "foo", "new": "bar"},
		"/test.txt",
		true,
		"success",
		"/backup/test.txt",
	)
	if err != nil {
		t.Fatalf("RecordOperation() error = %v", err)
	}

	ops := manager.GetHistory(10)
	if len(ops) != 1 {
		t.Errorf("GetHistory() returned %d operations, want 1", len(ops))
	}

	if ops[0].Type != OpTypeEdit {
		t.Errorf("Recorded operation type = %v, want edit", ops[0].Type)
	}

	if ops[0].BackupPath != "/backup/test.txt" {
		t.Errorf("Recorded operation BackupPath = %v, want /backup/test.txt", ops[0].BackupPath)
	}
}

func TestManager_GetHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record 5 operations
	for i := 0; i < 5; i++ {
		err = manager.RecordOperation(
			"write_file",
			map[string]interface{}{"file_path": "/test.txt"},
			"/test.txt",
			true,
			"success",
			"",
		)
		if err != nil {
			t.Fatalf("RecordOperation() error = %v", err)
		}
	}

	// Get last 3
	ops := manager.GetHistory(3)
	if len(ops) != 3 {
		t.Errorf("GetHistory(3) returned %d operations, want 3", len(ops))
	}

	// Verify we get the most recent (IDs 3, 4, 5)
	if ops[0].ID != 3 {
		t.Errorf("First operation ID = %v, want 3", ops[0].ID)
	}
	if ops[2].ID != 5 {
		t.Errorf("Last operation ID = %v, want 5", ops[2].ID)
	}
}

func TestManager_GetAllHistory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record 5 operations
	for i := 0; i < 5; i++ {
		err = manager.RecordOperation(
			"write_file",
			nil,
			"/test.txt",
			true,
			"success",
			"",
		)
		if err != nil {
			t.Fatalf("RecordOperation() error = %v", err)
		}
	}

	ops := manager.GetAllHistory()
	if len(ops) != 5 {
		t.Errorf("GetAllHistory() returned %d operations, want 5", len(ops))
	}
}

func TestManager_GetOperation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.RecordOperation("write_file", nil, "/test.txt", true, "success", "")
	if err != nil {
		t.Fatalf("RecordOperation() error = %v", err)
	}

	op, err := manager.GetOperation(1)
	if err != nil {
		t.Fatalf("GetOperation() error = %v", err)
	}

	if op.ID != 1 {
		t.Errorf("GetOperation(1) returned ID = %v, want 1", op.ID)
	}

	// Test non-existent operation
	_, err = manager.GetOperation(999)
	if err == nil {
		t.Error("GetOperation(999) should return error for non-existent ID")
	}
}

func TestManager_GetUndoableOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record undoable operation (write)
	err = manager.RecordOperation("write_file", nil, "/test.txt", true, "success", "/backup/test.txt")
	if err != nil {
		t.Fatalf("RecordOperation() error = %v", err)
	}

	// Record non-undoable operation (read)
	op := Operation{
		Timestamp: time.Now(),
		Tool:      "read_file",
		Type:      OpTypeRead,
		Target:    "/test.txt",
		Success:   true,
	}
	err = manager.Record(op)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	undoable := manager.GetUndoableOperations()
	if len(undoable) != 1 {
		t.Errorf("GetUndoableOperations() returned %d, want 1", len(undoable))
	}

	if undoable[0].Tool != "write_file" {
		t.Errorf("Undoable operation tool = %v, want write_file", undoable[0].Tool)
	}
}

func TestManager_Clear(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record some operations
	for i := 0; i < 3; i++ {
		err = manager.RecordOperation("write_file", nil, "/test.txt", true, "success", "")
		if err != nil {
			t.Fatalf("RecordOperation() error = %v", err)
		}
	}

	// Clear history
	err = manager.Clear()
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	ops := manager.GetAllHistory()
	if len(ops) != 0 {
		t.Errorf("After Clear(), GetAllHistory() returned %d operations, want 0", len(ops))
	}
}

func TestManager_GetStats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record 2 successful write operations
	for i := 0; i < 2; i++ {
		err = manager.RecordOperation("write_file", nil, "/test.txt", true, "success", "/backup/test.txt")
		if err != nil {
			t.Fatalf("RecordOperation() error = %v", err)
		}
	}

	// Record 1 failed operation
	err = manager.RecordOperation("write_file", nil, "/test.txt", false, "error", "")
	if err != nil {
		t.Fatalf("RecordOperation() error = %v", err)
	}

	stats := manager.GetStats()

	if stats["total"] != 3 {
		t.Errorf("stats[total] = %v, want 3", stats["total"])
	}

	if stats["undoable"] != 2 {
		t.Errorf("stats[undoable] = %v, want 2", stats["undoable"])
	}

	if stats["mutations"] != 3 {
		t.Errorf("stats[mutations] = %v, want 3", stats["mutations"])
	}
}

func TestManager_MaxOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Record more than MaxOperations
	for i := 0; i < MaxOperations+10; i++ {
		err = manager.RecordOperation("write_file", nil, "/test.txt", true, "success", "")
		if err != nil {
			t.Fatalf("RecordOperation() error = %v", err)
		}
	}

	ops := manager.GetAllHistory()
	if len(ops) > MaxOperations {
		t.Errorf("History has %d operations, should be capped at %d", len(ops), MaxOperations)
	}
}

func TestManager_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-history-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager and record operation
	manager1, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager1.RecordOperation("write_file", nil, "/test.txt", true, "success", "")
	if err != nil {
		t.Fatalf("RecordOperation() error = %v", err)
	}

	// Create new manager with same path - should load existing history
	manager2, err := NewManager(tmpDir, "test-session")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ops := manager2.GetAllHistory()
	if len(ops) != 1 {
		t.Errorf("After reload, GetAllHistory() returned %d operations, want 1", len(ops))
	}

	if ops[0].Tool != "write_file" {
		t.Errorf("After reload, operation tool = %v, want write_file", ops[0].Tool)
	}
}
