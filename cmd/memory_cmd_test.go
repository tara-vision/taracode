package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// memoryDir is where newMemoryManager-style managers keep their files, for a manager built on
// taracodeDir.
func memoryDir(taracodeDir string) string { return filepath.Join(taracodeDir, "memory") }

// managerAt builds a memory manager on taracodeDir.
func managerAt(t *testing.T, taracodeDir string) *memory.Manager {
	t.Helper()
	mm, err := memory.NewManager(taracodeDir)
	if err != nil {
		t.Fatal(err)
	}
	return mm
}

func TestMemoryCommandsNeedAnInitialisedProject(t *testing.T) {
	out := captureStdoutForTest(t, func() {
		handleRemember(nil, []string{"anything"}, nil)
		handleMemory(nil, nil)
	})
	if strings.Count(out, "Memory not available.") != 2 || !strings.Contains(out, "/init") {
		t.Fatalf("%q", out)
	}
}

func TestRememberWithoutTextPrintsTheUsage(t *testing.T) {
	mm := newMemoryManager(t)
	out := captureStdoutForTest(t, func() { handleRemember(mm, nil, nil) })
	if !strings.Contains(out, "Usage: /remember <text to remember>") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

// TestRememberSavesTheTextWithItsTags: #words become tags and leave the text, the category comes
// from the keywords, and the memory is a manual one.
func TestRememberSavesTheTextWithItsTags(t *testing.T) {
	mm := newMemoryManager(t)
	out := captureStdoutForTest(t, func() {
		handleRemember(mm, strings.Fields("Always run the tests before a push #ci #go"), nil)
	})
	list := mm.List()
	if len(list) != 1 {
		t.Fatalf("memories %+v", list)
	}
	saved, err := mm.Get(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Content != "Always run the tests before a push" || strings.Join(saved.Tags, ",") != "ci,go" ||
		saved.Category != storage.MemoryCategoryPattern || saved.Source != storage.MemorySourceManual {
		t.Fatalf("saved %+v", saved)
	}
	for _, want := range []string{"Saved [pattern] (ID: " + saved.ID + ")", "Tags: ci, go"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %q", want, out)
		}
	}
}

// TestRememberRefreshesTheSystemPrompt: the next request after /remember already carries the memory.
func TestRememberRefreshesTheSystemPrompt(t *testing.T) {
	r, srv := projectREPL(t, ollamatest.Turn{Content: "noted"})
	_ = captureStdoutForTest(t, func() { r.dispatch("/remember Database is PostgreSQL on port 5432") })
	if out := captureStdoutForTest(t, func() { r.dispatch("/memory") }); !strings.Contains(out, "Database is PostgreSQL") {
		t.Fatalf("/memory does not list the project's memory: %q", out)
	}
	_ = captureStdoutForTest(t, func() {
		if err := r.asst.ProcessMessage("hello"); err != nil {
			t.Error(err)
		}
	})
	var system string
	for _, req := range srv.Requests {
		if req.Path != "/api/chat" {
			continue
		}
		messages, _ := req.Body["messages"].([]any)
		if len(messages) > 0 {
			first, _ := messages[0].(map[string]any)
			system, _ = first["content"].(string)
		}
	}
	if !strings.Contains(system, "- [learning] Database is PostgreSQL on port 5432") {
		t.Fatalf("the request's system prompt lacks the memory:\n%s", system)
	}
}

func TestRememberReportsASaveError(t *testing.T) {
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	if err := os.Chmod(memoryDir(dir), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(memoryDir(dir), 0o755) })
	out := captureStdoutForTest(t, func() { handleRemember(mm, []string{"a", "fact"}, nil) })
	if !strings.Contains(out, "Error saving memory:") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

func TestDetectMemoryCategory(t *testing.T) {
	tests := []struct {
		content string
		want    storage.MemoryCategory
	}{
		{"We decided on Postgres", storage.MemoryCategoryDecision},
		{"The architecture is event driven", storage.MemoryCategoryDecision},
		{"Prefer table tests", storage.MemoryCategoryPattern},
		{"Variables are snake_case", storage.MemoryCategoryPattern},
		{"The deploy fails without a token", storage.MemoryCategoryError},
		{"Restart the pod as a solution", storage.MemoryCategoryError},
		{"The cluster runs in eu-west-1", storage.MemoryCategoryLearning},
	}
	for _, tt := range tests {
		if got := detectMemoryCategory(tt.content); got != tt.want {
			t.Errorf("detectMemoryCategory(%q) = %s, want %s", tt.content, got, tt.want)
		}
	}
}

func TestGetCategoryIcon(t *testing.T) {
	tests := []struct {
		category storage.MemoryCategory
		want     string
	}{
		{storage.MemoryCategoryDecision, "\U0001F3DB\uFE0F"}, // classical building
		{storage.MemoryCategoryPattern, "\U0001F4D0"},        // triangular ruler
		{storage.MemoryCategoryError, "\U0001F41B"},          // bug
		{storage.MemoryCategoryLearning, ui.IconTip},
		{storage.MemoryCategory("other"), ui.IconSession},
	}
	for _, tt := range tests {
		if got := getCategoryIcon(tt.category); got != tt.want {
			t.Errorf("getCategoryIcon(%s) = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestMemoryListsEveryMemory(t *testing.T) {
	mm := newMemoryManager(t)
	out := captureStdoutForTest(t, func() { handleMemory(mm, nil) })
	if !strings.Contains(out, "No memories saved for this project.") {
		t.Fatalf("%q", out)
	}

	used, err := mm.Create(storage.MemoryCategoryError, "The deploy fails without a token", "", nil,
		storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := mm.IncrementUseCount(used.ID); err != nil {
		t.Fatal(err)
	}
	unused, err := mm.Create(storage.MemoryCategoryLearning, "The cluster runs in eu-west-1", "", nil,
		storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	out = captureStdoutForTest(t, func() { handleMemory(mm, nil) })
	for _, want := range []string{
		"Project Memories (2 total):",
		"[" + used.ID + "] error (used 1x)", "The deploy fails without a token",
		"[" + unused.ID + "] learning\n", "The cluster runs in eu-west-1", "just now",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing lacks %q:\n%s", want, out)
		}
	}
}

func TestMemorySearch(t *testing.T) {
	mm := newMemoryManager(t)
	if _, err := mm.Create(storage.MemoryCategoryLearning, "Staging uses the blue cluster", "", []string{"infra"},
		storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"search"}, "Usage: /memory search <query>"},
		{[]string{"search", "purple"}, "No memories found matching: purple"},
		{[]string{"search", "blue", "cluster"}, "Search Results for \"blue cluster\" (1 matches):"},
		{[]string{"search", "infra"}, "Staging uses the blue cluster"},
	}
	for _, tt := range tests {
		out := captureStdoutForTest(t, func() { handleMemory(mm, tt.args) })
		if !strings.Contains(out, tt.want) {
			t.Errorf("/memory %s: output lacks %q: %q", strings.Join(tt.args, " "), tt.want, out)
		}
	}
}

func TestMemoryDeleteAsksFirst(t *testing.T) {
	mm := newMemoryManager(t)
	mem, err := mm.Create(storage.MemoryCategoryLearning, "Keep me around", "", nil, storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdoutForTest(t, func() { handleMemory(mm, []string{"delete"}) })
	if !strings.Contains(out, "Usage: /memory delete <id>") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"delete", "zzzz"}) })
	if !strings.Contains(out, "Memory not found: zzzz") {
		t.Fatalf("%q", out)
	}

	withStdin(t, "n\n", func() {
		out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"delete", mem.ID[:4]}) })
	})
	if !strings.Contains(out, "Delete memory ["+mem.ID+"]?") || !strings.Contains(out, "Keep me around") ||
		!strings.Contains(out, "Cancelled.") || mm.Count() != 1 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}

	withStdin(t, "Y\n", func() {
		out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"delete", mem.ID}) })
	})
	if !strings.Contains(out, "Memory deleted.") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

