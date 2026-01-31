package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SandboxedPath validates that a target path stays within the project root.
// It returns the new relative directory (from project root) and absolute path.
// Returns an error if the path would escape the sandbox or is invalid.
func SandboxedPath(target, currentRelDir, projectRoot string) (newRelDir, newAbsDir string, err error) {
	// Reject absolute paths
	if filepath.IsAbs(target) {
		return "", "", fmt.Errorf("absolute paths not allowed, use relative paths within project")
	}

	// Handle empty target (cd with no args) - return to project root
	if target == "" {
		return "", projectRoot, nil
	}

	// Calculate the new relative path
	var newRel string
	if currentRelDir == "" {
		newRel = target
	} else {
		newRel = filepath.Join(currentRelDir, target)
	}

	// Clean the path to resolve . and ..
	newRel = filepath.Clean(newRel)

	// Check if path escapes project root
	// After cleaning, if path starts with ".." it would escape
	if newRel == ".." || strings.HasPrefix(newRel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("cannot navigate above project root")
	}

	// Special case: cleaned path is "." means we're at root
	if newRel == "." {
		newRel = ""
	}

	// Calculate absolute path
	newAbs := projectRoot
	if newRel != "" {
		newAbs = filepath.Join(projectRoot, newRel)
	}

	// Resolve symlinks to prevent escape via symlink
	resolvedAbs, err := filepath.EvalSymlinks(newAbs)
	if err != nil {
		// If path doesn't exist yet, check without symlink resolution
		if os.IsNotExist(err) {
			// Verify the directory exists
			if _, statErr := os.Stat(newAbs); statErr != nil {
				return "", "", fmt.Errorf("directory does not exist: %s", target)
			}
		} else {
			return "", "", fmt.Errorf("cannot access path: %w", err)
		}
		resolvedAbs = newAbs
	}

	// Resolve project root symlinks for comparison
	resolvedRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		resolvedRoot = projectRoot
	}

	// Verify resolved path is within project root
	if !strings.HasPrefix(resolvedAbs, resolvedRoot) {
		return "", "", fmt.Errorf("path escapes project root (symlink detected)")
	}

	// Verify it's a directory
	info, err := os.Stat(newAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("directory does not exist: %s", target)
		}
		return "", "", fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("not a directory: %s", target)
	}

	return newRel, newAbs, nil
}

// FormatPrompt creates the REPL prompt with optional directory indicator.
// When at project root, returns just the prompt symbol.
// When in a subdirectory, includes the relative path.
func FormatPrompt(relDir string) string {
	return FormatPromptWithContext(relDir, 0, 0)
}

// FormatPromptWithContext creates the REPL prompt with directory and context budget.
// Shows token usage as "Xk/Yk" when usedTokens > 0 and maxTokens > 0.
// Colors: gray for path, cyan for context budget, blue for prompt symbol.
func FormatPromptWithContext(relDir string, usedTokens, maxTokens int) string {
	var parts []string

	// Add directory indicator if not at root
	if relDir != "" {
		displayPath := relDir
		if len(displayPath) > 25 {
			pathParts := strings.Split(displayPath, string(filepath.Separator))
			if len(pathParts) > 2 {
				displayPath = "..." + string(filepath.Separator) + filepath.Join(pathParts[len(pathParts)-2:]...)
			}
		}
		parts = append(parts, fmt.Sprintf("\033[90m[%s]\033[0m", displayPath))
	}

	// Add context budget if tracking is enabled
	if usedTokens > 0 && maxTokens > 0 {
		usedK := float64(usedTokens) / 1000
		maxK := float64(maxTokens) / 1000

		// Color based on usage percentage
		usagePercent := float64(usedTokens) / float64(maxTokens) * 100
		var colorCode string
		switch {
		case usagePercent >= 90:
			colorCode = "\033[31m" // Red: critical
		case usagePercent >= 75:
			colorCode = "\033[33m" // Yellow: warning
		default:
			colorCode = "\033[36m" // Cyan: normal
		}

		// Format based on size
		var contextStr string
		if usedK >= 10 {
			contextStr = fmt.Sprintf("%.0fk/%.0fk", usedK, maxK)
		} else {
			contextStr = fmt.Sprintf("%.1fk/%.0fk", usedK, maxK)
		}
		parts = append(parts, fmt.Sprintf("%s[%s]\033[0m", colorCode, contextStr))
	}

	// Build final prompt
	if len(parts) > 0 {
		return strings.Join(parts, " ") + " \033[34m❯\033[0m "
	}
	return "\033[34m❯\033[0m "
}

// FormatPromptWithMode creates the REPL prompt with directory, context budget, and mode indicator.
// When in security mode, adds a shield icon to the prompt.
func FormatPromptWithMode(relDir string, usedTokens, maxTokens int, securityMode bool) string {
	var parts []string

	// Add security mode indicator first
	if securityMode {
		parts = append(parts, "\033[38;5;208m🛡\033[0m") // Orange shield
	}

	// Add directory indicator if not at root
	if relDir != "" {
		displayPath := relDir
		if len(displayPath) > 25 {
			pathParts := strings.Split(displayPath, string(filepath.Separator))
			if len(pathParts) > 2 {
				displayPath = "..." + string(filepath.Separator) + filepath.Join(pathParts[len(pathParts)-2:]...)
			}
		}
		parts = append(parts, fmt.Sprintf("\033[90m[%s]\033[0m", displayPath))
	}

	// Add context budget if tracking is enabled
	if usedTokens > 0 && maxTokens > 0 {
		usedK := float64(usedTokens) / 1000
		maxK := float64(maxTokens) / 1000

		// Color based on usage percentage
		usagePercent := float64(usedTokens) / float64(maxTokens) * 100
		var colorCode string
		switch {
		case usagePercent >= 90:
			colorCode = "\033[31m" // Red: critical
		case usagePercent >= 75:
			colorCode = "\033[33m" // Yellow: warning
		default:
			colorCode = "\033[36m" // Cyan: normal
		}

		// Format based on size
		var contextStr string
		if usedK >= 10 {
			contextStr = fmt.Sprintf("%.0fk/%.0fk", usedK, maxK)
		} else {
			contextStr = fmt.Sprintf("%.1fk/%.0fk", usedK, maxK)
		}
		parts = append(parts, fmt.Sprintf("%s[%s]\033[0m", colorCode, contextStr))
	}

	// Build final prompt
	if len(parts) > 0 {
		return strings.Join(parts, " ") + " \033[34m❯\033[0m "
	}
	return "\033[34m❯\033[0m "
}

// FormatPwd returns a formatted pwd output showing both relative and absolute paths.
func FormatPwd(relDir, projectRoot string) string {
	if relDir == "" {
		return fmt.Sprintf("/ (project root: %s)", projectRoot)
	}
	return fmt.Sprintf("/%s (absolute: %s)", relDir, filepath.Join(projectRoot, relDir))
}
