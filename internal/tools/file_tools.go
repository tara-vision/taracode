package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadFile(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Check for line range parameters
	startLine, hasStart := params["start_line"]
	endLine, hasEnd := params["end_line"]

	// If no range specified, return full content
	if !hasStart && !hasEnd {
		return string(content), nil
	}

	// Parse line ranges
	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	var start, end int
	if hasStart {
		switch v := startLine.(type) {
		case int:
			start = v
		case float64:
			start = int(v)
		default:
			return "", fmt.Errorf("start_line must be a number")
		}
		if start < 1 || start > totalLines {
			return "", fmt.Errorf("start_line %d is out of range (file has %d lines)", start, totalLines)
		}
	} else {
		start = 1
	}

	if hasEnd {
		switch v := endLine.(type) {
		case int:
			end = v
		case float64:
			end = int(v)
		default:
			return "", fmt.Errorf("end_line must be a number")
		}
		if end < start || end > totalLines {
			return "", fmt.Errorf("end_line %d is invalid (must be between %d and %d)", end, start, totalLines)
		}
	} else {
		end = totalLines
	}

	// Extract range (convert to 0-indexed)
	selectedLines := lines[start-1 : end]

	// Add line numbers for context
	var result strings.Builder
	result.WriteString(fmt.Sprintf("=== %s (lines %d-%d of %d) ===\n", filepath.Base(filePath), start, end, totalLines))
	for i, line := range selectedLines {
		lineNum := start + i
		result.WriteString(fmt.Sprintf("%4d: %s\n", lineNum, line))
	}

	return result.String(), nil
}

func WriteFile(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Create parent directories if they don't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directories: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully wrote to %s", filePath), nil
}

func AppendFile(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Read existing content
	existingContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Append new content
	newContent := string(existingContent) + content

	// Write back
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully appended to %s", filePath), nil
}

func InsertLines(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter is required")
	}

	// line_number can be int or float64 (from JSON unmarshaling)
	var lineNum int
	switch v := params["line_number"].(type) {
	case int:
		lineNum = v
	case float64:
		lineNum = int(v)
	default:
		return "", fmt.Errorf("line_number parameter is required and must be a number")
	}

	// Resolve path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Read current content
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(fileContent), "\n")

	// Validate line number (1-indexed)
	if lineNum < 1 || lineNum > len(lines)+1 {
		return "", fmt.Errorf("line_number %d is out of range (file has %d lines)", lineNum, len(lines))
	}

	// Insert content at specified line (convert to 0-indexed)
	insertIdx := lineNum - 1
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertIdx]...)
	newLines = append(newLines, content)
	newLines = append(newLines, lines[insertIdx:]...)

	// Write back
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully inserted at line %d in %s", lineNum, filePath), nil
}

func ReplaceLines(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	content, ok := params["content"].(string)
	if !ok {
		return "", fmt.Errorf("content parameter is required")
	}

	var startLine, endLine int
	switch v := params["start_line"].(type) {
	case int:
		startLine = v
	case float64:
		startLine = int(v)
	default:
		return "", fmt.Errorf("start_line parameter is required and must be a number")
	}

	switch v := params["end_line"].(type) {
	case int:
		endLine = v
	case float64:
		endLine = int(v)
	default:
		return "", fmt.Errorf("end_line parameter is required and must be a number")
	}

	// Resolve path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Read current content
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(fileContent), "\n")

	// Validate range (1-indexed)
	if startLine < 1 || startLine > len(lines) {
		return "", fmt.Errorf("start_line %d is out of range (file has %d lines)", startLine, len(lines))
	}
	if endLine < startLine || endLine > len(lines) {
		return "", fmt.Errorf("end_line %d is invalid (must be between %d and %d)", endLine, startLine, len(lines))
	}

	// Replace lines (convert to 0-indexed)
	startIdx := startLine - 1
	endIdx := endLine

	newLines := make([]string, 0)
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, content)
	newLines = append(newLines, lines[endIdx:]...)

	// Write back
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	linesReplaced := endLine - startLine + 1
	return fmt.Sprintf("Successfully replaced lines %d-%d (%d lines) in %s", startLine, endLine, linesReplaced, filePath), nil
}

