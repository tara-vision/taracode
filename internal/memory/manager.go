package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tara-vision/taracode/internal/storage"
)

const (
	// MaxPreviewLength is the maximum length for memory previews
	MaxPreviewLength = 80
	// DefaultMaxMemories is the default maximum number of memories
	DefaultMaxMemories = 500
	// DefaultMaxContextTokens is the default max tokens for context injection
	DefaultMaxContextTokens = 2000
	// DefaultRetentionDays is the default retention period
	DefaultRetentionDays = 90
)

// Manager handles memory storage and retrieval for a project
type Manager struct {
	rootDir string // .taracode directory path
	index   *storage.MemoryIndex
	mu      sync.RWMutex
}

// NewManager creates a new memory manager for the given project
func NewManager(taracodeDir string) (*Manager, error) {
	m := &Manager{
		rootDir: taracodeDir,
	}

	// Ensure memory directory exists
	memoryDir := filepath.Join(taracodeDir, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Load existing index or create new
	if err := m.loadIndex(); err != nil {
		return nil, err
	}

	return m, nil
}

// getIndexPath returns the path to the index file
func (m *Manager) getIndexPath() string {
	return filepath.Join(m.rootDir, "memory", "index.json")
}

// getMemoryPath returns the path to a specific memory file
func (m *Manager) getMemoryPath(id string) string {
	return filepath.Join(m.rootDir, "memory", fmt.Sprintf("memory_%s.json", id))
}

// loadIndex reads the memory index from disk
func (m *Manager) loadIndex() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	indexPath := m.getIndexPath()
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize new index
			m.index = &storage.MemoryIndex{
				Memories:    []storage.MemoryMetadata{},
				TotalCount:  0,
				LastUpdated: time.Now(),
			}
			return nil
		}
		return fmt.Errorf("failed to read index file: %w", err)
	}

	var index storage.MemoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		// Corrupted file, start fresh
		m.index = &storage.MemoryIndex{
			Memories:    []storage.MemoryMetadata{},
			TotalCount:  0,
			LastUpdated: time.Now(),
		}
		return nil
	}

	m.index = &index
	return nil
}

// saveIndex writes the memory index to disk
func (m *Manager) saveIndex() error {
	indexPath := m.getIndexPath()
	data, err := json.MarshalIndent(m.index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	return os.WriteFile(indexPath, data, 0644)
}

// Create saves a new memory and returns its ID
func (m *Manager) Create(category storage.MemoryCategory, content, context string, tags []string, source storage.MemorySource) (*storage.Memory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID
	id := uuid.New().String()[:8] // Short ID for easier reference

	now := time.Now()
	mem := &storage.Memory{
		ID:         id,
		Category:   category,
		Content:    content,
		Context:    context,
		Tags:       tags,
		Source:     source,
		CreatedAt:  now,
		LastUsedAt: now,
		UseCount:   0,
	}

	// Save memory file
	memPath := m.getMemoryPath(id)
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal memory: %w", err)
	}
	if err := os.WriteFile(memPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write memory file: %w", err)
	}

	// Update index
	preview := content
	if len(preview) > MaxPreviewLength {
		preview = preview[:MaxPreviewLength-3] + "..."
	}

	meta := storage.MemoryMetadata{
		ID:         id,
		Category:   category,
		Preview:    preview,
		Tags:       tags,
		CreatedAt:  now,
		LastUsedAt: now,
		UseCount:   0,
	}

	m.index.Memories = append(m.index.Memories, meta)
	m.index.TotalCount++
	m.index.LastUpdated = now

	if err := m.saveIndex(); err != nil {
		return nil, err
	}

	return mem, nil
}

// Get retrieves a memory by ID (supports partial ID matching)
func (m *Manager) Get(id string) (*storage.Memory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find matching ID (support partial match)
	fullID := m.findMatchingID(id)
	if fullID == "" {
		return nil, fmt.Errorf("memory not found: %s", id)
	}

	memPath := m.getMemoryPath(fullID)
	data, err := os.ReadFile(memPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory file: %w", err)
	}

	var mem storage.Memory
	if err := json.Unmarshal(data, &mem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal memory: %w", err)
	}

	return &mem, nil
}

// findMatchingID finds a full ID from a partial ID (must hold lock)
func (m *Manager) findMatchingID(partialID string) string {
	var matches []string
	for _, meta := range m.index.Memories {
		if strings.HasPrefix(meta.ID, partialID) {
			matches = append(matches, meta.ID)
		}
	}

	if len(matches) == 1 {
		return matches[0]
	}

	// Check exact match if multiple partial matches
	for _, id := range matches {
		if id == partialID {
			return id
		}
	}

	return "" // No unique match
}

