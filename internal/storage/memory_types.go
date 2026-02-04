package storage

import "time"

// MemoryCategory categorizes the type of memory
type MemoryCategory string

const (
	MemoryCategoryDecision MemoryCategory = "decision" // Architectural/design decisions with rationale
	MemoryCategoryPattern  MemoryCategory = "pattern"  // Code style preferences, conventions
	MemoryCategoryError    MemoryCategory = "error"    // Common errors and their solutions
	MemoryCategoryLearning MemoryCategory = "learning" // General observations about the project
)

// MemorySource indicates how the memory was created
type MemorySource string

const (
	MemorySourceManual     MemorySource = "manual"     // User explicitly saved via /remember
	MemorySourceAuto       MemorySource = "auto"       // Auto-captured from conversation
	MemorySourceCorrection MemorySource = "correction" // Captured from user correction
	MemorySourceImport     MemorySource = "import"     // Imported from external file
)

// Memory represents a single piece of project knowledge
type Memory struct {
	ID         string         `json:"id"`
	Category   MemoryCategory `json:"category"`
	Content    string         `json:"content"`
	Context    string         `json:"context,omitempty"` // Additional context about when/why
	Tags       []string       `json:"tags,omitempty"`    // Searchable tags
	Source     MemorySource   `json:"source"`
	CreatedAt  time.Time      `json:"created_at"`
	LastUsedAt time.Time      `json:"last_used_at"`
	UseCount   int            `json:"use_count"` // Times injected into context
}

// MemoryIndex tracks all memories for quick lookup
type MemoryIndex struct {
	Memories    []MemoryMetadata `json:"memories"`
	TotalCount  int              `json:"total_count"`
	LastUpdated time.Time        `json:"last_updated"`
}

// MemoryMetadata contains summary info for listing memories
type MemoryMetadata struct {
	ID         string         `json:"id"`
	Category   MemoryCategory `json:"category"`
	Preview    string         `json:"preview"` // First 80 chars of content
	Tags       []string       `json:"tags,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	LastUsedAt time.Time      `json:"last_used_at"`
	UseCount   int            `json:"use_count"`
}

// MemoryConfig stores memory feature configuration
type MemoryConfig struct {
	Enabled          bool             `json:"enabled"`
	MaxMemories      int              `json:"max_memories"`
	MaxContextTokens int              `json:"max_context_tokens"`   // Max tokens to inject into prompt
	RetentionDays    int              `json:"retention_days"`       // Auto-cleanup after N days of non-use
	AutoCapture      bool             `json:"auto_capture"`         // Detect and suggest memories
	Categories       []MemoryCategory `json:"categories,omitempty"` // Enabled categories (nil = all)
}

// DefaultMemoryConfig returns sensible defaults for memory configuration
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		Enabled:          true,
		MaxMemories:      500,
		MaxContextTokens: 2000,
		RetentionDays:    90,
		AutoCapture:      true,
		Categories:       nil, // All categories enabled
	}
}

// MemoryExport represents the format for exporting/importing memories
type MemoryExport struct {
	Version    string    `json:"version"`
	ExportedAt time.Time `json:"exported_at"`
	Memories   []Memory  `json:"memories"`
}

// MemoryStats contains statistics about the memory store
type MemoryStats struct {
	TotalMemories   int            `json:"total_memories"`
	ByCategory      map[string]int `json:"by_category"`
	BySource        map[string]int `json:"by_source"`
	TotalUseCount   int            `json:"total_use_count"`
	OldestMemory    *time.Time     `json:"oldest_memory,omitempty"`
	NewestMemory    *time.Time     `json:"newest_memory,omitempty"`
	MostUsedID      string         `json:"most_used_id,omitempty"`
	MostUsedContent string         `json:"most_used_content,omitempty"`
	MostUsedCount   int            `json:"most_used_count"`
	UnusedCount     int            `json:"unused_count"` // Memories never injected
	EstimatedTokens int            `json:"estimated_tokens"`
}