func DeleteLines(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	var startLine, endLine int
	switch v := params["start_line"].(type) {
	case int:
		startLine = v
	case float64:
		startLine = int(v)
	default:
		return "", fmt.Errorf("start_line parameter is required and must be a number")
	}

	switch v := params["end_line"].(type) {
	case int:
		endLine = v
	case float64:
		endLine = int(v)
	default:
		return "", fmt.Errorf("end_line parameter is required and must be a number")
	}

	// Resolve path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Read current content
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(fileContent), "\n")

	// Validate range (1-indexed)
	if startLine < 1 || startLine > len(lines) {
		return "", fmt.Errorf("start_line %d is out of range (file has %d lines)", startLine, len(lines))
	}
	if endLine < startLine || endLine > len(lines) {
		return "", fmt.Errorf("end_line %d is invalid (must be between %d and %d)", endLine, startLine, len(lines))
	}

	// Delete lines (convert to 0-indexed)
	startIdx := startLine - 1
	endIdx := endLine

	newLines := make([]string, 0)
	newLines = append(newLines, lines[:startIdx]...)
	newLines = append(newLines, lines[endIdx:]...)

	// Write back
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	linesDeleted := endLine - startLine + 1
	return fmt.Sprintf("Successfully deleted lines %d-%d (%d lines) from %s", startLine, endLine, linesDeleted, filePath), nil
}

func EditFile(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	oldString, ok := params["old_string"].(string)
	if !ok {
		return "", fmt.Errorf("old_string parameter is required")
	}

	// Prevent empty old_string which would replace every character
	if oldString == "" {
		return "", fmt.Errorf("old_string cannot be empty. To completely rewrite a file, use write_file tool. To add content to the end, use append_file. To modify specific text, read the file first to find exact text to replace")
	}

	newString, ok := params["new_string"].(string)
	if !ok {
		return "", fmt.Errorf("new_string parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Read current content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	fileContent := string(content)

	// Check if old_string exists in the file
	if !strings.Contains(fileContent, oldString) {
		return "", fmt.Errorf("old_string not found in file. Make sure to match the exact text including whitespace and indentation")
	}

	// Count occurrences
	count := strings.Count(fileContent, oldString)

	// Check for replace_all option
	replaceAll := false
	if ra, ok := params["replace_all"].(bool); ok {
		replaceAll = ra
	}

	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(fileContent, oldString, newString)
	} else {
		if count > 1 {
			return "", fmt.Errorf("old_string appears %d times in file. Use replace_all=true to replace all occurrences, or provide more context to make it unique", count)
		}
		newContent = strings.Replace(fileContent, oldString, newString, 1)
	}

	// Write the modified content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if replaceAll && count > 1 {
		return fmt.Sprintf("Successfully replaced %d occurrences in %s", count, filePath), nil
	}
	return fmt.Sprintf("Successfully edited %s", filePath), nil
}

func CopyFile(params map[string]interface{}, workingDir string) (string, error) {
	sourcePath, ok := params["source_path"].(string)
	if !ok {
		return "", fmt.Errorf("source_path parameter is required")
	}

	destPath, ok := params["dest_path"].(string)
	if !ok {
		return "", fmt.Errorf("dest_path parameter is required")
	}

	// Resolve paths relative to working directory
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(workingDir, sourcePath)
	}

	if !filepath.IsAbs(destPath) {
		destPath = filepath.Join(workingDir, destPath)
	}

	// Read source file
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", fmt.Errorf("failed to read source file: %w", err)
	}

	// Create parent directories for destination
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directories: %w", err)
	}

	// Write to destination
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write destination file: %w", err)
	}

	return fmt.Sprintf("Successfully copied %s to %s", sourcePath, destPath), nil
}

func MoveFile(params map[string]interface{}, workingDir string) (string, error) {
	sourcePath, ok := params["source_path"].(string)
	if !ok {
		return "", fmt.Errorf("source_path parameter is required")
	}

	destPath, ok := params["dest_path"].(string)
	if !ok {
		return "", fmt.Errorf("dest_path parameter is required")
	}

	// Resolve paths relative to working directory
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(workingDir, sourcePath)
	}

	if !filepath.IsAbs(destPath) {
		destPath = filepath.Join(workingDir, destPath)
	}

	// Check if source exists
	if _, err := os.Stat(sourcePath); err != nil {
		return "", fmt.Errorf("source file does not exist: %w", err)
	}

	// Create parent directories for destination
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directories: %w", err)
	}

	// Try to rename (works for same filesystem)
	if err := os.Rename(sourcePath, destPath); err != nil {
		// Fall back to copy+delete for cross-device moves
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return "", fmt.Errorf("failed to read source file: %w", err)
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			return "", fmt.Errorf("failed to write destination file: %w", err)
		}

		if err := os.Remove(sourcePath); err != nil {
			return "", fmt.Errorf("failed to remove source file after copy: %w", err)
		}
	}

	return fmt.Sprintf("Successfully moved %s to %s", sourcePath, destPath), nil
}

