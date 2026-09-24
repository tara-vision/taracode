package ui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	// markdownMu serializes every use of markdownRenderer: a glamour renderer keeps its block stack
	// in the renderer itself, so two renders at once corrupt each other (ruling P3-R47).
	markdownMu       sync.Mutex
	markdownRenderer *glamour.TermRenderer
)

func init() {
	initMarkdownRenderer()
}

// initMarkdownRenderer initializes the Glamour markdown renderer
func initMarkdownRenderer() {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	var err error

	// Detect terminal width for word wrapping
	width := 100
	// Try to get actual terminal width (gracefully handle errors)
	// This is a simple approach; could be enhanced with terminal detection

	// Use auto style which adapts to light/dark terminals
	markdownRenderer, err = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		// Fallback: renderer will be nil, RenderMarkdown returns plain text
		markdownRenderer = nil
	}
}

// RenderMarkdown renders markdown content with syntax highlighting. It is safe for concurrent use.
func RenderMarkdown(content string) string {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	if markdownRenderer == nil {
		return content
	}

	rendered, err := markdownRenderer.Render(content)
	if err != nil {
		return content
	}

	// Trim extra whitespace that glamour sometimes adds
	return strings.TrimSpace(rendered)
}

// RenderMarkdownToWriter renders markdown and writes to the given writer
func RenderMarkdownToWriter(content string) {
	rendered := RenderMarkdown(content)
	os.Stdout.WriteString(rendered)
	os.Stdout.WriteString("\n")
}

// HasCodeBlocks checks if content contains markdown code blocks
func HasCodeBlocks(content string) bool {
	return strings.Contains(content, "```")
}

// SetWordWrap reinitializes the renderer with a new word wrap width
func SetWordWrap(width int) {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	var err error
	markdownRenderer, err = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		markdownRenderer = nil
	}
}

// DisableMarkdown disables markdown rendering (returns plain text)
func DisableMarkdown() {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	markdownRenderer = nil
}

// EnableMarkdown re-enables markdown rendering
func EnableMarkdown() {
	initMarkdownRenderer()
}

// IsMarkdownEnabled returns whether markdown rendering is available
func IsMarkdownEnabled() bool {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	return markdownRenderer != nil
}
