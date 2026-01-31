package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLogManagement(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "taracode-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create manager
	mgr, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create a session
	session, err := mgr.CreateSession("Test Session")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	t.Run("InitAuditLog", func(t *testing.T) {
		err := mgr.InitAuditLog(session.ID, "security")
		if err != nil {
			t.Errorf("InitAuditLog failed: %v", err)
		}

		// Verify audit log was created
		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if log == nil {
			t.Error("Expected audit log to be created")
		}
		if log.SessionMode != "security" {
			t.Errorf("Expected session mode 'security', got '%s'", log.SessionMode)
		}
		if len(log.Entries) != 0 {
			t.Errorf("Expected 0 entries, got %d", len(log.Entries))
		}
	})

	t.Run("AddAuditEntry_Allow", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp:   time.Now(),
			ToolName:    "write_file",
			Category:    "write",
			Action:      AuditActionAllow,
			Target:      "/tmp/test.txt",
			Implication: "Modifies file on disk",
			Params: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"content":   "test content",
			},
		}

		err := mgr.AddAuditEntry(session.ID, entry)
		if err != nil {
			t.Errorf("AddAuditEntry failed: %v", err)
		}

		// Verify entry was added
		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if len(log.Entries) != 1 {
			t.Errorf("Expected 1 entry, got %d", len(log.Entries))
		}
		if log.TotalAllow != 1 {
			t.Errorf("Expected TotalAllow=1, got %d", log.TotalAllow)
		}
		if log.TotalDeny != 0 {
			t.Errorf("Expected TotalDeny=0, got %d", log.TotalDeny)
		}
		if log.Entries[0].ToolName != "write_file" {
			t.Errorf("Expected tool name 'write_file', got '%s'", log.Entries[0].ToolName)
		}
	})

	t.Run("AddAuditEntry_Deny", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp:   time.Now(),
			ToolName:    "execute_command",
			Category:    "execute",
			Action:      AuditActionDeny,
			Target:      "rm -rf /",
			Implication: "Executes arbitrary command",
		}

		err := mgr.AddAuditEntry(session.ID, entry)
		if err != nil {
			t.Errorf("AddAuditEntry failed: %v", err)
		}

		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if len(log.Entries) != 2 {
			t.Errorf("Expected 2 entries, got %d", len(log.Entries))
		}
		if log.TotalAllow != 1 {
			t.Errorf("Expected TotalAllow=1, got %d", log.TotalAllow)
		}
		if log.TotalDeny != 1 {
			t.Errorf("Expected TotalDeny=1, got %d", log.TotalDeny)
		}
	})

	t.Run("AddAuditEntry_AllowAll", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp:  time.Now(),
			ToolName:   "git_add",
			Category:   "git",
			Action:     AuditActionAllowAll,
			Target:     "*.go",
			BatchIndex: 2,
			BatchTotal: 5,
		}

		err := mgr.AddAuditEntry(session.ID, entry)
		if err != nil {
			t.Errorf("AddAuditEntry failed: %v", err)
		}

		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if log.TotalAllow != 2 { // allow + allow_all
			t.Errorf("Expected TotalAllow=2, got %d", log.TotalAllow)
		}
	})

	t.Run("AddAuditEntry_DenyAll", func(t *testing.T) {
		entry := AuditEntry{
			Timestamp:  time.Now(),
			ToolName:   "terraform_destroy",
			Category:   "destructive",
			Action:     AuditActionDenyAll,
			Target:     "aws_instance.main",
			BatchIndex: 1,
			BatchTotal: 3,
		}

		err := mgr.AddAuditEntry(session.ID, entry)
		if err != nil {
			t.Errorf("AddAuditEntry failed: %v", err)
		}

		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if log.TotalDeny != 2 { // deny + deny_all
			t.Errorf("Expected TotalDeny=2, got %d", log.TotalDeny)
		}
	})

	t.Run("ClearAuditLog", func(t *testing.T) {
		err := mgr.ClearAuditLog(session.ID)
		if err != nil {
			t.Errorf("ClearAuditLog failed: %v", err)
		}

		log, err := mgr.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed: %v", err)
		}
		if len(log.Entries) != 0 {
			t.Errorf("Expected 0 entries after clear, got %d", len(log.Entries))
		}
	})

	t.Run("GetAuditLog_NoSession", func(t *testing.T) {
		_, err := mgr.GetAuditLog("nonexistent-id")
		if err == nil {
			t.Error("Expected error for nonexistent session")
		}
	})

	t.Run("AuditLogPersistence", func(t *testing.T) {
		// Add an entry
		entry := AuditEntry{
			Timestamp: time.Now(),
			ToolName:  "delete_file",
			Category:  "destructive",
			Action:    AuditActionAllow,
			Target:    "/tmp/to-delete.txt",
		}

		_ = mgr.InitAuditLog(session.ID, "security")
		err := mgr.AddAuditEntry(session.ID, entry)
		if err != nil {
			t.Errorf("AddAuditEntry failed: %v", err)
		}

		// Create new manager to test persistence
		mgr2, err := NewManager(tmpDir)
		if err != nil {
			t.Fatalf("Failed to create second manager: %v", err)
		}

		log, err := mgr2.GetAuditLog(session.ID)
		if err != nil {
			t.Errorf("GetAuditLog failed after reload: %v", err)
		}
		if len(log.Entries) != 1 {
			t.Errorf("Expected 1 entry after reload, got %d", len(log.Entries))
		}
		if log.Entries[0].ToolName != "delete_file" {
			t.Errorf("Expected tool name 'delete_file', got '%s'", log.Entries[0].ToolName)
		}
	})
}

func TestExtractAuditTargetHelper(t *testing.T) {
	// Test the target extraction by creating session files and verifying structure
	tmpDir, err := os.MkdirTemp("", "taracode-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Check session file structure
	mgr, _ := NewManager(tmpDir)
	session, _ := mgr.CreateSession("Test")
	_ = mgr.InitAuditLog(session.ID, "security")

	entry := AuditEntry{
		Timestamp: time.Now(),
		ToolName:  "execute_command",
		Category:  "execute",
		Action:    AuditActionAllow,
		Target:    "ls -la /tmp",
		Params: map[string]interface{}{
			"command": "ls -la /tmp",
		},
	}
	_ = mgr.AddAuditEntry(session.ID, entry)

	// Verify session file exists with audit log
	sessionPath := filepath.Join(tmpDir, ".taracode", "history", "session_"+session.ID+".json")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("Failed to read session file: %v", err)
	}

	// Check that audit_log is in the JSON
	if len(data) == 0 {
		t.Error("Session file is empty")
	}
	// Simple check that audit_log key exists
	if !contains(string(data), "audit_log") {
		t.Error("Session file does not contain audit_log")
	}
	if !contains(string(data), "execute_command") {
		t.Error("Session file does not contain tool name")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
