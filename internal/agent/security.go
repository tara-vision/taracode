package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tara-vision/taracode/internal/ui"
)

// handleEditPreview handles the edit preview workflow for edit_file operations
// Returns (proceed, result, error) - if proceed is false, use result as the tool result
func (a *Assistant) handleEditPreview(params map[string]interface{}) (bool, string, error) {
	// Check if preview mode is enabled
	if !a.previewEdits {
		return true, "", nil
	}

	// Extract parameters
	filePath, ok := params["path"].(string)
	if !ok {
		return true, "", nil // Let the tool handle the error
	}

	oldString, ok := params["old"].(string)
	if !ok || oldString == "" {
		return true, "", nil // Let the tool handle the error
	}

	newString, _ := params["new"].(string)

	// Resolve path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(a.workingDir, filePath)
	}

	// Read current file content
	content, err := os.ReadFile(absPath) //nolint:gosec // absPath resolves within the project working directory
	if err != nil {
		return true, "", nil // Let the tool handle the error
	}

	oldContent := string(content)

	// Check that old occurs in the file
	if !strings.Contains(oldContent, oldString) {
		return true, "", nil // Let the tool handle the error
	}

	// Check preview threshold (number of lines changed)
	threshold := a.previewThreshold
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

	// Generate new content for preview (edit_file replaces exactly one occurrence)
	newContent := strings.Replace(oldContent, oldString, newString, 1)

	// Create preview
	preview := &ui.EditPreview{
		FilePath:   filePath,
		OldContent: oldContent,
		NewContent: newContent,
		OldString:  oldString,
		NewString:  newString,
	}

	// Display preview and get user choice
	choice := a.editPreviewChoice(preview)

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
				message := fmt.Sprintf("Failed to create backup: %v", err)
				_, _ = fmt.Fprintln(a.out, a.renderer.WarningMessage(message))
				return false, message, err
			}
			ui.DisplayBackupCreated(backupPath)
		}
		return true, "", nil
	}

	return true, "", nil
}

// editPreviewChoice asks for the preview decision, through confirmEditPreview when something has
// replaced it (tests, or any non-interactive caller) and through the terminal prompt otherwise.
func (a *Assistant) editPreviewChoice(preview *ui.EditPreview) ui.EditPreviewChoice {
	if a.confirmEditPreview != nil {
		return a.confirmEditPreview(preview)
	}
	return ui.DisplayEditPreview(preview)
}
