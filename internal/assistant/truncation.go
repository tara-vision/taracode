package assistant

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Default truncation limits
const (
	DefaultMaxToolOutputLines = 500
	DefaultMaxToolOutputChars = 15000
)

// TruncationConfig holds tool output truncation settings
type TruncationConfig struct {
	MaxLines int // Maximum lines per tool output (0 = unlimited)
	MaxChars int // Maximum characters per tool output (0 = unlimited)
}

// TruncationResult holds the result of a truncation operation
type TruncationResult struct {
	Output       string
	WasTruncated bool
	OrigLines    int
	OrigChars    int
	KeptLines    int
	KeptChars    int
	ToolName     string
}

// TruncateToolOutput applies size limits to tool output.
// Returns the (possibly truncated) output and metadata about what happened.
func TruncateToolOutput(output string, toolName string, cfg TruncationConfig) TruncationResult {
	if output == "" {
		return TruncationResult{Output: output, ToolName: toolName}
	}

	// Both limits disabled
	if cfg.MaxLines <= 0 && cfg.MaxChars <= 0 {
		return TruncationResult{Output: output, ToolName: toolName}
	}

	origChars := utf8.RuneCountInString(output)
	lines := strings.Split(output, "\n")
	origLines := len(lines)

	result := TruncationResult{
		ToolName:  toolName,
		OrigLines: origLines,
		OrigChars: origChars,
	}

	truncated := false

	// Apply line limit first
	if cfg.MaxLines > 0 && origLines > cfg.MaxLines {
		lines = truncateAtBoundary(lines, cfg.MaxLines, toolName)
		truncated = true
	}

	// Rejoin and check char limit
	joined := strings.Join(lines, "\n")
	currentChars := utf8.RuneCountInString(joined)

	if cfg.MaxChars > 0 && currentChars > cfg.MaxChars {
		joined = truncateChars(joined, cfg.MaxChars)
		truncated = true
		// Recount lines after char truncation
		lines = strings.Split(joined, "\n")
	}

	result.KeptLines = len(lines)
	result.KeptChars = utf8.RuneCountInString(joined)
	result.WasTruncated = truncated

	if truncated {
		notice := buildTruncationNotice(toolName, origLines, origChars, result.KeptLines, result.KeptChars)
		result.Output = joined + "\n" + notice
	} else {
		result.Output = output
	}

	return result
}

// truncateAtBoundary tries to truncate lines at a structural boundary.
// For JSON-like content, tries to end at a complete object.
// For other content, truncates at the line limit.
func truncateAtBoundary(lines []string, maxLines int, toolName string) []string {
	if len(lines) <= maxLines {
		return lines
	}

	kept := lines[:maxLines]

	// For JSON content, try to find a complete boundary
	if looksLikeJSON(lines) {
		kept = truncateJSONBoundary(lines, maxLines)
	}

	return kept
}

// truncateChars truncates output at character limit, trying to break at a line boundary
func truncateChars(output string, maxChars int) string {
	runes := []rune(output)
	if len(runes) <= maxChars {
		return output
	}

	// Truncate to maxChars runes
	truncated := string(runes[:maxChars])

	// Try to break at the last newline to avoid splitting a line
	// Use byte length of truncated string for consistent comparison
	lastNewline := strings.LastIndex(truncated, "\n")
	truncatedLen := len(truncated)
	if lastNewline > 0 && lastNewline > truncatedLen*3/4 {
		truncated = truncated[:lastNewline]
	}

	return truncated
}

// looksLikeJSON checks if the content appears to be JSON
func looksLikeJSON(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	firstLine := strings.TrimSpace(lines[0])
	return strings.HasPrefix(firstLine, "{") || strings.HasPrefix(firstLine, "[")
}

// truncateJSONBoundary tries to truncate JSON at a complete object boundary
func truncateJSONBoundary(lines []string, maxLines int) []string {
	if len(lines) <= maxLines {
		return lines
	}

	// Search backward from maxLines for a line that closes a JSON object/array
	// This is a heuristic, not a parser
	for i := maxLines - 1; i > maxLines*3/4; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "}," || trimmed == "}" || trimmed == "]," || trimmed == "]" ||
			trimmed == "}]" || trimmed == "}]," {
			return lines[:i+1]
		}
	}

	// No good boundary found, truncate at limit
	return lines[:maxLines]
}

// buildTruncationNotice creates the notice appended to truncated output
func buildTruncationNotice(toolName string, origLines, origChars, keptLines, keptChars int) string {
	var parts []string

	if origLines != keptLines {
		parts = append(parts, fmt.Sprintf("showing first %d of %d lines", keptLines, origLines))
	}
	if origChars != keptChars {
		parts = append(parts, fmt.Sprintf("%d of %d chars", keptChars, origChars))
	}

	detail := strings.Join(parts, ", ")

	// Add tool-specific hint for how to get more
	hint := ""
	switch toolName {
	case "read_file":
		hint = " Use start_line/end_line parameters to read specific sections."
	case "search_files":
		hint = " Narrow your search pattern to reduce results."
	case "execute_command":
		hint = " Pipe to head/tail or grep to filter output."
	case "kubectl_logs":
		hint = " Use --tail or --since to limit log output."
	case "git_log":
		hint = " Use max_count parameter to limit commits."
	case "git_diff":
		hint = " Specify file paths to limit diff scope."
	}

	return fmt.Sprintf("[Output truncated: %s.%s]", detail, hint)
}

// isBinaryContent checks if content appears to be binary (contains null bytes)
func isBinaryContent(output string) bool {
	// Check first 512 bytes for null bytes
	check := output
	if len(check) > 512 {
		check = check[:512]
	}
	return strings.ContainsRune(check, 0)
}

// TruncateBinaryOutput handles binary content detection
func TruncateBinaryOutput(output string, toolName string, maxBytes int) TruncationResult {
	if !isBinaryContent(output) {
		return TruncationResult{Output: output, ToolName: toolName}
	}

	if maxBytes <= 0 {
		maxBytes = 256
	}

	preview := output
	if len(preview) > maxBytes {
		preview = preview[:maxBytes]
	}

	result := TruncationResult{
		Output:       fmt.Sprintf("[Binary content detected, showing first %d bytes]\n%s", maxBytes, preview),
		WasTruncated: true,
		OrigChars:    len(output),
		KeptChars:    maxBytes,
		ToolName:     toolName,
	}
	return result
}
