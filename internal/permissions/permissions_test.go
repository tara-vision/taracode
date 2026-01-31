package permissions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionCategory(t *testing.T) {
	tests := []struct {
		name     string
		category PermissionCategory
		expected string
	}{
		{"read", CategoryRead, "read"},
		{"write", CategoryWrite, "write"},
		{"execute", CategoryExecute, "execute"},
		{"git", CategoryGit, "git"},
		{"destructive", CategoryDestructive, "destructive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.category) != tt.expected {
				t.Errorf("PermissionCategory = %v, want %v", tt.category, tt.expected)
			}
		})
	}
}

func TestPermission(t *testing.T) {
	tests := []struct {
		name       string
		permission Permission
		expected   string
	}{
		{"ask", PermissionAsk, "ask"},
		{"allow", PermissionAllow, "allow"},
		{"deny", PermissionDeny, "deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.permission) != tt.expected {
				t.Errorf("Permission = %v, want %v", tt.permission, tt.expected)
			}
		})
	}
}

func TestGetToolCategory(t *testing.T) {
	tests := []struct {
		toolName string
		expected PermissionCategory
	}{
		// Read operations
		{"read_file", CategoryRead},
		{"list_files", CategoryRead},
		{"find_files", CategoryRead},
		{"search_files", CategoryRead},
		{"git_status", CategoryRead},
		{"git_diff", CategoryRead},
		{"git_log", CategoryRead},
		{"web_search", CategoryRead},
		{"web_fetch", CategoryRead},
		{"trivy_scan", CategoryRead},
		// Write operations
		{"write_file", CategoryWrite},
		{"edit_file", CategoryWrite},
		{"append_file", CategoryWrite},
		{"copy_file", CategoryWrite},
		{"move_file", CategoryWrite},
		{"create_directory", CategoryWrite},
		// Execute operations
		{"execute_command", CategoryExecute},
		{"docker_exec", CategoryExecute},
		{"kubectl_exec", CategoryExecute},
		// Git operations
		{"git_add", CategoryGit},
		{"git_commit", CategoryGit},
		// Destructive operations
		{"delete_file", CategoryDestructive},
		{"kubectl_delete", CategoryDestructive},
		{"terraform_apply", CategoryDestructive},
		{"terraform_destroy", CategoryDestructive},
		// Unknown tool defaults to destructive
		{"unknown_tool", CategoryDestructive},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := GetToolCategory(tt.toolName)
			if got != tt.expected {
				t.Errorf("GetToolCategory(%q) = %v, want %v", tt.toolName, got, tt.expected)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if manager == nil {
		t.Fatal("NewManager() returned nil")
	}
}

func TestManager_CheckPermission_Defaults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Read operations should be auto-allowed by default
	if perm := manager.CheckPermission("read_file"); perm != PermissionAllow {
		t.Errorf("CheckPermission(read_file) = %v, want allow", perm)
	}

	// Write operations should ask by default
	if perm := manager.CheckPermission("write_file"); perm != PermissionAsk {
		t.Errorf("CheckPermission(write_file) = %v, want ask", perm)
	}

	// Execute operations should ask by default
	if perm := manager.CheckPermission("execute_command"); perm != PermissionAsk {
		t.Errorf("CheckPermission(execute_command) = %v, want ask", perm)
	}

	// Destructive operations should ask by default
	if perm := manager.CheckPermission("delete_file"); perm != PermissionAsk {
		t.Errorf("CheckPermission(delete_file) = %v, want ask", perm)
	}
}

func TestManager_SetToolPermission(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .taracode directory
	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Set tool permission
	err = manager.SetToolPermission("write_file", PermissionAllow)
	if err != nil {
		t.Fatalf("SetToolPermission() error = %v", err)
	}

	// Check it was set
	if perm := manager.CheckPermission("write_file"); perm != PermissionAllow {
		t.Errorf("After SetToolPermission, CheckPermission(write_file) = %v, want allow", perm)
	}
}

func TestManager_SetCategoryPermission(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Set category permission
	err = manager.SetCategoryPermission(CategoryWrite, PermissionAllow)
	if err != nil {
		t.Fatalf("SetCategoryPermission() error = %v", err)
	}

	// Check all write tools now return allow
	writeTool := "write_file"
	if perm := manager.CheckPermission(writeTool); perm != PermissionAllow {
		t.Errorf("After SetCategoryPermission, CheckPermission(%s) = %v, want allow", writeTool, perm)
	}
}

func TestManager_ToolOverridesCategory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Set category to allow
	err = manager.SetCategoryPermission(CategoryWrite, PermissionAllow)
	if err != nil {
		t.Fatalf("SetCategoryPermission() error = %v", err)
	}

	// Set specific tool to deny
	err = manager.SetToolPermission("write_file", PermissionDeny)
	if err != nil {
		t.Fatalf("SetToolPermission() error = %v", err)
	}

	// Tool-specific should override category
	if perm := manager.CheckPermission("write_file"); perm != PermissionDeny {
		t.Errorf("Tool-specific should override category, got %v, want deny", perm)
	}

	// Other write tools should still use category
	if perm := manager.CheckPermission("edit_file"); perm != PermissionAllow {
		t.Errorf("Other write tools should use category, got %v, want allow", perm)
	}
}