// Delete removes a memory by ID
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find matching ID
	fullID := m.findMatchingID(id)
	if fullID == "" {
		return fmt.Errorf("memory not found: %s", id)
	}

	// Remove from index
	newMemories := make([]storage.MemoryMetadata, 0, len(m.index.Memories)-1)
	for _, meta := range m.index.Memories {
		if meta.ID != fullID {
			newMemories = append(newMemories, meta)
		}
	}
	m.index.Memories = newMemories
	m.index.TotalCount = len(newMemories)
	m.index.LastUpdated = time.Now()

	if err := m.saveIndex(); err != nil {
		return err
	}

	// Remove memory file
	memPath := m.getMemoryPath(fullID)
	if err := os.Remove(memPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete memory file: %w", err)
	}

	return nil
}

// List returns all memory metadata
func (m *Manager) List() []storage.MemoryMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy sorted by creation date (newest first)
	result := make([]storage.MemoryMetadata, len(m.index.Memories))
	copy(result, m.index.Memories)

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

// Search finds memories matching query and/or categories
func (m *Manager) Search(query string, categories []storage.MemoryCategory) ([]storage.MemoryMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	query = strings.ToLower(query)
	var results []storage.MemoryMetadata

	for _, meta := range m.index.Memories {
		// Filter by category if specified
		if len(categories) > 0 {
			found := false
			for _, cat := range categories {
				if meta.Category == cat {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Match against preview and tags
		if query == "" {
			results = append(results, meta)
			continue
		}

		if strings.Contains(strings.ToLower(meta.Preview), query) {
			results = append(results, meta)
			continue
		}

		for _, tag := range meta.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				results = append(results, meta)
				break
			}
		}
	}

	// Sort by relevance (use count + recency)
	sort.Slice(results, func(i, j int) bool {
		scoreI := m.calculateScore(&results[i])
		scoreJ := m.calculateScore(&results[j])
		return scoreI > scoreJ
	})

	return results, nil
}

// calculateScore computes a relevance score for a memory
func (m *Manager) calculateScore(meta *storage.MemoryMetadata) float64 {
	// Recency: decay over 30 days
	daysSinceUsed := time.Since(meta.LastUsedAt).Hours() / 24
	recencyScore := 1.0 / (1.0 + daysSinceUsed/30.0)

	// Frequency: logarithmic scale
	frequencyScore := float64(meta.UseCount) / 10.0
	if frequencyScore > 1.0 {
		frequencyScore = 1.0
	}

	// Combined score (weighted: 60% recency, 40% frequency)
	return 0.6*recencyScore + 0.4*frequencyScore
}

// GetRelevantMemories returns memories for context injection within token budget
func (m *Manager) GetRelevantMemories(contextHint string, maxTokens int) []storage.Memory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if maxTokens <= 0 {
		maxTokens = DefaultMaxContextTokens
	}

	// Get all memories with scores
	type scoredMemory struct {
		meta  storage.MemoryMetadata
		score float64
	}

	scored := make([]scoredMemory, 0, len(m.index.Memories))
	for _, meta := range m.index.Memories {
		score := m.calculateScore(&meta)

		// Boost score if contextHint matches content
		if contextHint != "" && strings.Contains(strings.ToLower(meta.Preview), strings.ToLower(contextHint)) {
			score *= 1.5
		}

		scored = append(scored, scoredMemory{meta: meta, score: score})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Load memories until token budget exhausted
	var memories []storage.Memory
	tokenCount := 0

	for _, sm := range scored {
		// Estimate tokens: ~4 chars per token + overhead
		estimatedTokens := (len(sm.meta.Preview) + 50) / 4

		if tokenCount+estimatedTokens > maxTokens {
			break
		}

		// Load full memory
		mem, err := m.loadMemory(sm.meta.ID)
		if err != nil {
			continue
		}

		memories = append(memories, *mem)
		tokenCount += estimatedTokens
	}

	return memories
}

// loadMemory reads a memory file (must hold at least read lock)
func (m *Manager) loadMemory(id string) (*storage.Memory, error) {
	memPath := m.getMemoryPath(id)
	data, err := os.ReadFile(memPath)
	if err != nil {
		return nil, err
	}

	var mem storage.Memory
	if err := json.Unmarshal(data, &mem); err != nil {
		return nil, err
	}

	return &mem, nil
}

// IncrementUseCount updates the use count and last used time for a memory
func (m *Manager) IncrementUseCount(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update memory file
	mem, err := m.loadMemory(id)
	if err != nil {
		return err
	}

	mem.UseCount++
	mem.LastUsedAt = time.Now()

	memPath := m.getMemoryPath(id)
	data, err := json.MarshalIndent(mem, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(memPath, data, 0644); err != nil {
		return err
	}

	// Update index
	for i := range m.index.Memories {
		if m.index.Memories[i].ID == id {
			m.index.Memories[i].UseCount++
			m.index.Memories[i].LastUsedAt = mem.LastUsedAt
			break
		}
	}
	m.index.LastUpdated = time.Now()

	return m.saveIndex()
}

// Cleanup removes memories older than specified days that haven't been used
func (m *Manager) Cleanup(olderThanDays int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if olderThanDays <= 0 {
		olderThanDays = DefaultRetentionDays
	}

	cutoff := time.Now().AddDate(0, 0, -olderThanDays)
	var toDelete []string

	for _, meta := range m.index.Memories {
		// Delete if not used since cutoff AND created before cutoff
		if meta.LastUsedAt.Before(cutoff) && meta.CreatedAt.Before(cutoff) {
			toDelete = append(toDelete, meta.ID)
		}
	}

	// Remove from index
	newMemories := make([]storage.MemoryMetadata, 0, len(m.index.Memories)-len(toDelete))
	deleteSet := make(map[string]bool)
	for _, id := range toDelete {
		deleteSet[id] = true
	}

	for _, meta := range m.index.Memories {
		if !deleteSet[meta.ID] {
			newMemories = append(newMemories, meta)
		}
	}

	m.index.Memories = newMemories
	m.index.TotalCount = len(newMemories)
	m.index.LastUpdated = time.Now()

	if err := m.saveIndex(); err != nil {
		return 0, err
	}

	// Delete memory files
	for _, id := range toDelete {
		memPath := m.getMemoryPath(id)
		os.Remove(memPath) // Ignore errors for individual files
	}

	return len(toDelete), nil
}

// Clear removes all memories
func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete all memory files
	for _, meta := range m.index.Memories {
		memPath := m.getMemoryPath(meta.ID)
		os.Remove(memPath)
	}

	// Reset index
	m.index.Memories = []storage.MemoryMetadata{}
	m.index.TotalCount = 0
	m.index.LastUpdated = time.Now()

	return m.saveIndex()
}

// ExportJSON exports all memories to JSON format
func (m *Manager) ExportJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var memories []storage.Memory
	for _, meta := range m.index.Memories {
		mem, err := m.loadMemory(meta.ID)
		if err != nil {
			continue
		}
		memories = append(memories, *mem)
	}

	export := storage.MemoryExport{
		Version:    "1.0",
		ExportedAt: time.Now(),
		Memories:   memories,
	}

	return json.MarshalIndent(export, "", "  ")
}

