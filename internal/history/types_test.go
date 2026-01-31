package history

import (
	"testing"
	"time"
)

func TestOperationType(t *testing.T) {
	tests := []struct {
		name     string
		opType   OperationType
		expected string
	}{
		{"write", OpTypeWrite, "write"},
		{"edit", OpTypeEdit, "edit"},
		{"delete", OpTypeDelete, "delete"},
		{"move", OpTypeMove, "move"},
		{"copy", OpTypeCopy, "copy"},
		{"read", OpTypeRead, "read"},
		{"create", OpTypeCreate, "create"},
		{"execute", OpTypeExecute, "execute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.opType) != tt.expected {
				t.Errorf("OperationType = %v, want %v", tt.opType, tt.expected)
			}
		})
	}
}

func TestOperation_IsUndoable(t *testing.T) {
	tests := []struct {
		name     string
		opType   OperationType
		expected bool
	}{
		{"write is undoable", OpTypeWrite, true},
		{"edit is undoable", OpTypeEdit, true},
		{"delete is undoable", OpTypeDelete, true},
		{"move is undoable", OpTypeMove, true},
		{"copy is undoable", OpTypeCopy, true},
		{"read is not undoable", OpTypeRead, false},
		{"create is not undoable", OpTypeCreate, false},
		{"execute is not undoable", OpTypeExecute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := &Operation{Type: tt.opType}
			if got := op.IsUndoable(); got != tt.expected {
				t.Errorf("Operation.IsUndoable() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOperation_IsFileMutation(t *testing.T) {
	tests := []struct {
		name     string
		opType   OperationType
		expected bool
	}{
		{"write is mutation", OpTypeWrite, true},
		{"edit is mutation", OpTypeEdit, true},
		{"delete is mutation", OpTypeDelete, true},
		{"move is mutation", OpTypeMove, true},
		{"copy is mutation", OpTypeCopy, true},
		{"create is mutation", OpTypeCreate, true},
		{"read is not mutation", OpTypeRead, false},
		{"execute is not mutation", OpTypeExecute, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := &Operation{Type: tt.opType}
			if got := op.IsFileMutation(); got != tt.expected {
				t.Errorf("Operation.IsFileMutation() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestToolToOperationType(t *testing.T) {
	tests := []struct {
		toolName string
		expected OperationType
	}{
		// Write operations
		{"write_file", OpTypeWrite},
		{"append_file", OpTypeWrite},
		// Edit operations
		{"edit_file", OpTypeEdit},
		{"insert_lines", OpTypeEdit},
		{"replace_lines", OpTypeEdit},
		{"delete_lines", OpTypeEdit},
		// Delete operation
		{"delete_file", OpTypeDelete},
		// Move operation
		{"move_file", OpTypeMove},
		// Copy operation
		{"copy_file", OpTypeCopy},
		// Read operations
		{"read_file", OpTypeRead},
		{"list_files", OpTypeRead},
		{"find_files", OpTypeRead},
		{"search_files", OpTypeRead},
		// Create operation
		{"create_directory", OpTypeCreate},
		// Execute operation
		{"execute_command", OpTypeExecute},
		// Unknown tool defaults to execute
		{"unknown_tool", OpTypeExecute},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			if got := ToolToOperationType(tt.toolName); got != tt.expected {
				t.Errorf("ToolToOperationType(%q) = %v, want %v", tt.toolName, got, tt.expected)
			}
		})
	}
}

func TestOperationHistory(t *testing.T) {
	history := &OperationHistory{
		SessionID:  "test-session-123",
		Operations: []Operation{},
		NextID:     1,
	}

	if history.SessionID != "test-session-123" {
		t.Errorf("SessionID = %v, want %v", history.SessionID, "test-session-123")
	}

	if history.NextID != 1 {
		t.Errorf("NextID = %v, want %v", history.NextID, 1)
	}

	if len(history.Operations) != 0 {
		t.Errorf("Operations length = %v, want %v", len(history.Operations), 0)
	}
}

func TestUndoResult(t *testing.T) {
	result := &UndoResult{
		OperationID:  42,
		Tool:         "write_file",
		Target:       "/path/to/file.txt",
		Success:      true,
		Message:      "Reverted successfully",
		RestoredFrom: "/backup/file.txt.bak",
	}

	if result.OperationID != 42 {
		t.Errorf("OperationID = %v, want %v", result.OperationID, 42)
	}
	if result.Tool != "write_file" {
		t.Errorf("Tool = %v, want %v", result.Tool, "write_file")
	}
	if !result.Success {
		t.Errorf("Success = %v, want %v", result.Success, true)
	}
}

func TestOperationFields(t *testing.T) {
	now := time.Now()
	op := &Operation{
		ID:             1,
		Timestamp:      now,
		Tool:           "write_file",
		Type:           OpTypeWrite,
		Params:         map[string]interface{}{"file_path": "/test.txt"},
		Target:         "/test.txt",
		BackupPath:     "/backup/test.txt",
		Result:         "success",
		Success:        true,
		LinesChanged:   10,
		BytesWritten:   1024,
		Undone:         false,
		UndoneAt:       nil,
		DeletedContent: "",
		OriginalPath:   "",
		CreatedPath:    "",
	}

	if op.ID != 1 {
		t.Errorf("ID = %v, want %v", op.ID, 1)
	}
	if op.Tool != "write_file" {
		t.Errorf("Tool = %v, want %v", op.Tool, "write_file")
	}
	if op.Type != OpTypeWrite {
		t.Errorf("Type = %v, want %v", op.Type, OpTypeWrite)
	}
	if !op.Success {
		t.Errorf("Success = %v, want %v", op.Success, true)
	}
	if op.LinesChanged != 10 {
		t.Errorf("LinesChanged = %v, want %v", op.LinesChanged, 10)
	}
	if op.BytesWritten != 1024 {
		t.Errorf("BytesWritten = %v, want %v", op.BytesWritten, 1024)
	}
}
