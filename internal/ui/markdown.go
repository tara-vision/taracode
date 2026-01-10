package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
)

var markdownRenderer *glamour.TermRenderer

func init() {
	initMarkdownRenderer()
}

// initMarkdownRenderer initializes the Glamour markdown renderer
func initMarkdownRenderer() {
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

// RenderMarkdown renders markdown content with syntax highlighting
func RenderMarkdown(content string) string {
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
	markdownRenderer = nil
}

// EnableMarkdown re-enables markdown rendering
func EnableMarkdown() {
	initMarkdownRenderer()
}

// IsMarkdownEnabled returns whether markdown rendering is available
func IsMarkdownEnabled() bool {
	return markdownRenderer != nil
}
