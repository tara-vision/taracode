package agent

import (
	"strings"
	"testing"
)

// TestCleanResponseStripsThinkBlocks covers both the closed <think>...</think> case and an
// unclosed tag, which cleanResponse also strips.
func TestCleanResponseStripsThinkBlocks(t *testing.T) {
	closed := cleanResponse("<think>reasoning here</think>The answer is 42.")
	if closed != "The answer is 42." {
		t.Fatalf("closed tag: cleanResponse = %q", closed)
	}

	unclosed := cleanResponse("leftover reasoning</think>The answer is 43.")
	if unclosed != "The answer is 43." {
		t.Fatalf("unclosed tag: cleanResponse = %q", unclosed)
	}
}

// TestStreamFilterHidesThinkTagsAcrossChunks feeds the opening tag split mid-token, across two
// Process calls, to prove the filter buffers a partial tag instead of leaking it to the display.
func TestStreamFilterHidesThinkTagsAcrossChunks(t *testing.T) {
	f := NewStreamFilter()

	var visible strings.Builder
	visible.WriteString(f.Process("<thi"))
	visible.WriteString(f.Process("nk>hidden</think>shown"))
	visible.WriteString(f.Flush())

	if visible.String() != "shown" {
		t.Fatalf("visible output = %q, want %q", visible.String(), "shown")
	}
	if !strings.Contains(f.FullContent(), "hidden") {
		t.Fatalf("FullContent must keep the reasoning for tool-call parsing: %q", f.FullContent())
	}
}
