package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/tara-vision/taracode/internal/assistant"
)

// fuzzyMatch returns a score for how well pattern matches candidate (higher is better, 0 means no match)
func fuzzyMatch(pattern, candidate string) int {
	if pattern == "" {
		return 1 // Empty pattern matches everything with low score
	}

	// Case-insensitive matching
	pattern = strings.ToLower(pattern)
	candidate = strings.ToLower(candidate)

	// Exact prefix match is best
	if strings.HasPrefix(candidate, pattern) {
		return 1000 + len(pattern)
	}

	// Basename prefix match (e.g., "main" matches "src/main.go")
	base := filepath.Base(candidate)
	baseLower := strings.ToLower(base)
	if strings.HasPrefix(baseLower, pattern) {
		return 900 + len(pattern)
	}

	// Contains substring
	if strings.Contains(candidate, pattern) {
		return 500
	}

	// Fuzzy: all chars in order (e.g., "mgo" matches "main.go")
	pi := 0
	for _, c := range candidate {
		if pi < len(pattern) && c == rune(pattern[pi]) {
			pi++
		}
	}
	if pi == len(pattern) {
		return 100 + pi
	}

	return 0
}

// matchCandidate holds a file path and its match score
type matchCandidate struct {
	path  string
	score int
}

// skipDirectories contains directories to exclude from file operations
var skipDirectories = []string{"node_modules", "vendor", "__pycache__", "dist", "build", ".git", ".taracode"}

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
	workingDir     string
	gitignoreRules []gitignorePattern
}

// gitignorePattern represents a parsed gitignore pattern
type gitignorePattern struct {
	pattern  string
	isNegate bool
	isDir    bool
}

// NewFileCompleter creates a new file completer for the given working directory
func NewFileCompleter(workingDir string) *FileCompleter {
	fc := &FileCompleter{workingDir: workingDir}
	fc.loadGitignore()
	return fc
}

// UpdateWorkingDir updates the working directory for file completion
func (f *FileCompleter) UpdateWorkingDir(newDir string) {
	f.workingDir = newDir
	f.loadGitignore()
}

// loadGitignore reads and parses .gitignore from the working directory
func (f *FileCompleter) loadGitignore() {
	f.gitignoreRules = nil

	gitignorePath := filepath.Join(f.workingDir, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		return // No .gitignore or can't read it
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		pattern := gitignorePattern{}

		// Check for negation
		if strings.HasPrefix(line, "!") {
			pattern.isNegate = true
			line = line[1:]
		}

		// Check for directory-only pattern
		if strings.HasSuffix(line, "/") {
			pattern.isDir = true
			line = strings.TrimSuffix(line, "/")
		}

		pattern.pattern = line
		f.gitignoreRules = append(f.gitignoreRules, pattern)
	}
}

// isIgnored checks if a path matches any gitignore pattern
func (f *FileCompleter) isIgnored(relPath string, isDir bool) bool {
	if len(f.gitignoreRules) == 0 {
		return false
	}

	ignored := false
	for _, rule := range f.gitignoreRules {
		// Directory-only patterns only match directories
		if rule.isDir && !isDir {
			continue
		}

		matched := matchGitignorePattern(rule.pattern, relPath)
		if matched {
			if rule.isNegate {
				ignored = false
			} else {
				ignored = true
			}
		}
	}

	return ignored
}

