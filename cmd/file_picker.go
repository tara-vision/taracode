package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
)

// isInitializedProject checks if current directory has TARACODE.md and .taracode/
func isInitializedProject(workingDir string) bool {
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	taracodeDir := filepath.Join(workingDir, ".taracode")

	if _, err := os.Stat(taracodeFile); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(taracodeDir); os.IsNotExist(err) {
		return false
	}
	return true
}

// FileCompleter implements readline.AutoCompleter for @ file references
type FileCompleter struct {
	workingDir string
}

// NewFileCompleter creates a new file completer for the given working directory
func NewFileCompleter(workingDir string) *FileCompleter {
	return &FileCompleter{workingDir: workingDir}
}

// Do implements readline.AutoCompleter interface
func (f *FileCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	// Only complete if we're in an initialized project
	if !isInitializedProject(f.workingDir) {
		return nil, 0
	}

	// Find the @ symbol before cursor
	lineStr := string(line[:pos])
	lastAtIdx := strings.LastIndex(lineStr, "@")
	if lastAtIdx == -1 {
		return nil, 0
	}

	// Get the partial path after @
	prefix := lineStr[lastAtIdx+1:]

	// Get all project files
	files, err := getFilesRecursive(f.workingDir)
	if err != nil || len(files) == 0 {
		return nil, 0
	}

	// Filter files matching prefix
	var candidates [][]rune
	prefixLower := strings.ToLower(prefix)
	for _, file := range files {
		fileLower := strings.ToLower(file)
		if prefix == "" || strings.HasPrefix(fileLower, prefixLower) {
			// Return the remaining part to complete
			remaining := file[len(prefix):]
			candidates = append(candidates, []rune(remaining))
		}
	}

	// Length is the part we're replacing (the prefix after @)
	return candidates, len(prefix)
}

// getFilesRecursive returns all files and directories in directory recursively
func getFilesRecursive(dir string) ([]string, error) {
	var items []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		base := filepath.Base(path)

		// Skip hidden files/dirs
		if strings.HasPrefix(base, ".") && base != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common directories
		skipDirs := []string{"node_modules", "vendor", "__pycache__", "dist", "build"}
		for _, skip := range skipDirs {
			if info.IsDir() && base == skip {
				return filepath.SkipDir
			}
		}

		relPath, _ := filepath.Rel(dir, path)
		if relPath == "." {
			return nil
		}

		// Include both files and directories
		if info.IsDir() {
			items = append(items, relPath+"/") // Add trailing slash for dirs
		} else {
			items = append(items, relPath)
		}

		return nil
	})
	return items, err
}

// selectFile shows interactive file picker and returns selected file path
func selectFile(workingDir string) (string, error) {
	files, err := getFilesRecursive(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to list files: %w", err)
	}

	// Filter to only show files, not directories
	var fileOnly []string
	for _, f := range files {
		if !strings.HasSuffix(f, "/") {
			fileOnly = append(fileOnly, f)
		}
	}

	if len(fileOnly) == 0 {
		return "", fmt.Errorf("no files found in directory")
	}

	// Configure promptui selector
	searcher := func(input string, index int) bool {
		file := fileOnly[index]
		input = strings.ToLower(input)
		file = strings.ToLower(file)
		return strings.Contains(file, input)
	}

	prompt := promptui.Select{
		Label:             "Select a file",
		Items:             fileOnly,
		Size:              20,
		Searcher:          searcher,
		StartInSearchMode: true,
		HideSelected:      true,
	}

	_, result, err := prompt.Run()
	if err != nil {
		return "", err
	}

	return result, nil
}

// expandFileReferences detects @ symbols and expands them with file content
func expandFileReferences(message string, workingDir string) (string, error) {
	// Pattern: @ followed by optional whitespace/path or standalone @
	if !strings.Contains(message, "@") {
		return message, nil
	}

	// Check if project is initialized
	if !isInitializedProject(workingDir) {
		// Don't expand @ in non-initialized projects
		return message, nil
	}

	// Find all @ positions
	parts := strings.Split(message, "@")
	if len(parts) == 1 {
		return message, nil // No @ found
	}

	result := parts[0] // Start with text before first @

	for i := 1; i < len(parts); i++ {
		part := parts[i]
		words := strings.Fields(part)

		var filePath string
		var remainingText string

		if len(words) == 0 {
			// Standalone @ at end - show picker as fallback
			fmt.Println("\n📁 Select a file (or use Tab after @ for completion):")
			selected, err := selectFile(workingDir)
			if err != nil {
				return "", fmt.Errorf("file selection cancelled: %w", err)
			}
			filePath = selected
			remainingText = ""
		} else {
			// @ followed by path (e.g., @main.go or @src/main.go)
			// Extract the file path (first word)
			filePath = words[0]
			remainingText = strings.TrimPrefix(part, filePath)
		}

		// Read file content
		fullPath := filepath.Join(workingDir, filePath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		// Determine file extension for syntax highlighting hint
		ext := filepath.Ext(filePath)
		if ext != "" {
			ext = ext[1:] // Remove leading dot
		}

		// Append expanded file reference
		result += fmt.Sprintf("\n\n**File: `%s`**\n```%s\n%s\n```%s",
			filePath, ext, string(content), remainingText)
	}

	return result, nil
}