func TestMemoryDeleteReportsAWriteError(t *testing.T) {
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	mem, err := mm.Create(storage.MemoryCategoryLearning, "Stuck in place", "", nil, storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	readOnlyIndex(t, dir)
	var out string
	withStdin(t, "y\n", func() {
		out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"delete", mem.ID}) })
	})
	if !strings.Contains(out, "Error deleting:") {
		t.Fatalf("%q", out)
	}
}

// readOnlyIndex makes the memory index unwritable, so every change the manager saves fails.
func readOnlyIndex(t *testing.T, taracodeDir string) {
	t.Helper()
	index := filepath.Join(memoryDir(taracodeDir), "index.json")
	if err := os.Chmod(index, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(index, 0o644) })
}

func TestMemoryExportWritesTheMemories(t *testing.T) {
	mm := newMemoryManager(t)
	if _, err := mm.Create(storage.MemoryCategoryDecision, "We chose Helm", "", []string{"deploy"},
		storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(t.TempDir(), "mine.json")
	out := captureStdoutForTest(t, func() { handleMemory(mm, []string{"export", named}) })
	if !strings.Contains(out, "Exported 1 memories to: "+named) {
		t.Fatalf("%q", out)
	}
	var export storage.MemoryExport
	data, err := os.ReadFile(named)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &export); err != nil || len(export.Memories) != 1 ||
		export.Memories[0].Content != "We chose Helm" {
		t.Fatalf("export %+v err=%v", export, err)
	}

	// Without a name the file lands in the current directory, named after the time.
	t.Chdir(t.TempDir())
	out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"export"}) })
	matches, err := filepath.Glob("memories-*.json")
	if err != nil || len(matches) != 1 || !strings.Contains(out, "to: "+matches[0]) {
		t.Fatalf("matches %v err=%v output %q", matches, err, out)
	}

	out = captureStdoutForTest(t, func() {
		handleMemory(mm, []string{"export", filepath.Join(t.TempDir(), "missing", "out.json")})
	})
	if !strings.Contains(out, "Error writing file:") {
		t.Fatalf("%q", out)
	}
}