// matchGitignorePattern checks if a path matches a gitignore pattern
func matchGitignorePattern(pattern, path string) bool {
	// Handle patterns with leading /
	if strings.HasPrefix(pattern, "/") {
		// Anchored to root
		pattern = pattern[1:]
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// For patterns without /, match against basename
	if !strings.Contains(pattern, "/") {
		base := filepath.Base(path)
		matched, _ := filepath.Match(pattern, base)
		if matched {
			return true
		}
	}

	// Try matching full path
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	// Try matching with ** expansion (simplified)
	if strings.Contains(pattern, "**") {
		// Replace ** with * for simple matching
		simplePattern := strings.ReplaceAll(pattern, "**", "*")
		matched, _ := filepath.Match(simplePattern, path)
		return matched
	}

	// Check if any path component matches
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts {
		matched, _ := filepath.Match(pattern, part)
		if matched {
			return true
		}
	}

	return false
}

// maxDepth is the maximum directory depth to traverse
const maxDepth = 10

// getFilesWithGitignore returns all files and directories, respecting .gitignore
func (f *FileCompleter) getFilesWithGitignore() ([]string, error) {
	var items []string
	baseDepth := strings.Count(f.workingDir, string(filepath.Separator))

	err := filepath.Walk(f.workingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Check depth limit
		currentDepth := strings.Count(path, string(filepath.Separator)) - baseDepth
		if currentDepth > maxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
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
		for _, skip := range skipDirectories {
			if info.IsDir() && base == skip {
				return filepath.SkipDir
			}
		}

		relPath, _ := filepath.Rel(f.workingDir, path)
		if relPath == "." {
			return nil
		}

		// Check gitignore patterns
		if f.isIgnored(relPath, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
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

// maxCompletionResults is the maximum number of completion candidates to show
const maxCompletionResults = 20

// Do implements readline.AutoCompleter interface with fuzzy matching
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

	// Get all project files (with gitignore filtering)
	files, err := f.getFilesWithGitignore()
	if err != nil || len(files) == 0 {
		return nil, 0
	}

	// Score all files using fuzzy matching
	var matches []matchCandidate
	for _, file := range files {
		score := fuzzyMatch(prefix, file)
		if score > 0 {
			matches = append(matches, matchCandidate{path: file, score: score})
		}
	}

	if len(matches) == 0 {
		return nil, 0
	}

	// Sort by score (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	// Limit results
	if len(matches) > maxCompletionResults {
		matches = matches[:maxCompletionResults]
	}

	// Build candidates - for readline we need to return the full replacement
	// since fuzzy matching might not share a common prefix
	var candidates [][]rune
	for _, m := range matches {
		// Return the full path as the completion
		candidates = append(candidates, []rune(m.path))
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
		for _, skip := range skipDirectories {
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

// getFilesInDirectory returns all files in a specific directory (non-recursive or recursive based on flag)
func getFilesInDirectory(dir string, baseDir string, recursive bool) ([]string, error) {
	var files []string

	walkFn := func(path string, info os.FileInfo, err error) error {
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
		for _, skip := range skipDirectories {
			if info.IsDir() && base == skip {
				return filepath.SkipDir
			}
		}

		// Skip the root directory itself
		if path == dir {
			return nil
		}

		// For non-recursive, skip subdirectories' contents
		if !recursive && info.IsDir() {
			return filepath.SkipDir
		}

		// Only include files, not directories
		if !info.IsDir() {
			relPath, _ := filepath.Rel(baseDir, path)
			files = append(files, relPath)
		}

		return nil
	}

	err := filepath.Walk(dir, walkFn)
	return files, err
}

// expandDirectoryReference expands a directory path to include all its files
func expandDirectoryReference(dirPath string, workingDir string) (string, error) {
	fullPath := filepath.Join(workingDir, dirPath)

	// Get all files in the directory (recursive)
	files, err := getFilesInDirectory(fullPath, workingDir, true)
	if err != nil {
		return "", fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	if len(files) == 0 {
		return fmt.Sprintf("\n\n**Directory: `%s`** (empty or no readable files)\n", dirPath), nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("\n\n**Directory: `%s`** (%d files)\n", dirPath, len(files)))

	for _, file := range files {
		filePath := filepath.Join(workingDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			// Skip files we can't read
			continue
		}

		// Determine file extension for syntax highlighting
		ext := filepath.Ext(file)
		if ext != "" {
			ext = ext[1:] // Remove leading dot
		}

		result.WriteString(fmt.Sprintf("\n**File: `%s`**\n```%s\n%s\n```\n", file, ext, string(content)))
	}

	return result.String(), nil
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
			// Extract the file path (first word), strip trailing punctuation
			filePath = strings.TrimRight(words[0], "?!,;:\"'")
			remainingText = strings.TrimPrefix(part, words[0])
		}

		fullPath := filepath.Join(workingDir, filePath)

		// Check if path is a directory
		info, err := os.Stat(fullPath)
		if err != nil {
			return "", fmt.Errorf("failed to access %s: %w", filePath, err)
		}

		if info.IsDir() {
			// Expand directory to include all files
			expanded, err := expandDirectoryReference(filePath, workingDir)
			if err != nil {
				return "", err
			}
			result += expanded + remainingText
		} else {
			// Read single file content
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
	}

	return result, nil
}

// ExpandedMessage contains both text and any images extracted from file references
type ExpandedMessage struct {
	Text   string
	Images []*assistant.ImageData
}

// expandFileReferencesWithImages detects @ symbols and expands them, separating images from text
// projectRoot is used for init check, workingDir is the current directory for file resolution
func expandFileReferencesWithImages(message string, projectRoot string, workingDir string) (*ExpandedMessage, error) {
	result := &ExpandedMessage{
		Text:   message,
		Images: nil,
	}

	// Pattern: @ followed by optional whitespace/path or standalone @
	if !strings.Contains(message, "@") {
		return result, nil
	}

	// Check if project is initialized (check at project root, not current dir)
	if !isInitializedProject(projectRoot) {
		// Don't expand @ in non-initialized projects
		return result, nil
	}

	// Find all @ positions
	parts := strings.Split(message, "@")
	if len(parts) == 1 {
		return result, nil // No @ found
	}

	var textResult strings.Builder
	textResult.WriteString(parts[0]) // Start with text before first @

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
				return nil, fmt.Errorf("file selection cancelled: %w", err)
			}
			filePath = selected
			remainingText = ""
		} else {
			// @ followed by path (e.g., @main.go or @src/main.go)
			// Extract the file path (first word), strip trailing punctuation
			filePath = strings.TrimRight(words[0], "?!,;:\"'")
			remainingText = strings.TrimPrefix(part, words[0])
		}

		fullPath := filepath.Join(workingDir, filePath)

		// Check if path exists
		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to access %s: %w", filePath, err)
		}

		if info.IsDir() {
			// Expand directory to include all files (skip images in directories for now)
			expanded, err := expandDirectoryReference(filePath, workingDir)
			if err != nil {
				return nil, err
			}
			textResult.WriteString(expanded)
			textResult.WriteString(remainingText)
		} else if assistant.IsImageFile(filePath) {
			// Load image and add to images slice
			img, err := assistant.LoadImage(fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load image %s: %w", filePath, err)
			}
			result.Images = append(result.Images, img)
			// Add a reference to the image in text for context
			textResult.WriteString(fmt.Sprintf("\n\n[Image: %s]\n", filePath))
			textResult.WriteString(remainingText)
			fmt.Printf("📷 Loaded image: %s\n", filePath)
		} else {
			// Read single text file content
			content, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
			}

			// Determine file extension for syntax highlighting hint
			ext := filepath.Ext(filePath)
			if ext != "" {
				ext = ext[1:] // Remove leading dot
			}

			// Append expanded file reference
			textResult.WriteString(fmt.Sprintf("\n\n**File: `%s`**\n```%s\n%s\n```%s",
				filePath, ext, string(content), remainingText))
		}
	}

	result.Text = textResult.String()
	return result, nil
}
