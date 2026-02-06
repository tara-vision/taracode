package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PermissionCategory represents a category of tools
type PermissionCategory string

const (
	CategoryRead        PermissionCategory = "read"        // read_file, list_files, git_status, etc.
	CategoryWrite       PermissionCategory = "write"       // write_file, edit_file, append_file, etc.
	CategoryExecute     PermissionCategory = "execute"     // execute_command, docker_exec, kubectl_exec
	CategoryGit         PermissionCategory = "git"         // git_add, git_commit, git_branch
	CategoryDestructive PermissionCategory = "destructive" // delete_file, terraform_destroy, kubectl_delete
	CategoryMCP         PermissionCategory = "mcp"         // MCP server tools (e.g., github.list_repos)
)

// Permission represents the permission level for a tool or category
type Permission string

const (
	PermissionAsk   Permission = "ask"   // Always ask (default)
	PermissionAllow Permission = "allow" // Always allow
	PermissionDeny  Permission = "deny"  // Always deny
)

// PermissionConfig represents the structure stored in .taracode/permissions.json
type PermissionConfig struct {
	Version    int                               `json:"version"`
	Categories map[PermissionCategory]Permission `json:"categories"`
	Tools      map[string]Permission             `json:"tools"`
}

// toolCategoryMap maps tool names to their categories
var toolCategoryMap = map[string]PermissionCategory{
	// Read operations (safe, auto-allowed by default)
	"read_file":        CategoryRead,
	"list_files":       CategoryRead,
	"find_files":       CategoryRead,
	"search_files":     CategoryRead,
	"git_status":       CategoryRead,
	"git_diff":         CategoryRead,
	"git_log":          CategoryRead,
	"git_branch":       CategoryRead, // listing branches is read
	"kubectl_get":      CategoryRead,
	"kubectl_describe": CategoryRead,
	"kubectl_logs":     CategoryRead,
	"terraform_output": CategoryRead,
	"terraform_state":  CategoryRead,
	"docker_ps":        CategoryRead,
	"docker_logs":      CategoryRead,
	"helm_list":        CategoryRead,
	"web_search":       CategoryRead,
	"web_fetch":        CategoryRead,
	"get_datetime":     CategoryRead,
	"trivy_scan":       CategoryRead,
	"gitleaks_scan":    CategoryRead,
	"secrets_scan":     CategoryRead,
	"dependency_audit": CategoryRead,
	"sast_scan":        CategoryRead,
	"tfsec_scan":       CategoryRead,
	"kubesec_scan":     CategoryRead,

	// Write operations
	"write_file":       CategoryWrite,
	"edit_file":        CategoryWrite,
	"append_file":      CategoryWrite,
	"insert_lines":     CategoryWrite,
	"replace_lines":    CategoryWrite,
	"delete_lines":     CategoryWrite,
	"copy_file":        CategoryWrite,
	"move_file":        CategoryWrite,
	"create_directory": CategoryWrite,

	// Execute operations
	"execute_command": CategoryExecute,
	"docker_exec":     CategoryExecute,
	"kubectl_exec":    CategoryExecute,

	// Git mutation operations
	"git_add":    CategoryGit,
	"git_commit": CategoryGit,
	"git_stash":  CategoryGit,

	// Destructive operations
	"delete_file":       CategoryDestructive,
	"kubectl_delete":    CategoryDestructive,
	"kubectl_apply":     CategoryDestructive, // Can create/modify resources
	"helm_install":      CategoryDestructive, // Modifies cluster
	"terraform_init":    CategoryDestructive, // Downloads providers, modifies state
	"terraform_plan":    CategoryDestructive, // Can have side effects with providers
	"terraform_apply":   CategoryDestructive,
	"terraform_destroy": CategoryDestructive,
	"docker_build":      CategoryDestructive, // Creates images
	"docker_compose":    CategoryDestructive, // Can start/stop services
	"aws_cli":           CategoryDestructive, // Can modify cloud resources
	"aws_ecs":           CategoryDestructive,
	"aws_eks":           CategoryDestructive,
	"az_cli":            CategoryDestructive,
	"az_aks":            CategoryDestructive,
	"gcloud":            CategoryDestructive,
	"gke":               CategoryDestructive,
}

// defaultCategoryPermissions defines default behavior per category
var defaultCategoryPermissions = map[PermissionCategory]Permission{
	CategoryRead:        PermissionAllow, // Safe operations auto-allowed
	CategoryWrite:       PermissionAsk,
	CategoryExecute:     PermissionAsk,
	CategoryGit:         PermissionAsk,
	CategoryDestructive: PermissionAsk,
	CategoryMCP:         PermissionAsk, // MCP tools always ask by default
}

// Manager handles permission checking and persistence
type Manager struct {
	projectRoot string
	config      *PermissionConfig
	mu          sync.RWMutex
	configPath  string
}

