package assistant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// ClearAuditLog clears the audit log for the current session
func (a *Assistant) ClearAuditLog() error {
	if a.storage == nil || a.session == nil {
		return fmt.Errorf("no active session")
	}
	return a.storage.ClearAuditLog(a.session.ID)
}

// checkToolPermission checks if a tool is allowed to execute and handles user prompts
// Returns (allowed, resultMessage) - if not allowed, resultMessage contains the denial message
func (a *Assistant) checkToolPermission(toolName string, params map[string]interface{}) (bool, string) {
	// If no permission manager, allow all (graceful degradation)
	if a.permMgr == nil {
		return true, ""
	}

	perm := a.permMgr.CheckPermission(toolName)

	switch perm {
	case permissions.PermissionAllow:
		return true, ""
	case permissions.PermissionDeny:
		ui.DisplayPermissionDenied(toolName)
		return false, fmt.Sprintf("Tool '%s' blocked by permission settings", toolName)
	case permissions.PermissionAsk:
		// Prompt user for permission
		choice := ui.PromptToolPermission(toolName, params)

		// Save permission if requested
		if choice.SavePerm != "" {
			var err error
			if choice.SaveScope == "tool" {
				err = a.permMgr.SetToolPermission(toolName, choice.SavePerm)
			} else if choice.SaveScope == "category" {
				err = a.permMgr.SetCategoryPermission(choice.Category, choice.SavePerm)
			}
			if err == nil {
				ui.DisplayPermissionSaved(choice.SaveScope, string(choice.Category), choice.SavePerm)
			}
		}

		if !choice.Allowed {
			return false, fmt.Sprintf("Tool '%s' denied by user", toolName)
		}
		return true, ""
	}

	return true, ""
}

// GetPermissionManager returns the permission manager for external access (e.g., REPL commands)
func (a *Assistant) GetPermissionManager() *permissions.Manager {
	return a.permMgr
}

// checkSecurityAudit enforces audit-first behavior in security mode
// This is called AFTER permission check passes, providing an additional layer of protection
// Returns (allowed, resultMessage) - if not allowed, resultMessage contains the denial message
func (a *Assistant) checkSecurityAudit(toolName string, params map[string]interface{}, batch *ui.BatchAuditContext) (bool, string) {
	// Only enforce in security mode
	if a.mode != storage.ModeSecurity {
		return true, ""
	}

	// Get the tool's category
	category := permissions.GetToolCategory(toolName)

	// Only audit non-read operations
	// Read operations are safe and don't require audit confirmation
	if category == permissions.CategoryRead {
		return true, ""
	}

	// Helper to record audit entry
	recordAudit := func(action storage.AuditAction) {
		if a.storage == nil || a.session == nil {
			return
		}
		entry := storage.AuditEntry{
			Timestamp:   time.Now(),
			ToolName:    toolName,
			Category:    string(category),
			Action:      action,
			Params:      params,
			Target:      extractAuditTarget(toolName, params),
			Implication: ui.GetSecurityImplication(toolName),
		}
		if batch != nil {
			entry.BatchIndex = batch.CurrentIndex
			entry.BatchTotal = batch.TotalTools
		}
		// Record asynchronously to avoid blocking
		go func() {
			_ = a.storage.AddAuditEntry(a.session.ID, entry)
		}()
	}

	// Check if batch decision already made
	if batch != nil {
		if batch.AllowRemaining {
			recordAudit(storage.AuditActionAllowAll)
			return true, ""
		}
		if batch.DenyRemaining {
			recordAudit(storage.AuditActionDenyAll)
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit (batch deny)", toolName)
		}
	}

	// Build security audit info
	auditInfo := &ui.SecurityAuditInfo{
		ToolName:    toolName,
		Category:    category,
		Params:      params,
		Implication: ui.GetSecurityImplication(toolName),
	}

	// Loop to handle "details" option
	for {
		choice := ui.PromptSecurityAuditBatch(auditInfo, batch)

		switch choice {
		case ui.SecurityAuditAllow:
			recordAudit(storage.AuditActionAllow)
			return true, ""
		case ui.SecurityAuditDeny:
			recordAudit(storage.AuditActionDeny)
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit", toolName)
		case ui.SecurityAuditAllowAll:
			recordAudit(storage.AuditActionAllowAll)
			if batch != nil {
				batch.AllowRemaining = true
				remaining := batch.TotalTools - batch.CurrentIndex
				if remaining > 0 {
					ui.DisplaySecurityAuditBatchAllowed(remaining)
				}
			}
			return true, ""
		case ui.SecurityAuditDenyAll:
			recordAudit(storage.AuditActionDenyAll)
			if batch != nil {
				batch.DenyRemaining = true
				remaining := batch.TotalTools - batch.CurrentIndex
				if remaining > 0 {
					ui.DisplaySecurityAuditBatchDenied(remaining)
				}
			}
			ui.DisplaySecurityAuditDenied(toolName)
			return false, fmt.Sprintf("Operation '%s' blocked by security audit (batch deny)", toolName)
		case ui.SecurityAuditDetails:
			ui.DisplaySecurityAuditDetails(auditInfo)
			// Loop continues to show prompt again
		}
	}
}