// ImportJSON imports memories from JSON data
func (m *Manager) ImportJSON(data []byte) (int, error) {
	var export storage.MemoryExport
	if err := json.Unmarshal(data, &export); err != nil {
		return 0, fmt.Errorf("failed to parse import data: %w", err)
	}

	imported := 0
	for _, mem := range export.Memories {
		// Create with import source, preserving original metadata
		_, err := m.Create(mem.Category, mem.Content, mem.Context, mem.Tags, storage.MemorySourceImport)
		if err != nil {
			continue
		}
		imported++
	}

	return imported, nil
}

// GetStats returns statistics about the memory store
func (m *Manager) GetStats() *storage.MemoryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &storage.MemoryStats{
		TotalMemories: len(m.index.Memories),
		ByCategory:    make(map[string]int),
		BySource:      make(map[string]int),
	}

	var mostUsedMeta *storage.MemoryMetadata
	for i := range m.index.Memories {
		meta := &m.index.Memories[i]

		// Count by category
		stats.ByCategory[string(meta.Category)]++

		// Track oldest/newest
		if stats.OldestMemory == nil || meta.CreatedAt.Before(*stats.OldestMemory) {
			t := meta.CreatedAt
			stats.OldestMemory = &t
		}
		if stats.NewestMemory == nil || meta.CreatedAt.After(*stats.NewestMemory) {
			t := meta.CreatedAt
			stats.NewestMemory = &t
		}

		// Track most used
		stats.TotalUseCount += meta.UseCount
		if meta.UseCount == 0 {
			stats.UnusedCount++
		}
		if mostUsedMeta == nil || meta.UseCount > mostUsedMeta.UseCount {
			mostUsedMeta = meta
		}

		// Estimate tokens
		stats.EstimatedTokens += (len(meta.Preview) + 50) / 4
	}

	if mostUsedMeta != nil && mostUsedMeta.UseCount > 0 {
		stats.MostUsedID = mostUsedMeta.ID
		stats.MostUsedContent = mostUsedMeta.Preview
		stats.MostUsedCount = mostUsedMeta.UseCount
	}

	// Count by source (need to load memories for this)
	for _, meta := range m.index.Memories {
		mem, err := m.loadMemory(meta.ID)
		if err != nil {
			continue
		}
		stats.BySource[string(mem.Source)]++
	}

	return stats
}

// Count returns the total number of memories
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.index.TotalCount
}
