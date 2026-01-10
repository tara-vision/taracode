package context

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ImportantFilePatterns defines files that should be analyzed with their type and importance
var ImportantFilePatterns = map[string]FileTypeInfo{
	// Entry points - highest importance
	"main.go":    {Type: "entry_point", Importance: 10},
	"main.py":    {Type: "entry_point", Importance: 10},
	"main.rs":    {Type: "entry_point", Importance: 10},
	"main.js":    {Type: "entry_point", Importance: 10},
	"main.ts":    {Type: "entry_point", Importance: 10},
	"index.js":   {Type: "entry_point", Importance: 9},
	"index.ts":   {Type: "entry_point", Importance: 9},
	"index.tsx":  {Type: "entry_point", Importance: 9},
	"app.py":     {Type: "entry_point", Importance: 9},
	"app.js":     {Type: "entry_point", Importance: 9},
	"app.ts":     {Type: "entry_point", Importance: 9},
	"lib.rs":     {Type: "entry_point", Importance: 9},
	"mod.rs":     {Type: "module", Importance: 7},
	"__init__.py": {Type: "module", Importance: 6},

	// Configuration files
	"go.mod":           {Type: "config", Importance: 9},
	"go.sum":           {Type: "config", Importance: 3},
	"package.json":     {Type: "config", Importance: 9},
	"Cargo.toml":       {Type: "config", Importance: 9},
	"pyproject.toml":   {Type: "config", Importance: 9},
	"setup.py":         {Type: "config", Importance: 8},
	"requirements.txt": {Type: "config", Importance: 8},
	"tsconfig.json":    {Type: "config", Importance: 8},
	"webpack.config.js": {Type: "config", Importance: 7},
	"vite.config.ts":   {Type: "config", Importance: 7},
	"vite.config.js":   {Type: "config", Importance: 7},
	"rollup.config.js": {Type: "config", Importance: 7},
	"babel.config.js":  {Type: "config", Importance: 6},
	".eslintrc.js":     {Type: "config", Importance: 5},
	".eslintrc.json":   {Type: "config", Importance: 5},
	".prettierrc":      {Type: "config", Importance: 4},
	"jest.config.js":   {Type: "config", Importance: 6},
	"vitest.config.ts": {Type: "config", Importance: 6},

	// Build files
	"Makefile":           {Type: "build", Importance: 8},
	"CMakeLists.txt":     {Type: "build", Importance: 8},
	"Dockerfile":         {Type: "build", Importance: 7},
	"docker-compose.yml": {Type: "build", Importance: 7},
	"docker-compose.yaml": {Type: "build", Importance: 7},
	".goreleaser.yml":    {Type: "build", Importance: 6},
	".goreleaser.yaml":   {Type: "build", Importance: 6},

	// Documentation
	"README.md":       {Type: "documentation", Importance: 9},
	"README":          {Type: "documentation", Importance: 8},
	"CONTRIBUTING.md": {Type: "documentation", Importance: 6},
	"CHANGELOG.md":    {Type: "documentation", Importance: 5},
	"LICENSE":         {Type: "documentation", Importance: 4},
	"LICENSE.md":      {Type: "documentation", Importance: 4},

	// CI/CD
	".github/workflows/ci.yml":    {Type: "ci", Importance: 6},
	".github/workflows/ci.yaml":   {Type: "ci", Importance: 6},
	".github/workflows/main.yml":  {Type: "ci", Importance: 6},
	".github/workflows/main.yaml": {Type: "ci", Importance: 6},
	".gitlab-ci.yml":              {Type: "ci", Importance: 6},
	"Jenkinsfile":                 {Type: "ci", Importance: 6},
}

// ImportantDirPatterns identify important directories with their importance score
var ImportantDirPatterns = map[string]int{
	"cmd":        9,  // Go command packages
	"internal":   8,  // Go internal packages
	"pkg":        8,  // Go public packages
	"src":        8,  // Source directories
	"lib":        8,  // Library code
	"api":        8,  // API definitions
	"routes":     7,  // Web routes
	"handlers":   7,  // Request handlers
	"controllers": 7, // Controllers
	"models":     7,  // Data models
	"services":   7,  // Business logic
	"utils":      5,  // Utilities
	"helpers":    5,  // Helpers
	"tests":      6,  // Test files
	"test":       6,  // Test directory
	"spec":       6,  // Spec files
	"components": 7,  // UI components
	"views":      6,  // View templates
	"templates":  6,  // Templates
	"static":     4,  // Static assets
	"public":     4,  // Public assets
	"assets":     4,  // Assets
	"scripts":    5,  // Scripts
	"bin":        5,  // Binaries
	"config":     6,  // Configuration
	"configs":    6,  // Configurations
	"migrations": 5,  // Database migrations
	"db":         5,  // Database
}