func TestMemoryImportReadsAnExport(t *testing.T) {
	source := newMemoryManager(t)
	for _, content := range []string{"First fact", "Second fact"} {
		if _, err := source.Create(storage.MemoryCategoryLearning, content, "", nil, storage.MemorySourceManual); err != nil {
			t.Fatal(err)
		}
	}
	data, err := source.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
	if err := os.WriteFile(good, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	mm := newMemoryManager(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"import"}, "Usage: /memory import <file>"},
		{[]string{"import", filepath.Join(dir, "absent.json")}, "Error reading file:"},
		{[]string{"import", bad}, "Error importing:"},
		{[]string{"import", good}, "Imported 2 memories from: " + good},
	}
	for _, tt := range tests {
		out := captureStdoutForTest(t, func() { handleMemory(mm, tt.args) })
		if !strings.Contains(out, tt.want) {
			t.Errorf("/memory %s: output lacks %q: %q", strings.Join(tt.args, " "), tt.want, out)
		}
	}
	if mm.Count() != 2 {
		t.Fatalf("count %d after the import", mm.Count())
	}
	for _, meta := range mm.List() {
		if mem, err := mm.Get(meta.ID); err != nil || mem.Source != storage.MemorySourceImport {
			t.Fatalf("imported memory %+v err=%v", mem, err)
		}
	}
}