func DeleteFile(params map[string]interface{}, workingDir string) (string, error) {
	filePath, ok := params["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	// Check if path exists
	info, err := os.Stat(filePath)
	if err != nil {
		// Succeed silently if file doesn't exist (idempotent behavior)
		return fmt.Sprintf("Successfully deleted %s", filePath), nil
	}

	// Extract recursive option
	recursive := false
	if rec, ok := params["recursive"].(bool); ok {
		recursive = rec
	}

	// If it's a directory, check recursive flag
	if info.IsDir() {
		if !recursive {
			return "", fmt.Errorf("cannot delete directory without recursive=true. Use recursive=true to delete the directory and its contents")
		}
		// Delete directory and contents
		if err := os.RemoveAll(filePath); err != nil {
			return "", fmt.Errorf("failed to delete directory: %w", err)
		}
	} else {
		// Delete file
		if err := os.Remove(filePath); err != nil {
			return "", fmt.Errorf("failed to delete file: %w", err)
		}
	}

	return fmt.Sprintf("Successfully deleted %s", filePath), nil
}

func CreateDirectory(params map[string]interface{}, workingDir string) (string, error) {
	dirPath, ok := params["path"].(string)
	if !ok {
		return "", fmt.Errorf("path parameter is required")
	}

	// Resolve path relative to working directory
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(workingDir, dirPath)
	}

	// Check if path already exists
	if info, err := os.Stat(dirPath); err == nil {
		if info.IsDir() {
			return fmt.Sprintf("Directory already exists: %s", dirPath), nil
		}
		return "", fmt.Errorf("a file already exists at path: %s", dirPath)
	}

	// Create directory with all parent directories
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	return fmt.Sprintf("Created directory: %s", dirPath), nil
}

func ListFiles(params map[string]interface{}, workingDir string) (string, error) {
	directory := workingDir
	if dir, ok := params["directory"].(string); ok && dir != "" {
		if !filepath.IsAbs(dir) {
			directory = filepath.Join(workingDir, dir)
		} else {
			directory = dir
		}
	}

	recursive := false
	if rec, ok := params["recursive"].(bool); ok {
		recursive = rec
	}

	var result strings.Builder

	if recursive {
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relPath, _ := filepath.Rel(directory, path)
			if relPath == "." {
				return nil
			}
			if info.IsDir() {
				result.WriteString(fmt.Sprintf("[DIR]  %s\n", relPath))
			} else {
				result.WriteString(fmt.Sprintf("[FILE] %s (%d bytes)\n", relPath, info.Size()))
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to walk directory: %w", err)
		}
	} else {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return "", fmt.Errorf("failed to read directory: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				result.WriteString(fmt.Sprintf("[DIR]  %s\n", entry.Name()))
			} else {
				info, _ := entry.Info()
				result.WriteString(fmt.Sprintf("[FILE] %s (%d bytes)\n", entry.Name(), info.Size()))
			}
		}
	}

	return result.String(), nil
}

func FindFiles(params map[string]interface{}, workingDir string) (string, error) {
	pattern, ok := params["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern parameter is required")
	}

	directory := workingDir
	if dir, ok := params["directory"].(string); ok && dir != "" {
		if !filepath.IsAbs(dir) {
			directory = filepath.Join(workingDir, dir)
		} else {
			directory = dir
		}
	}

	// Parse exclude patterns
	excludes := make([]string, 0)
	if excludeParam, ok := params["exclude"].([]interface{}); ok {
		for _, e := range excludeParam {
			if excludeStr, ok := e.(string); ok {
				excludes = append(excludes, excludeStr)
			}
		}
	}

	var matches []string
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on errors
		}

		// Get relative path
		relPath, _ := filepath.Rel(directory, path)
		if relPath == "." {
			return nil
		}

		// Check exclusions
		for _, exclude := range excludes {
			matched, _ := filepath.Match(exclude, filepath.Base(path))
			if matched || strings.Contains(relPath, exclude) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Check if matches pattern
		if !info.IsDir() {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				matches = append(matches, relPath)
			}
			// Also support ** glob for recursive matching
			if strings.Contains(pattern, "**") {
				// Simple ** handling: match anywhere in path
				simplePattern := strings.ReplaceAll(pattern, "**/*", "*")
				simplePattern = strings.ReplaceAll(simplePattern, "**/", "")
				matched, _ := filepath.Match(simplePattern, filepath.Base(path))
				if matched {
					matches = append(matches, relPath)
				}
			}
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(matches) == 0 {
		return "No files found matching pattern", nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d files matching '%s':\n", len(matches), pattern))
	for _, match := range matches {
		result.WriteString(fmt.Sprintf("  %s\n", match))
	}

	return result.String(), nil
}