// AnalyzeImportantFiles finds and analyzes key project files from the directory tree
func AnalyzeImportantFiles(rootPath string, tree *DirectoryTree) []FileAnalysis {
	var analyses []FileAnalysis

	analyzeTreeRecursive(rootPath, tree, &analyses)

	// Sort by importance (highest first)
	sort.Slice(analyses, func(i, j int) bool {
		return analyses[i].Importance > analyses[j].Importance
	})

	return analyses
}

func analyzeTreeRecursive(rootPath string, node *DirectoryTree, analyses *[]FileAnalysis) {
	if node == nil {
		return
	}

	if !node.IsDir {
		// Check if this is an important file by exact name match
		filename := filepath.Base(node.Path)
		if info, found := ImportantFilePatterns[filename]; found {
			analysis := analyzeFile(rootPath, node.Path, info)
			if analysis != nil {
				*analyses = append(*analyses, *analysis)
			}
		} else {
			// Check by relative path (for files like .github/workflows/ci.yml)
			if info, found := ImportantFilePatterns[node.Path]; found {
				analysis := analyzeFile(rootPath, node.Path, info)
				if analysis != nil {
					*analyses = append(*analyses, *analysis)
				}
			}
		}
		return
	}

	for _, child := range node.Children {
		analyzeTreeRecursive(rootPath, child, analyses)
	}
}

func analyzeFile(rootPath, relPath string, info FileTypeInfo) *FileAnalysis {
	fullPath := filepath.Join(rootPath, relPath)

	file, err := os.Open(fullPath)
	if err != nil {
		return nil
	}
	defer file.Close()

	analysis := &FileAnalysis{
		Path:       relPath,
		Type:       info.Type,
		Language:   DetectFileType(relPath),
		Importance: info.Importance,
	}

	// Count lines and extract basic info
	scanner := bufio.NewScanner(file)
	lineCount := 0
	var imports []string
	var exports []string

	// Limit scanning to first 500 lines for performance
	maxLines := 500

	for scanner.Scan() && lineCount < maxLines {
		lineCount++
		line := scanner.Text()

		// Language-specific extraction
		switch analysis.Language {
		case "go":
			extractGoInfo(line, &imports, &exports)
		case "javascript", "typescript":
			extractJSInfo(line, &imports, &exports)
		case "python":
			extractPythonInfo(line, &imports, &exports)
		case "rust":
			extractRustInfo(line, &imports, &exports)
		}
	}

	// Continue counting lines if we hit the limit
	for scanner.Scan() {
		lineCount++
	}

	analysis.LineCount = lineCount

	// Deduplicate and limit imports/exports
	analysis.Imports = deduplicateStrings(imports, 20)
	analysis.Exports = deduplicateStrings(exports, 20)
	analysis.Summary = generateSummary(analysis)

	return analysis
}

