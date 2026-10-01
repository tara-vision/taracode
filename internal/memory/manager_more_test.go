package memory

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/storage"
)

// seedStore writes memories and an index listing them under a fresh .taracode, and returns that
// directory. A memory whose Content is "" gets no file: the index names a memory that is gone.
func seedStore(t *testing.T, mems ...storage.Memory) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".taracode")
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := storage.MemoryIndex{TotalCount: len(mems), LastUpdated: time.Now()}
	for _, m := range mems {
		index.Memories = append(index.Memories, storage.MemoryMetadata{ID: m.ID, Category: m.Category, Preview: m.Content,
			CreatedAt: m.CreatedAt, LastUsedAt: m.LastUsedAt, UseCount: m.UseCount})
		if m.Content == "" {
			continue
		}
		writeJSON(t, filepath.Join(memDir, "memory_"+m.ID+".json"), m)
	}
	writeJSON(t, filepath.Join(memDir, "index.json"), index)
	return dir
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T, dir string) *Manager {
	t.Helper()
	m, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func memoryAt(id, content string, at time.Time) storage.Memory {
	return storage.Memory{ID: id, Category: storage.MemoryCategoryLearning, Content: content, Source: storage.MemorySourceManual,
		CreatedAt: at, LastUsedAt: at}
}

// occupy puts a non-empty directory where a file is expected, so writing or removing it fails.
func occupy(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestNewManagerReloadsTheIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".taracode")
	first := openStore(t, dir)
	for _, c := range []string{"use helm", "pin images"} {
		if _, err := first.Create(storage.MemoryCategoryDecision, c, "", nil, storage.MemorySourceManual); err != nil {
			t.Fatal(err)
		}
	}
	if again := openStore(t, dir); again.Count() != 2 || len(again.List()) != 2 {
		t.Fatalf("reloaded %d memories", again.Count())
	}
}

func TestNewManagerFailuresAndABrokenIndex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "taracode")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(file); err == nil || !strings.HasPrefix(err.Error(), "failed to create memory directory: ") {
		t.Fatalf("a file: %v", err)
	}
	dir := filepath.Join(t.TempDir(), ".taracode")
	occupy(t, filepath.Join(dir, "memory", "index.json"))
	if _, err := NewManager(dir); err == nil || !strings.HasPrefix(err.Error(), "failed to read index file: ") {
		t.Fatalf("an unreadable index: %v", err)
	}
	broken := filepath.Join(t.TempDir(), ".taracode")
	if err := os.MkdirAll(filepath.Join(broken, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "memory", "index.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := openStore(t, broken); m.Count() != 0 || len(m.List()) != 0 {
		t.Fatal("a broken index starts empty")
	}
}

// TestGetMatchesAnExactIDAmongPrefixes: an ID that is also the prefix of another ID still finds its
// own memory; a prefix of both finds neither.
func TestGetMatchesAnExactIDAmongPrefixes(t *testing.T) {
	now := time.Now()
	m := openStore(t, seedStore(t, memoryAt("abc", "short id", now), memoryAt("abcd", "long id", now)))
	if mem, err := m.Get("abc"); err != nil || mem.Content != "short id" {
		t.Fatalf("exact: %+v %v", mem, err)
	}
	if _, err := m.Get("ab"); err == nil || err.Error() != "memory not found: ab" {
		t.Fatalf("ambiguous: %v", err)
	}
}

