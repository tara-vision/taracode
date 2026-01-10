package context

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultExcludeDirs are directories to skip during exploration
var DefaultExcludeDirs = map[string]bool{
	".git":          true,
	".taracode":     true,
	"node_modules":  true,
	"vendor":        true,
	"__pycache__":   true,
	".venv":         true,
	"venv":          true,
	"env":           true,
	".idea":         true,
	".vscode":       true,
	"dist":          true,
	"build":         true,
	"target":        true,  // Rust
	".next":         true,  // Next.js
	"coverage":      true,
	".cache":        true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".tox":          true,
	".eggs":         true,
	"*.egg-info":    true,
	".bundle":       true,
	".sass-cache":   true,
	"bower_components": true,
	".terraform":    true,
	".serverless":   true,
}

// DefaultExcludePatterns are file patterns to skip
var DefaultExcludePatterns = []string{
	"*.lock",
	"*.sum",
	".DS_Store",
	"*.log",
	"*.min.js",
	"*.min.css",
	"*.map",
	"*.pyc",
	"*.pyo",
	"*.class",
	"*.o",
	"*.obj",
	"*.exe",
	"*.dll",
	"*.so",
	"*.dylib",
	"*.a",
	"*.lib",
	"*.bin",
	"*.out",
}

// ExplorerOptions configures the directory exploration
type ExplorerOptions struct {
	MaxDepth        int               // 0 = unlimited
	ExcludeDirs     map[string]bool
	ExcludePatterns []string
	IncludeHidden   bool
}

// DefaultExplorerOptions returns sensible defaults for exploration
func DefaultExplorerOptions() ExplorerOptions {
	return ExplorerOptions{
		MaxDepth:        0, // Unlimited
		ExcludeDirs:     DefaultExcludeDirs,
		ExcludePatterns: DefaultExcludePatterns,
		IncludeHidden:   false,
	}
}

// ExploreProject builds a complete directory tree starting from rootPath
func ExploreProject(rootPath string, opts ExplorerOptions) (*DirectoryTree, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	return exploreRecursive(absRoot, absRoot, 0, opts)
}

func exploreRecursive(rootPath, currentPath string, depth int, opts ExplorerOptions) (*DirectoryTree, error) {
	info, err := os.Stat(currentPath)
	if err != nil {
		return nil, err
	}

	relPath, _ := filepath.Rel(rootPath, currentPath)
	if relPath == "." {
		relPath = ""
	}

	node := &DirectoryTree{
		Name:  info.Name(),
		Path:  relPath,
		IsDir: info.IsDir(),
		Size:  info.Size(),
	}

	if !info.IsDir() {
		node.FileType = DetectFileType(info.Name())
		return node, nil
	}

	// Check depth limit (0 = unlimited)
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		return node, nil
	}

	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return node, nil // Return partial result on error
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files/dirs unless explicitly included
		if !opts.IncludeHidden && strings.HasPrefix(name, ".") {
			continue
		}

		// Skip excluded directories
		if entry.IsDir() && opts.ExcludeDirs[name] {
			continue
		}

		// Skip excluded file patterns
		if !entry.IsDir() && matchesExcludePattern(name, opts.ExcludePatterns) {
			continue
		}

		childPath := filepath.Join(currentPath, name)
		child, err := exploreRecursive(rootPath, childPath, depth+1, opts)
		if err == nil && child != nil {
			node.Children = append(node.Children, child)
		}
	}

	return node, nil
}

// DetectFileType returns a language/type identifier based on file extension
func DetectFileType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	// Handle files without extensions
	baseName := strings.ToLower(filepath.Base(filename))
	switch baseName {
	case "makefile", "gnumakefile":
		return "makefile"
	case "dockerfile":
		return "dockerfile"
	case "vagrantfile":
		return "ruby"
	case "rakefile", "gemfile":
		return "ruby"
	case "procfile":
		return "procfile"
	case "cmakelists.txt":
		return "cmake"
	}

	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".py", ".pyw", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	case ".rb", ".rake":
		return "ruby"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".c":
		return "c"
	case ".cpp", ".cc", ".cxx", ".c++":
		return "cpp"
	case ".h", ".hpp", ".hxx":
		return "header"
	case ".cs":
		return "csharp"
	case ".swift":
		return "swift"
	case ".m", ".mm":
		return "objc"
	case ".php":
		return "php"
	case ".lua":
		return "lua"
	case ".pl", ".pm":
		return "perl"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".ps1", ".psm1":
		return "powershell"
	case ".r":
		return "r"
	case ".sql":
		return "sql"
	case ".md", ".markdown":
		return "markdown"
	case ".rst":
		return "rst"
	case ".txt":
		return "text"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css", ".scss", ".sass", ".less":
		return "css"
	case ".vue":
		return "vue"
	case ".svelte":
		return "svelte"
	case ".proto":
		return "protobuf"
	case ".graphql", ".gql":
		return "graphql"
	case ".tf", ".tfvars":
		return "terraform"
	case ".hcl":
		return "hcl"
	case ".zig":
		return "zig"
	case ".nim":
		return "nim"
	case ".ex", ".exs":
		return "elixir"
	case ".erl", ".hrl":
		return "erlang"
	case ".clj", ".cljs", ".cljc":
		return "clojure"
	case ".hs", ".lhs":
		return "haskell"
	case ".ml", ".mli":
		return "ocaml"
	case ".fs", ".fsx":
		return "fsharp"
	default:
		if ext != "" {
			return strings.TrimPrefix(ext, ".")
		}
		return ""
	}
}

func matchesExcludePattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// CountFiles returns the total number of files in the tree
func CountFiles(tree *DirectoryTree) int {
	if tree == nil {
		return 0
	}
	if !tree.IsDir {
		return 1
	}
	count := 0
	for _, child := range tree.Children {
		count += CountFiles(child)
	}
	return count
}

// CountDirs returns the total number of directories in the tree
func CountDirs(tree *DirectoryTree) int {
	if tree == nil {
		return 0
	}
	if !tree.IsDir {
		return 0
	}
	count := 1
	for _, child := range tree.Children {
		count += CountDirs(child)
	}
	return count
}

// GetMaxDepth returns the maximum depth of the tree
func GetMaxDepth(tree *DirectoryTree) int {
	if tree == nil {
		return 0
	}
	if !tree.IsDir || len(tree.Children) == 0 {
		return 0
	}
	maxChildDepth := 0
	for _, child := range tree.Children {
		childDepth := GetMaxDepth(child)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}
	return maxChildDepth + 1
}