func extractGoInfo(line string, imports, exports *[]string) {
	line = strings.TrimSpace(line)

	// Imports
	if strings.HasPrefix(line, "import ") || (strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`)) {
		re := regexp.MustCompile(`"([^"]+)"`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*imports = append(*imports, matches[1])
		}
	}

	// Exports (public functions/types - starts with uppercase)
	if strings.HasPrefix(line, "func ") {
		// Match "func Name(" or "func (receiver) Name("
		re := regexp.MustCompile(`func\s+(?:\([^)]+\)\s+)?([A-Z][a-zA-Z0-9_]*)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*exports = append(*exports, matches[1])
		}
	}
	if strings.HasPrefix(line, "type ") {
		re := regexp.MustCompile(`type\s+([A-Z][a-zA-Z0-9_]*)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*exports = append(*exports, matches[1])
		}
	}
	if strings.HasPrefix(line, "var ") || strings.HasPrefix(line, "const ") {
		re := regexp.MustCompile(`(?:var|const)\s+([A-Z][a-zA-Z0-9_]*)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*exports = append(*exports, matches[1])
		}
	}
}

func extractJSInfo(line string, imports, exports *[]string) {
	line = strings.TrimSpace(line)

	// Imports
	if strings.Contains(line, "import ") || strings.Contains(line, "require(") {
		re := regexp.MustCompile(`['"]([^'"]+)['"]`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			// Skip relative imports for brevity
			if !strings.HasPrefix(matches[1], ".") {
				*imports = append(*imports, matches[1])
			}
		}
	}

	// Exports
	if strings.HasPrefix(line, "export ") {
		re := regexp.MustCompile(`export\s+(?:default\s+)?(?:async\s+)?(?:function|class|const|let|var|interface|type)\s+(\w+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*exports = append(*exports, matches[1])
		}
	}
	// module.exports
	if strings.Contains(line, "module.exports") {
		*exports = append(*exports, "module.exports")
	}
}

func extractPythonInfo(line string, imports, exports *[]string) {
	line = strings.TrimSpace(line)

	// Imports
	if strings.HasPrefix(line, "import ") {
		re := regexp.MustCompile(`import\s+(\S+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			// Get the top-level module
			parts := strings.Split(matches[1], ".")
			*imports = append(*imports, parts[0])
		}
	}
	if strings.HasPrefix(line, "from ") {
		re := regexp.MustCompile(`from\s+(\S+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			if !strings.HasPrefix(matches[1], ".") {
				parts := strings.Split(matches[1], ".")
				*imports = append(*imports, parts[0])
			}
		}
	}

	// Class/function definitions (potential exports - public if not starting with _)
	if strings.HasPrefix(line, "def ") {
		re := regexp.MustCompile(`def\s+(\w+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			if !strings.HasPrefix(matches[1], "_") {
				*exports = append(*exports, matches[1])
			}
		}
	}
	if strings.HasPrefix(line, "class ") {
		re := regexp.MustCompile(`class\s+(\w+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			if !strings.HasPrefix(matches[1], "_") {
				*exports = append(*exports, matches[1])
			}
		}
	}
}

func extractRustInfo(line string, imports, exports *[]string) {
	line = strings.TrimSpace(line)

	// Use statements
	if strings.HasPrefix(line, "use ") {
		re := regexp.MustCompile(`use\s+(\w+)`)
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			*imports = append(*imports, matches[1])
		}
	}

	// Public items
	if strings.HasPrefix(line, "pub ") {
		if strings.Contains(line, "fn ") {
			re := regexp.MustCompile(`pub\s+(?:async\s+)?fn\s+(\w+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				*exports = append(*exports, matches[1])
			}
		} else if strings.Contains(line, "struct ") {
			re := regexp.MustCompile(`pub\s+struct\s+(\w+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				*exports = append(*exports, matches[1])
			}
		} else if strings.Contains(line, "enum ") {
			re := regexp.MustCompile(`pub\s+enum\s+(\w+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				*exports = append(*exports, matches[1])
			}
		} else if strings.Contains(line, "trait ") {
			re := regexp.MustCompile(`pub\s+trait\s+(\w+)`)
			if matches := re.FindStringSubmatch(line); len(matches) > 1 {
				*exports = append(*exports, matches[1])
			}
		}
	}
}

func generateSummary(analysis *FileAnalysis) string {
	var parts []string

	parts = append(parts, analysis.Type)
	parts = append(parts, fmt.Sprintf("%d lines", analysis.LineCount))

	if len(analysis.Exports) > 0 {
		if len(analysis.Exports) <= 3 {
			parts = append(parts, fmt.Sprintf("exports: %s", strings.Join(analysis.Exports, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("%d exports", len(analysis.Exports)))
		}
	}

	return strings.Join(parts, " | ")
}

func deduplicateStrings(items []string, maxItems int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))

	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
			if len(result) >= maxItems {
				break
			}
		}
	}

	return result
}

// GetDirImportance returns the importance score for a directory name
func GetDirImportance(dirName string) int {
	if importance, ok := ImportantDirPatterns[strings.ToLower(dirName)]; ok {
		return importance
	}
	return 0
}