func TestMemoryStatsSummarisesTheStore(t *testing.T) {
	mm := newMemoryManager(t)
	long := strings.Repeat("abcdefghij", 6)
	used, err := mm.Create(storage.MemoryCategoryPattern, long, "", nil, storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := mm.IncrementUseCount(used.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := mm.Create(storage.MemoryCategoryError, "Retry the flaky job", "", nil, storage.MemorySourceAuto); err != nil {
		t.Fatal(err)
	}
	out := captureStdoutForTest(t, func() { handleMemory(mm, []string{"stats"}) })
	for _, want := range []string{
		"Total memories:     2", "Total context uses: 1", "Unused memories:    1",
		"By Category:", "pattern", "error", "By Source:", "manual", "auto",
		"Most Used: [" + used.ID + "] (1x)", "  " + long[:50] + "...",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stats lack %q:\n%s", want, out)
		}
	}

	short := newMemoryManager(t)
	mem, err := short.Create(storage.MemoryCategoryLearning, "Short fact", "", nil, storage.MemorySourceManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := short.IncrementUseCount(mem.ID); err != nil {
		t.Fatal(err)
	}
	out = captureStdoutForTest(t, func() { showMemoryStats(short) })
	if !strings.Contains(out, "  Short fact\n") || strings.Contains(out, "Short fact...") {
		t.Fatalf("%q", out)
	}
}

// ageMemories rewrites the index so every memory was created and last used days ago.
func ageMemories(t *testing.T, taracodeDir string, days int) {
	t.Helper()
	path := filepath.Join(memoryDir(taracodeDir), "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var index storage.MemoryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -days)
	for i := range index.Memories {
		index.Memories[i].CreatedAt, index.Memories[i].LastUsedAt = old, old
	}
	data, err = json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryCleanupRemovesOldUnusedMemories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	out := captureStdoutForTest(t, func() { handleMemory(mm, []string{"cleanup"}) })
	if !strings.Contains(out, "No memories older than 90 days to clean up.") {
		t.Fatalf("%q", out)
	}
	if _, err := mm.Create(storage.MemoryCategoryLearning, "An old fact", "", nil, storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	ageMemories(t, dir, 45)
	mm = managerAt(t, dir)
	out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"cleanup", "nonsense"}) })
	if !strings.Contains(out, "No memories older than 90 days") || mm.Count() != 1 {
		t.Fatalf("a bad day count keeps the 90-day default: count %d, output %q", mm.Count(), out)
	}
	out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"cleanup", "30"}) })
	if !strings.Contains(out, "Removed 1 memories not used in 30 days.") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

func TestMemoryCleanupReportsAWriteError(t *testing.T) {
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	if _, err := mm.Create(storage.MemoryCategoryLearning, "A fact", "", nil, storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	readOnlyIndex(t, dir)
	out := captureStdoutForTest(t, func() { cleanupMemories(mm, 30) })
	if !strings.Contains(out, "Error during cleanup:") {
		t.Fatalf("%q", out)
	}
}

func TestMemoryClearNeedsAYes(t *testing.T) {
	mm := newMemoryManager(t)
	out := captureStdoutForTest(t, func() { handleMemory(mm, []string{"clear"}) })
	if !strings.Contains(out, "No memories to clear.") {
		t.Fatalf("%q", out)
	}
	for _, content := range []string{"One", "Two"} {
		if _, err := mm.Create(storage.MemoryCategoryLearning, content, "", nil, storage.MemorySourceManual); err != nil {
			t.Fatal(err)
		}
	}
	withStdin(t, "y\n", func() {
		out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"clear"}) })
	})
	if !strings.Contains(out, "Clear all 2 memories? This cannot be undone.") || !strings.Contains(out, "Cancelled.") ||
		mm.Count() != 2 {
		t.Fatalf("y is not yes: count %d, output %q", mm.Count(), out)
	}
	withStdin(t, "YES\n", func() {
		out = captureStdoutForTest(t, func() { handleMemory(mm, []string{"clear"}) })
	})
	if !strings.Contains(out, "Cleared 2 memories.") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

func TestMemoryClearReportsAWriteError(t *testing.T) {
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	if _, err := mm.Create(storage.MemoryCategoryLearning, "A fact", "", nil, storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	readOnlyIndex(t, dir)
	var out string
	withStdin(t, "yes\n", func() {
		out = captureStdoutForTest(t, func() { clearMemories(mm) })
	})
	if !strings.Contains(out, "Error clearing memories:") {
		t.Fatalf("%q", out)
	}
}

func TestMemoryUnknownSubcommand(t *testing.T) {
	out := captureStdoutForTest(t, func() { handleMemory(newMemoryManager(t), []string{"frobnicate"}) })
	if !strings.Contains(out, "Unknown subcommand: frobnicate") || !strings.Contains(out, "Usage: /memory [search|") {
		t.Fatalf("%q", out)
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5*time.Minute + 30*time.Second, "5 minutes ago"},
		{70 * time.Minute, "1 hour ago"},
		{5*time.Hour + 30*time.Minute, "5 hours ago"},
		{30 * time.Hour, "yesterday"},
		{5*24*time.Hour + time.Hour, "5 days ago"},
		{40 * 24 * time.Hour, "1 month ago"},
		{100 * 24 * time.Hour, "3 months ago"},
	}
	for _, tt := range tests {
		if got := formatAge(time.Now().Add(-tt.ago)); got != tt.want {
			t.Errorf("formatAge(-%s) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}
