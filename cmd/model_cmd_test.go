package cmd

import (
	"fmt"
	"testing"

	"github.com/tara-vision/taracode/internal/provider"
)

// TestBuildModelSelectorItems covers the /model picker lines: the current model carries the "*"
// marker, the parameter count and size appear when the host reports them, and a bare name stays
// bare.
func TestBuildModelSelectorItems(t *testing.T) {
	sized := provider.ModelInfo{Name: "glm-4.7-flash", Params: "30B", Size: 19 << 30}
	models := []provider.ModelInfo{sized, {Name: "gemma4:12b", Params: "12B"}, {Name: "qwen2.5:7b"}}

	got := buildModelSelectorItems(models, "gemma4:12b")

	want := []string{
		fmt.Sprintf("  glm-4.7-flash (30B, %s)", sized.FormatSize()),
		"* gemma4:12b (12B)",
		"  qwen2.5:7b",
	}
	if len(got) != len(want) {
		t.Fatalf("%d items, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, got[i], want[i])
		}
	}
}