// extractAuditTarget extracts the primary target from tool parameters for audit logging
func extractAuditTarget(toolName string, params map[string]interface{}) string {
	// Map of tools to their primary target parameter
	targetParams := map[string][]string{
		// File operations
		"write_file":       {"file_path"},
		"append_file":      {"file_path"},
		"edit_file":        {"file_path"},
		"insert_lines":     {"file_path"},
		"replace_lines":    {"file_path"},
		"delete_lines":     {"file_path"},
		"copy_file":        {"source", "destination"},
		"move_file":        {"source", "destination"},
		"delete_file":      {"file_path"},
		"create_directory": {"path"},
		// Execution
		"execute_command": {"command"},
		// Git
		"git_add":    {"files"},
		"git_commit": {"message"},
		"git_branch": {"name", "action"},
		// Kubernetes
		"kubectl_apply":  {"resource", "file"},
		"kubectl_delete": {"resource", "name"},
		"kubectl_exec":   {"pod", "command"},
		// Docker
		"docker_build":   {"dockerfile", "tag"},
		"docker_compose": {"action"},
		"docker_exec":    {"container", "command"},
		// Terraform
		"terraform_apply":   {"target"},
		"terraform_destroy": {"target"},
		// Helm
		"helm_install": {"release", "chart"},
	}

	if paramNames, ok := targetParams[toolName]; ok {
		var targets []string
		for _, name := range paramNames {
			if val, ok := params[name]; ok {
				switch v := val.(type) {
				case string:
					if v != "" {
						targets = append(targets, v)
					}
				case []interface{}:
					for _, item := range v {
						if s, ok := item.(string); ok && s != "" {
							targets = append(targets, s)
						}
					}
				}
			}
		}
		if len(targets) > 0 {
			if len(targets) == 1 {
				return targets[0]
			}
			return fmt.Sprintf("%v", targets)
		}
	}

	return ""
}

// handleEditPreview handles the edit preview workflow for edit_file operations
// Returns (proceed, result, error) - if proceed is false, use result as the tool result
func (a *Assistant) handleEditPreview(params map[string]interface{}) (bool, string, error) {
	// Check if preview mode is enabled
	if !viper.GetBool("preview_edits") {
		return true, "", nil
	}

	// Extract parameters
	filePath, ok := params["file_path"].(string)
	if !ok {
		return true, "", nil // Let the tool handle the error
	}

	oldString, ok := params["old_string"].(string)
	if !ok || oldString == "" {
		return true, "", nil // Let the tool handle the error
	}

	newString, _ := params["new_string"].(string)

	// Resolve path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(a.workingDir, filePath)
	}

	// Read current file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return true, "", nil // Let the tool handle the error
	}

	oldContent := string(content)

	// Check if old_string exists
	if !strings.Contains(oldContent, oldString) {
		return true, "", nil // Let the tool handle the error
	}

	// Check preview threshold (number of lines changed)
	threshold := viper.GetInt("preview_threshold")
	oldLines := strings.Count(oldString, "\n") + 1
	newLines := strings.Count(newString, "\n") + 1
	linesChanged := oldLines
	if newLines > oldLines {
		linesChanged = newLines
	}

	// Skip preview if below threshold (unless threshold is 0 which means always preview)
	if threshold > 0 && linesChanged < threshold {
		return true, "", nil
	}

	// Generate new content for preview
	replaceAll := false
	if ra, ok := params["replace_all"].(bool); ok {
		replaceAll = ra
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, newString)
	} else {
		newContent = strings.Replace(oldContent, oldString, newString, 1)
	}

	// Create preview
	preview := &ui.EditPreview{
		FilePath:   filePath,
		OldContent: oldContent,
		NewContent: newContent,
		OldString:  oldString,
		NewString:  newString,
	}

	// Display preview and get user choice
	choice := ui.DisplayEditPreview(preview)

	switch choice {
	case ui.EditPreviewApply:
		// User approved - proceed with edit
		return true, "", nil

	case ui.EditPreviewCancel:
		// User cancelled
		ui.DisplayEditCancelled(filePath)
		return false, fmt.Sprintf("Edit cancelled by user: %s", filePath), nil

	case ui.EditPreviewBackupThenApply:
		// Create backup first, then proceed
		if a.storage != nil {
			backupPath, err := a.storage.CreateBackup(absPath)
			if err != nil {
				return false, fmt.Sprintf("Failed to create backup: %v", err), err
			}
			ui.DisplayBackupCreated(backupPath)
		}
		return true, "", nil
	}

	return true, "", nil
}
