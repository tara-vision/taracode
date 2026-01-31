package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/storage"
)

func setupTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "memory-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create .taracode dir: %v", err)
	}

	mgr, err := NewManager(taracodeDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create manager: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return mgr, cleanup
}

func TestNewManager(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	if mgr == nil {
		t.Fatal("expected manager to be non-nil")
	}

	// Check memory directory was created
	memoryDir := filepath.Join(mgr.rootDir, "memory")
	if _, err := os.Stat(memoryDir); os.IsNotExist(err) {
		t.Error("memory directory was not created")
	}
}

func TestCreate(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	mem, err := mgr.Create(
		storage.MemoryCategoryDecision,
		"Use PostgreSQL for the database",
		"Discussed in architecture meeting",
		[]string{"database", "architecture"},
		storage.MemorySourceManual,
	)

	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mem.ID == "" {
		t.Error("expected ID to be set")
	}
	if mem.Category != storage.MemoryCategoryDecision {
		t.Errorf("expected category %s, got %s", storage.MemoryCategoryDecision, mem.Category)
	}
	if mem.Content != "Use PostgreSQL for the database" {
		t.Errorf("unexpected content: %s", mem.Content)
	}
	if len(mem.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(mem.Tags))
	}

	// Verify index was updated
	if mgr.Count() != 1 {
		t.Errorf("expected count 1, got %d", mgr.Count())
	}
}