func TestGetAndIncrementReportUnreadableMemories(t *testing.T) {
	now := time.Now()
	dir := seedStore(t, memoryAt("gone", "", now), memoryAt("bad", "broken soon", now))
	if err := os.WriteFile(filepath.Join(dir, "memory", "memory_bad.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := openStore(t, dir)
	if _, err := m.Get("gone"); err == nil || !strings.HasPrefix(err.Error(), "failed to read memory file: ") {
		t.Fatalf("Get(gone): %v", err)
	}
	if _, err := m.Get("bad"); err == nil || !strings.HasPrefix(err.Error(), "failed to unmarshal memory: ") {
		t.Fatalf("Get(bad): %v", err)
	}
	if err := m.IncrementUseCount("gone"); !os.IsNotExist(err) {
		t.Fatalf("IncrementUseCount(gone): %v", err)
	}
	if err := m.IncrementUseCount("bad"); err == nil {
		t.Fatal("IncrementUseCount(bad)")
	}
}

func TestIncrementUseCountReportsAFileItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	dir := seedStore(t, memoryAt("ro", "read only", time.Now()))
	if err := os.Chmod(filepath.Join(dir, "memory", "memory_ro.json"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := openStore(t, dir).IncrementUseCount("ro"); !os.IsPermission(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteKeepsTheOthersAndReportsFailures(t *testing.T) {
	now := time.Now()
	dir := seedStore(t, memoryAt("one", "first", now), memoryAt("two", "second", now), memoryAt("three", "third", now))
	m := openStore(t, dir)
	if err := m.Delete("zzz"); err == nil || err.Error() != "memory not found: zzz" {
		t.Fatalf("unknown: %v", err)
	}
	if err := m.Delete("one"); err != nil {
		t.Fatal(err)
	}
	if list := m.List(); len(list) != 2 || m.Count() != 2 {
		t.Fatalf("left %+v", list)
	}
	occupy(t, filepath.Join(dir, "memory", "memory_two.json"))
	if err := m.Delete("two"); err == nil || !strings.HasPrefix(err.Error(), "failed to delete memory file: ") {
		t.Fatalf("a file it cannot remove: %v", err)
	}
	occupy(t, filepath.Join(dir, "memory", "index.json"))
	if err := m.Delete("three"); err == nil {
		t.Fatal("an index it cannot write")
	}
}

func TestCreateReportsWhatItCannotWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".taracode")
	m := openStore(t, dir)
	occupy(t, filepath.Join(dir, "memory", "index.json"))
	if _, err := m.Create(storage.MemoryCategoryError, "x", "", nil, storage.MemorySourceAuto); err == nil {
		t.Fatal("an index it cannot write")
	}
	memDir := filepath.Join(dir, "memory")
	if err := os.RemoveAll(memDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(storage.MemoryCategoryError, "x", "", nil, storage.MemorySourceAuto); err == nil ||
		!strings.HasPrefix(err.Error(), "failed to write memory file: ") {
		t.Fatalf("a memory directory that is a file: %v", err)
	}
	if n, err := m.ImportJSON([]byte(`{"memories":[{"category":"error","content":"x"}]}`)); n != 0 || err != nil {
		t.Fatalf("an import whose memories cannot be written: %d %v", n, err)
	}
	if _, err := m.ImportJSON([]byte("{")); err == nil || !strings.HasPrefix(err.Error(), "failed to parse import data: ") {
		t.Fatalf("broken import: %v", err)
	}
}

// TestScoreCapsTheUseCount: past ten uses, more uses do not raise the score.
func TestScoreCapsTheUseCount(t *testing.T) {
	m := &Manager{}
	at := time.Now().Add(-48 * time.Hour)
	score := func(uses int) float64 {
		return m.calculateScore(&storage.MemoryMetadata{UseCount: uses, LastUsedAt: at})
	}
	if math.Abs(score(25)-score(10)) > 1e-6 || score(10)-score(5) < 0.19 {
		t.Fatalf("5: %v, 10: %v, 25: %v", score(5), score(10), score(25))
	}
}

func TestRelevantMemoriesFollowTheHint(t *testing.T) {
	now := time.Now()
	dir := seedStore(t, memoryAt("db", "the database is postgres", now), memoryAt("ci", "ci runs on github", now),
		memoryAt("gone", "", now))
	got := openStore(t, dir).GetRelevantMemories("GitHub", 0)
	if len(got) != 2 || got[0].ID != "ci" || got[1].ID != "db" {
		t.Fatalf("%+v", got)
	}
}

// TestCleanupDefaultsToTheRetentionPeriod: by default a memory goes once it is older than the
// retention period and unused for as long; its file goes with it.
func TestCleanupDefaultsToTheRetentionPeriod(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -(DefaultRetentionDays + 10))
	inUse := memoryAt("used", "old but in use", old)
	inUse.LastUsedAt = now.AddDate(0, 0, -1)
	dir := seedStore(t, memoryAt("old", "stale fact", old), inUse, memoryAt("month", "a month old", now.AddDate(0, 0, -30)),
		memoryAt("new", "fresh fact", now))
	m := openStore(t, dir)
	if n, err := m.Cleanup(0); n != 1 || err != nil {
		t.Fatalf("removed %d, %v", n, err)
	}
	left := map[string]bool{}
	for _, meta := range m.List() {
		left[meta.ID] = true
	}
	if len(left) != 3 || !left["used"] || !left["month"] || !left["new"] {
		t.Fatalf("left %v: a memory within the period or used within it stays", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "memory_old.json")); !os.IsNotExist(err) {
		t.Fatalf("the removed memory's file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "memory_used.json")); err != nil {
		t.Fatalf("a kept memory's file: %v", err)
	}
	occupy(t, filepath.Join(dir, "memory", "index.json"))
	if _, err := m.Cleanup(1); err == nil {
		t.Fatal("an index it cannot write")
	}
}

// TestExportAndStatsSkipMissingMemories: a memory the index names without a file is left out of the
// export and of the per-source counts.
func TestExportAndStatsSkipMissingMemories(t *testing.T) {
	now := time.Now()
	m := openStore(t, seedStore(t, memoryAt("kept", "kept fact", now), memoryAt("gone", "", now)))
	data, err := m.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	var export storage.MemoryExport
	if err := json.Unmarshal(data, &export); err != nil || len(export.Memories) != 1 || export.Memories[0].ID != "kept" {
		t.Fatalf("%+v %v", export, err)
	}
	if stats := m.GetStats(); stats.TotalMemories != 2 || stats.BySource["manual"] != 1 {
		t.Fatalf("%+v", stats)
	}
}
