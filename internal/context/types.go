package context

import "time"

// ProjectContext represents the full analyzed project state
type ProjectContext struct {
	RootPath       string          `json:"root_path"`
	ProjectType    string          `json:"project_type"`    // go, nodejs, python, rust, etc.
	ModuleName     string          `json:"module_name"`     // go module, npm package name, etc.
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Structure      *DirectoryTree  `json:"structure"`
	ImportantFiles []FileAnalysis  `json:"important_files"`
	Dependencies   []string        `json:"dependencies"`    // from go.mod, package.json, etc.
	BuildCommands  []string        `json:"build_commands"`  // from Makefile
	GitInfo        *GitInfo        `json:"git_info,omitempty"`
}

// DirectoryTree represents the project structure with unlimited depth
type DirectoryTree struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`               // relative path from root
	IsDir    bool             `json:"is_dir"`
	Size     int64            `json:"size,omitempty"`
	Children []*DirectoryTree `json:"children,omitempty"`
	FileType string           `json:"file_type,omitempty"` // go, js, py, md, etc.
}

// FileAnalysis represents an analyzed important file
type FileAnalysis struct {
	Path       string   `json:"path"`
	Type       string   `json:"type"`        // entry_point, config, module, test, documentation
	Language   string   `json:"language"`
	Summary    string   `json:"summary"`     // pattern-based summary
	LineCount  int      `json:"line_count"`
	Exports    []string `json:"exports,omitempty"`    // exported functions/types
	Imports    []string `json:"imports,omitempty"`    // imported packages
	Importance int      `json:"importance"`  // 1-10 score
}

// GitInfo captures repository state
type GitInfo struct {
	Branch         string `json:"branch"`
	RemoteURL      string `json:"remote_url,omitempty"`
	HasUncommitted bool   `json:"has_uncommitted"`
	LastCommit     string `json:"last_commit"`
}

// FileTypeInfo defines metadata for important file patterns
type FileTypeInfo struct {
	Type       string
	Importance int
}