func TestGet(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	created, err := mgr.Create(
		storage.MemoryCategoryPattern,
		"Use camelCase for variables",
		"",
		nil,
		storage.MemorySourceManual,
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get by full ID
	got, err := mgr.Get(created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Content != created.Content {
		t.Errorf("content mismatch: %s != %s", got.Content, created.Content)
	}

	// Get by partial ID (first 4 chars)
	partial := created.ID[:4]
	got2, err := mgr.Get(partial)
	if err != nil {
		t.Fatalf("Get by partial ID failed: %v", err)
	}
	if got2.ID != created.ID {
		t.Errorf("ID mismatch: %s != %s", got2.ID, created.ID)
	}
}

func TestDelete(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	mem, err := mgr.Create(
		storage.MemoryCategoryError,
		"Connection timeout means check firewall",
		"",
		nil,
		storage.MemorySourceManual,
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mgr.Count() != 1 {
		t.Errorf("expected count 1, got %d", mgr.Count())
	}

	// Delete by partial ID
	err = mgr.Delete(mem.ID[:4])
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if mgr.Count() != 0 {
		t.Errorf("expected count 0, got %d", mgr.Count())
	}

	// Get should fail
	_, err = mgr.Get(mem.ID)
	if err == nil {
		t.Error("expected Get to fail after delete")
	}
}

func TestList(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create multiple memories
	for i := 0; i < 3; i++ {
		_, err := mgr.Create(
			storage.MemoryCategoryLearning,
			"Memory content "+string(rune('A'+i)),
			"",
			nil,
			storage.MemorySourceManual,
		)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	list := mgr.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(list))
	}

	// Should be sorted newest first
	if list[0].CreatedAt.Before(list[1].CreatedAt) {
		t.Error("expected newest first ordering")
	}
}

func TestSearch(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create test memories
	mgr.Create(storage.MemoryCategoryPattern, "Use snake_case for filenames", "", []string{"naming"}, storage.MemorySourceManual)
	mgr.Create(storage.MemoryCategoryPattern, "Use camelCase for variables", "", []string{"naming"}, storage.MemorySourceManual)
	mgr.Create(storage.MemoryCategoryDecision, "Use PostgreSQL database", "", []string{"database"}, storage.MemorySourceManual)

	// Search by content
	results, err := mgr.Search("camel", nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'camel', got %d", len(results))
	}

	// Search by tag
	results, err = mgr.Search("naming", nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'naming' tag, got %d", len(results))
	}

	// Search with category filter
	results, err = mgr.Search("", []storage.MemoryCategory{storage.MemoryCategoryDecision})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 decision, got %d", len(results))
	}
}

func TestGetRelevantMemories(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create some memories
	for i := 0; i < 10; i++ {
		mgr.Create(
			storage.MemoryCategoryLearning,
			"Memory content that takes up some tokens "+string(rune('A'+i)),
			"",
			nil,
			storage.MemorySourceManual,
		)
	}

	// Get relevant memories with small token budget
	memories := mgr.GetRelevantMemories("", 100)
	if len(memories) == 0 {
		t.Error("expected some memories to be returned")
	}
	if len(memories) > 5 {
		t.Error("expected token budget to limit results")
	}
}

func TestIncrementUseCount(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	mem, err := mgr.Create(
		storage.MemoryCategoryPattern,
		"Test memory",
		"",
		nil,
		storage.MemorySourceManual,
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if mem.UseCount != 0 {
		t.Errorf("expected initial use count 0, got %d", mem.UseCount)
	}

	// Increment multiple times
	for i := 0; i < 3; i++ {
		if err := mgr.IncrementUseCount(mem.ID); err != nil {
			t.Fatalf("IncrementUseCount failed: %v", err)
		}
	}

	// Verify count was updated
	updated, err := mgr.Get(mem.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if updated.UseCount != 3 {
		t.Errorf("expected use count 3, got %d", updated.UseCount)
	}
}

func TestCleanup(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create a memory with old timestamps (manually adjust file)
	mem, err := mgr.Create(
		storage.MemoryCategoryLearning,
		"Old memory",
		"",
		nil,
		storage.MemorySourceManual,
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Manually update timestamps to be old
	mgr.mu.Lock()
	oldTime := time.Now().AddDate(0, 0, -100) // 100 days ago
	for i := range mgr.index.Memories {
		if mgr.index.Memories[i].ID == mem.ID {
			mgr.index.Memories[i].CreatedAt = oldTime
			mgr.index.Memories[i].LastUsedAt = oldTime
			break
		}
	}
	mgr.saveIndex()
	mgr.mu.Unlock()

	// Create a recent memory
	mgr.Create(
		storage.MemoryCategoryLearning,
		"Recent memory",
		"",
		nil,
		storage.MemorySourceManual,
	)

	if mgr.Count() != 2 {
		t.Fatalf("expected 2 memories, got %d", mgr.Count())
	}

	// Cleanup with 30 day threshold
	deleted, err := mgr.Cleanup(30)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}
	if mgr.Count() != 1 {
		t.Errorf("expected 1 remaining, got %d", mgr.Count())
	}
}

func TestClear(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create multiple memories
	for i := 0; i < 5; i++ {
		mgr.Create(
			storage.MemoryCategoryLearning,
			"Memory "+string(rune('A'+i)),
			"",
			nil,
			storage.MemorySourceManual,
		)
	}

	if mgr.Count() != 5 {
		t.Fatalf("expected 5 memories, got %d", mgr.Count())
	}

	if err := mgr.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if mgr.Count() != 0 {
		t.Errorf("expected 0 memories after clear, got %d", mgr.Count())
	}
}

func TestExportImportJSON(t *testing.T) {
	mgr1, cleanup1 := setupTestManager(t)
	defer cleanup1()

	// Create memories in first manager
	mgr1.Create(storage.MemoryCategoryDecision, "Decision 1", "ctx1", []string{"tag1"}, storage.MemorySourceManual)
	mgr1.Create(storage.MemoryCategoryPattern, "Pattern 1", "ctx2", []string{"tag2"}, storage.MemorySourceManual)

	// Export
	data, err := mgr1.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON failed: %v", err)
	}

	// Create second manager and import
	mgr2, cleanup2 := setupTestManager(t)
	defer cleanup2()

	imported, err := mgr2.ImportJSON(data)
	if err != nil {
		t.Fatalf("ImportJSON failed: %v", err)
	}
	if imported != 2 {
		t.Errorf("expected 2 imported, got %d", imported)
	}
	if mgr2.Count() != 2 {
		t.Errorf("expected 2 memories, got %d", mgr2.Count())
	}
}

func TestGetStats(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	// Create memories of different categories
	mgr.Create(storage.MemoryCategoryDecision, "Decision 1", "", nil, storage.MemorySourceManual)
	mgr.Create(storage.MemoryCategoryPattern, "Pattern 1", "", nil, storage.MemorySourceAuto)
	mem, _ := mgr.Create(storage.MemoryCategoryPattern, "Pattern 2", "", nil, storage.MemorySourceManual)

	// Increment use count on one
	mgr.IncrementUseCount(mem.ID)
	mgr.IncrementUseCount(mem.ID)

	stats := mgr.GetStats()
	if stats.TotalMemories != 3 {
		t.Errorf("expected 3 total, got %d", stats.TotalMemories)
	}
	if stats.ByCategory["decision"] != 1 {
		t.Errorf("expected 1 decision, got %d", stats.ByCategory["decision"])
	}
	if stats.ByCategory["pattern"] != 2 {
		t.Errorf("expected 2 patterns, got %d", stats.ByCategory["pattern"])
	}
	if stats.MostUsedCount != 2 {
		t.Errorf("expected most used count 2, got %d", stats.MostUsedCount)
	}
	if stats.UnusedCount != 2 {
		t.Errorf("expected 2 unused, got %d", stats.UnusedCount)
	}
}

func TestPreviewTruncation(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	longContent := "This is a very long memory content that exceeds the maximum preview length and should be truncated to fit within the preview field properly"

	mem, err := mgr.Create(
		storage.MemoryCategoryLearning,
		longContent,
		"",
		nil,
		storage.MemorySourceManual,
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(list))
	}

	preview := list[0].Preview
	if len(preview) > MaxPreviewLength {
		t.Errorf("preview too long: %d > %d", len(preview), MaxPreviewLength)
	}
	if !contains(preview, "...") && len(longContent) > MaxPreviewLength {
		t.Error("expected truncated preview to end with ...")
	}

	// Full content should still be preserved
	full, _ := mgr.Get(mem.ID)
	if full.Content != longContent {
		t.Error("full content was not preserved")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr
}