// NewManager creates a new permission manager for the given project root
func NewManager(projectRoot string) (*Manager, error) {
	m := &Manager{
		projectRoot: projectRoot,
		configPath:  filepath.Join(projectRoot, ".taracode", "permissions.json"),
		config: &PermissionConfig{
			Version:    1,
			Categories: make(map[PermissionCategory]Permission),
			Tools:      make(map[string]Permission),
		},
	}

	// Load existing config if it exists
	if err := m.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load permissions: %w", err)
	}

	return m, nil
}

// load reads the permission config from disk
func (m *Manager) load() error {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	var config PermissionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse permissions.json: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = &config

	// Ensure maps are initialized
	if m.config.Categories == nil {
		m.config.Categories = make(map[PermissionCategory]Permission)
	}
	if m.config.Tools == nil {
		m.config.Tools = make(map[string]Permission)
	}

	return nil
}

// Save writes the permission config to disk
func (m *Manager) Save() error {
	m.mu.RLock()
	data, err := json.MarshalIndent(m.config, "", "  ")
	m.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to serialize permissions: %w", err)
	}

	// Ensure .taracode directory exists
	dir := filepath.Dir(m.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create .taracode directory: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write permissions.json: %w", err)
	}

	return nil
}

// GetToolCategory returns the category for a tool
func GetToolCategory(toolName string) PermissionCategory {
	if cat, ok := toolCategoryMap[toolName]; ok {
		return cat
	}
	// MCP tools have "server.tool" format with a dot
	if strings.Contains(toolName, ".") {
		return CategoryMCP
	}
	// Default to destructive for unknown tools (safer)
	return CategoryDestructive
}

// CheckPermission returns the permission for a tool
func (m *Manager) CheckPermission(toolName string) Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check tool-specific permission first
	if perm, ok := m.config.Tools[toolName]; ok {
		return perm
	}

	// Fall back to category permission
	category := GetToolCategory(toolName)
	if perm, ok := m.config.Categories[category]; ok {
		return perm
	}

	// Fall back to default for category
	if perm, ok := defaultCategoryPermissions[category]; ok {
		return perm
	}

	// Ultimate fallback: ask
	return PermissionAsk
}

// SetToolPermission sets permission for a specific tool
func (m *Manager) SetToolPermission(toolName string, perm Permission) error {
	m.mu.Lock()
	m.config.Tools[toolName] = perm
	m.mu.Unlock()
	return m.Save()
}

// SetCategoryPermission sets permission for a category
func (m *Manager) SetCategoryPermission(category PermissionCategory, perm Permission) error {
	m.mu.Lock()
	m.config.Categories[category] = perm
	m.mu.Unlock()
	return m.Save()
}

// Reset clears all custom permissions, returning to defaults
func (m *Manager) Reset() error {
	m.mu.Lock()
	m.config = &PermissionConfig{
		Version:    1,
		Categories: make(map[PermissionCategory]Permission),
		Tools:      make(map[string]Permission),
	}
	m.mu.Unlock()
	return m.Save()
}

// GetConfig returns a copy of the current config for display
func (m *Manager) GetConfig() PermissionConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	config := PermissionConfig{
		Version:    m.config.Version,
		Categories: make(map[PermissionCategory]Permission),
		Tools:      make(map[string]Permission),
	}
	for k, v := range m.config.Categories {
		config.Categories[k] = v
	}
	for k, v := range m.config.Tools {
		config.Tools[k] = v
	}
	return config
}

// GetAllCategories returns all available permission categories
func GetAllCategories() []PermissionCategory {
	return []PermissionCategory{
		CategoryRead,
		CategoryWrite,
		CategoryExecute,
		CategoryGit,
		CategoryDestructive,
		CategoryMCP,
	}
}

// GetCategoryDescription returns a human-readable description for a category
func GetCategoryDescription(cat PermissionCategory) string {
	switch cat {
	case CategoryRead:
		return "Read operations (read_file, list_files, git_status, etc.)"
	case CategoryWrite:
		return "Write operations (write_file, edit_file, append_file, etc.)"
	case CategoryExecute:
		return "Execute operations (execute_command, docker_exec, kubectl_exec)"
	case CategoryGit:
		return "Git mutations (git_add, git_commit)"
	case CategoryDestructive:
		return "Destructive operations (delete_file, terraform_destroy, kubectl_delete, etc.)"
	case CategoryMCP:
		return "MCP server tools (external integrations)"
	default:
		return string(cat)
	}
}

// GetDefaultPermission returns the default permission for a category
func GetDefaultPermission(cat PermissionCategory) Permission {
	if perm, ok := defaultCategoryPermissions[cat]; ok {
		return perm
	}
	return PermissionAsk
}

// IsValidCategory checks if a string is a valid category
func IsValidCategory(s string) bool {
	switch PermissionCategory(s) {
	case CategoryRead, CategoryWrite, CategoryExecute, CategoryGit, CategoryDestructive, CategoryMCP:
		return true
	}
	return false
}

// IsValidTool checks if a string is a known tool
func IsValidTool(s string) bool {
	_, ok := toolCategoryMap[s]
	return ok
}
