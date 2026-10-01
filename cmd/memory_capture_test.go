package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/storage"
)

// TestCheckAutoCaptureOffersAMemory covers which messages get a "Remember: ...?" offer, with which
// text and category, and that only a "y" saves it (as an auto memory).
func TestCheckAutoCaptureOffersAMemory(t *testing.T) {
	long := "We always use " + strings.Repeat("x", 250)
	tests := []struct {
		name     string
		message  string
		answer   string
		offer    string // "" = no offer at all
		category storage.MemoryCategory
	}{
		{"short", "use tabs please", "y\n", "", ""},
		{"question", "Should we always use make for builds?", "y\n", "", ""},
		{"nothing to remember", "Please list the pods in the default namespace", "y\n", "", ""},
		{"correction", "No, use tabs for indentation in this repo", "y\n",
			getCategoryIcon(storage.MemoryCategoryPattern) + ` Remember: "use tabs for indentation in this repo"? [y/N]`, storage.MemoryCategoryPattern},
		{"convention", "We always use make targets for every build", "y\n",
			`Remember: "We always use make targets for every build"?`, storage.MemoryCategoryPattern},
		{"knowledge", "Note that the staging database is read only", "y\n",
			getCategoryIcon(storage.MemoryCategoryLearning) + ` Remember: "Note that the staging database is read only"?`, storage.MemoryCategoryLearning},
		{"decision", "We decided to deploy everything with Helm charts", "y\n",
			getCategoryIcon(storage.MemoryCategoryDecision) + ` Remember: "We decided to deploy everything with Helm charts"?`, storage.MemoryCategoryDecision},
		{"declined", "We decided to deploy everything with Helm charts", "n\n",
			`Remember: "We decided to deploy everything with Helm charts"?`, ""},
		{"long", long, "y\n", `Remember: "` + long[:197] + `..."?`, storage.MemoryCategoryPattern},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := newMemoryManager(t)
			var out string
			withStdin(t, tt.answer, func() {
				out = captureStdoutForTest(t, func() { checkAutoCapture(mm, tt.message) })
			})
			if tt.offer == "" {
				if out != "" || mm.Count() != 0 {
					t.Fatalf("no offer expected: count %d, output %q", mm.Count(), out)
				}
				return
			}
			if !strings.Contains(out, tt.offer) {
				t.Fatalf("output lacks %q: %q", tt.offer, out)
			}
			if tt.category == "" {
				if mm.Count() != 0 || strings.Contains(out, "Saved") {
					t.Fatalf("a declined offer saved something: count %d, output %q", mm.Count(), out)
				}
				return
			}
			list := mm.List()
			if len(list) != 1 || list[0].Category != tt.category || !strings.Contains(out, "Saved ["+string(tt.category)+"]") {
				t.Fatalf("memories %+v, output %q", list, out)
			}
			if mem, err := mm.Get(list[0].ID); err != nil || mem.Source != storage.MemorySourceAuto {
				t.Fatalf("memory %+v err=%v", mem, err)
			}
		})
	}
}

func TestCheckAutoCaptureReportsASaveError(t *testing.T) {
	skipIfRoot(t)
	dir := filepath.Join(t.TempDir(), ".taracode")
	mm := managerAt(t, dir)
	if err := os.Chmod(memoryDir(dir), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(memoryDir(dir), 0o755) })
	var out string
	withStdin(t, "y\n", func() {
		out = captureStdoutForTest(t, func() { checkAutoCapture(mm, "We decided to deploy everything with Helm charts") })
	})
	if !strings.Contains(out, "Error saving:") || mm.Count() != 0 {
		t.Fatalf("count %d, output %q", mm.Count(), out)
	}
}

func TestExtractMemorySuggestion(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"No, use tabs", "use tabs"},
		{"no use tabs", "use tabs"},
		{"That's wrong, the port is 8080", "the port is 8080"},
		{"Actually, it runs on ARM", "it runs on ARM"},
		{"actually it runs on ARM", "it runs on ARM"},
		{"I meant the staging cluster", "the staging cluster"},
		{"Not like that, use helm", "use helm"},
		{"Use helm, not kubectl", "Use helm, not kubectl"},
		{"- use helm", "use helm"},
	}
	for _, tt := range tests {
		if got := extractMemorySuggestion(tt.message); got != tt.want {
			t.Errorf("extractMemorySuggestion(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}