func TestManager_Reset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Set some permissions
	manager.SetCategoryPermission(CategoryWrite, PermissionAllow)
	manager.SetToolPermission("edit_file", PermissionDeny)

	// Reset
	err = manager.Reset()
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	// Check defaults are restored
	if perm := manager.CheckPermission("write_file"); perm != PermissionAsk {
		t.Errorf("After Reset, CheckPermission(write_file) = %v, want ask", perm)
	}

	if perm := manager.CheckPermission("read_file"); perm != PermissionAllow {
		t.Errorf("After Reset, CheckPermission(read_file) = %v, want allow", perm)
	}
}

func TestManager_GetConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	manager, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	manager.SetToolPermission("write_file", PermissionAllow)

	config := manager.GetConfig()

	if config.Version != 1 {
		t.Errorf("GetConfig().Version = %v, want 1", config.Version)
	}

	if config.Tools["write_file"] != PermissionAllow {
		t.Errorf("GetConfig().Tools[write_file] = %v, want allow", config.Tools["write_file"])
	}
}

func TestManager_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "taracode-permissions-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("Failed to create .taracode dir: %v", err)
	}

	// Create and configure first manager
	manager1, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager1.SetToolPermission("write_file", PermissionAllow)
	if err != nil {
		t.Fatalf("SetToolPermission() error = %v", err)
	}

	// Create second manager - should load persisted config
	manager2, err := NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if perm := manager2.CheckPermission("write_file"); perm != PermissionAllow {
		t.Errorf("After reload, CheckPermission(write_file) = %v, want allow", perm)
	}
}

func TestGetAllCategories(t *testing.T) {
	categories := GetAllCategories()

	if len(categories) != 6 {
		t.Errorf("GetAllCategories() returned %d categories, want 6", len(categories))
	}

	expected := map[PermissionCategory]bool{
		CategoryRead:        true,
		CategoryWrite:       true,
		CategoryExecute:     true,
		CategoryGit:         true,
		CategoryDestructive: true,
		CategoryMCP:         true,
	}

	for _, cat := range categories {
		if !expected[cat] {
			t.Errorf("Unexpected category: %v", cat)
		}
	}
}

func TestGetCategoryDescription(t *testing.T) {
	tests := []struct {
		category PermissionCategory
		contains string
	}{
		{CategoryRead, "Read"},
		{CategoryWrite, "Write"},
		{CategoryExecute, "Execute"},
		{CategoryGit, "Git"},
		{CategoryDestructive, "Destructive"},
		{CategoryMCP, "MCP"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			desc := GetCategoryDescription(tt.category)
			if desc == "" {
				t.Errorf("GetCategoryDescription(%v) returned empty string", tt.category)
			}
		})
	}
}

func TestGetDefaultPermission(t *testing.T) {
	if perm := GetDefaultPermission(CategoryRead); perm != PermissionAllow {
		t.Errorf("GetDefaultPermission(read) = %v, want allow", perm)
	}

	if perm := GetDefaultPermission(CategoryWrite); perm != PermissionAsk {
		t.Errorf("GetDefaultPermission(write) = %v, want ask", perm)
	}
}

func TestIsValidCategory(t *testing.T) {
	validCategories := []string{"read", "write", "execute", "git", "destructive", "mcp"}
	for _, cat := range validCategories {
		if !IsValidCategory(cat) {
			t.Errorf("IsValidCategory(%q) = false, want true", cat)
		}
	}

	invalidCategories := []string{"invalid", "unknown", ""}
	for _, cat := range invalidCategories {
		if IsValidCategory(cat) {
			t.Errorf("IsValidCategory(%q) = true, want false", cat)
		}
	}
}

func TestIsValidTool(t *testing.T) {
	validTools := []string{"read_file", "write_file", "execute_command", "git_add"}
	for _, tool := range validTools {
		if !IsValidTool(tool) {
			t.Errorf("IsValidTool(%q) = false, want true", tool)
		}
	}

	invalidTools := []string{"invalid_tool", "unknown", ""}
	for _, tool := range invalidTools {
		if IsValidTool(tool) {
			t.Errorf("IsValidTool(%q) = true, want false", tool)
		}
	}
}
