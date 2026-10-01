package ui

import (
	"strings"
	"testing"
)

// restoreMarkdown puts the package's default renderer back when the test ends.
func restoreMarkdown(t *testing.T) {
	t.Helper()
	t.Cleanup(EnableMarkdown)
}

func TestMarkdownCanBeSwitchedOff(t *testing.T) {
	restoreMarkdown(t)
	const doc = "# Title\n\n**bold** text"
	if !IsMarkdownEnabled() {
		t.Fatal("markdown is on by default")
	}
	// Off a terminal glamour keeps the markdown marks but lays the text out (margins, padding).
	rendered := RenderMarkdown(doc)
	if rendered == doc || !strings.Contains(rendered, "Title") || !strings.Contains(rendered, "bold** text") {
		t.Fatalf("rendered %q", rendered)
	}
	DisableMarkdown()
	if IsMarkdownEnabled() || RenderMarkdown(doc) != doc {
		t.Fatal("off, the content passes through unchanged")
	}
	EnableMarkdown()
	if !IsMarkdownEnabled() || RenderMarkdown(doc) == doc {
		t.Fatal("back on, the content is rendered again")
	}
}

func TestSetWordWrapRewrapsTheText(t *testing.T) {
	restoreMarkdown(t)
	paragraph := strings.TrimSpace(strings.Repeat("word ", 30))
	SetWordWrap(40)
	for _, line := range strings.Split(RenderMarkdown(paragraph), "\n") {
		if len(strings.TrimRight(line, " ")) > 40 {
			t.Fatalf("a line longer than 40 columns: %q", line)
		}
	}
	if !IsMarkdownEnabled() {
		t.Fatal("a new wrap width keeps markdown on")
	}
}

func TestRenderMarkdownToWriterPrintsTheRender(t *testing.T) {
	restoreMarkdown(t)
	DisableMarkdown()
	if out := captureStdout(t, func() { RenderMarkdownToWriter("plain text") }); out != "plain text\n" {
		t.Fatalf("%q", out)
	}
}

func TestHasCodeBlocks(t *testing.T) {
	if !HasCodeBlocks("before\n```go\nx := 1\n```\n") || HasCodeBlocks("`inline` only") {
		t.Fatal("only fenced blocks count")
	}
}
