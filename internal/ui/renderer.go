package ui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
)

// Config holds UI configuration options
type Config struct {
	EnableColor    bool
	EnableSpinner  bool
	EnableMarkdown bool
}

// DefaultConfig returns the default UI configuration
func DefaultConfig() *Config {
	return &Config{
		EnableColor:    true,
		EnableSpinner:  true,
		EnableMarkdown: true,
	}
}

// Renderer handles all UI output formatting
type Renderer struct {
	config *Config
}

// NewRenderer creates a new renderer with default config
func NewRenderer() *Renderer {
	return &Renderer{
		config: DefaultConfig(),
	}
}

// NewRendererWithConfig creates a renderer with custom config
func NewRendererWithConfig(config *Config) *Renderer {
	return &Renderer{
		config: config,
	}
}

// WelcomeMessage returns the styled welcome banner
func (r *Renderer) WelcomeMessage() string {
	var sb strings.Builder
	title := TitleStyle.Render(IconCloud + " Tara Code")
	subtitle := Subtle.Render("DevOps & Cloud AI Assistant")
	fmt.Fprintf(&sb, "%s - %s\n", title, subtitle)
	sb.WriteString(Subtle.Render("Type '/help' for commands, 'exit' to quit"))
	sb.WriteString("\n")
	return sb.String()
}

// ProjectContextMessage returns styled project context info
func (r *Renderer) ProjectContextMessage(loaded bool) string {
	if loaded {
		return SuccessStyle.Render(IconFolder+" Project context loaded from TARACODE.md") + "\n"
	}
	return WarningStyle.Render(IconTip+" Run '/init' to initialize project context") + "\n"
}

// SessionResumeMessage returns styled session info
func (r *Renderer) SessionResumeMessage(messageCount int) string {
	var sb strings.Builder
	sb.WriteString(SessionStyle.Render(fmt.Sprintf("%s Resuming session with %d previous messages", IconSession, messageCount)))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render("   Type '/session new' to start fresh"))
	sb.WriteString("\n")
	return sb.String()
}

// formatDuration returns a human-readable duration suffix for tool execution.
// Returns empty string for durations under 1 second to reduce noise.
func formatDuration(durationMs int64) string {
	if durationMs < 1000 {
		return ""
	}
	seconds := float64(durationMs) / 1000.0
	if seconds < 60 {
		return fmt.Sprintf(" [%.1fs]", seconds)
	}
	minutes := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf(" [%dm%ds]", minutes, secs)
}

// FormatToolStatus returns styled tool execution status
func (r *Renderer) FormatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	return r.FormatToolStatusWithDuration(tool, params, result, isError, 0)
}

// FormatToolStatusWithDuration returns styled tool execution status with optional duration: one
// line per built-in tool, "<tool> completed" for anything else (MCP tools).
func (r *Renderer) FormatToolStatusWithDuration(
	tool string, params map[string]interface{}, result string, isError bool, durationMs int64,
) string {
	dur := formatDuration(durationMs)
	if isError {
		return ToolError.Render(IconError + " " + tool + " failed" + dur)
	}
	switch tool {
	case "write_file":
		return ToolWrite.Render(fmt.Sprintf("%s Wrote %s%s", IconSuccess, filepath.Base(param(params, "path")), dur))
	case "edit_file":
		return ToolWrite.Render(fmt.Sprintf("%s Edited %s%s", IconSuccess, filepath.Base(param(params, "path")), dur))
	}
	return ToolRead.Render(fmt.Sprintf("%s %s%s", IconArrow, readStatus(tool, params, result), dur))
}

// readStatus describes a finished call of any tool but the two file writers.
func readStatus(tool string, params map[string]interface{}, result string) string {
	switch tool {
	case "read_file":
		return fmt.Sprintf("Read %s (%d lines)", filepath.Base(param(params, "path")), strings.Count(result, "\n")+1)
	case "search_files":
		pattern := param(params, "pattern")
		if strings.HasPrefix(result, "No matches") {
			return fmt.Sprintf("Searched for \"%s\" (no matches)", pattern)
		}
		return fmt.Sprintf("Searched for \"%s\" (%d matches)", pattern, strings.Count(result, "\n")+1)
	case "list_files":
		dir := param(params, "path")
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		if strings.HasPrefix(result, "No entries") {
			return fmt.Sprintf("Listed %s (empty)", dir)
		}
		return fmt.Sprintf("Listed %s (%d items)", dir, strings.Count(result, "\n")+1)
	case "shell":
		return "Executed: " + TruncateString(param(params, "command"), MaxCommandDisplay)
	case "git", "helm", "docker":
		return strings.TrimSpace(tool + " " + firstWords(param(params, "args"), 1))
	case "kubectl":
		return strings.Join(nonEmpty(tool, param(params, "verb"), param(params, "resource"), param(params, "name")), " ")
	case "terraform":
		return strings.TrimSpace(tool + " " + param(params, "command"))
	case "cloud":
		return strings.Join(nonEmpty(param(params, "provider"), firstWords(param(params, "args"), 2)), " ")
	case "scan":
		target := param(params, "target")
		if target == "" || target == "." {
			target = "current directory"
		}
		return strings.Join(nonEmpty(param(params, "scanner"), "scan", target), " ")
	case "web_search":
		return searchStatus(param(params, "query"), result)
	case "web_fetch":
		return "Fetched " + urlHost(param(params, "url"))
	}
	return tool + " completed"
}

// numberedResult matches the "1. Title" line web_search writes for every result.
var numberedResult = regexp.MustCompile(`(?m)^\d+\. `)

// searchStatus summarises a web_search result: the result count, or that only an answer came back.
func searchStatus(query, result string) string {
	query = TruncateString(query, 40)
	count := len(numberedResult.FindAllString(result, -1))
	switch {
	case count == 0 && strings.Contains(result, "\nAnswer: "):
		return fmt.Sprintf("Searched \"%s\" (found answer)", query)
	case count == 0:
		return fmt.Sprintf("Searched \"%s\" (no results)", query)
	}
	return fmt.Sprintf("Searched \"%s\" (%d results)", query, count)
}

// param returns a string parameter, "" when absent or not a string.
func param(params map[string]interface{}, name string) string {
	s, _ := params[name].(string)
	return strings.TrimSpace(s)
}

// firstWords returns the first n whitespace-separated words of s.
func firstWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) > n {
		words = words[:n]
	}
	return strings.Join(words, " ")
}

// nonEmpty drops the empty strings.
func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// urlHost is the host of a URL, or the URL itself (shortened) when it does not parse.
func urlHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return TruncateString(raw, 50)
}

// PromptString returns the styled prompt
func (r *Renderer) PromptString() string {
	return PromptStyle.Render("❯") + " "
}

// Dim renders text in the muted style used for model reasoning, which is shown but never stored.
func (r *Renderer) Dim(s string) string {
	if r.config != nil && !r.config.EnableColor {
		return s
	}
	return Subtle.Render(s)
}

// ErrorMessage formats an error message
func (r *Renderer) ErrorMessage(err error) string {
	return ToolError.Render(fmt.Sprintf("%s Error: %v", IconError, err))
}

// WarningMessage formats a warning message
func (r *Renderer) WarningMessage(msg string) string {
	return WarningStyle.Render(fmt.Sprintf("%s %s", IconWarning, msg))
}

// InfoMessage formats an info message
func (r *Renderer) InfoMessage(msg string) string {
	return SessionStyle.Render(fmt.Sprintf("%s %s", IconInfo, msg))
}

// SuccessMessage formats a success message
func (r *Renderer) SuccessMessage(msg string) string {
	return SuccessStyle.Render(fmt.Sprintf("%s %s", IconSuccess, msg))
}

// FormatUsage formats token usage statistics for display
func (r *Renderer) FormatUsage(usage *storage.TokenUsage) string {
	if usage == nil || usage.TotalTokens == 0 {
		return Subtle.Render("No token usage recorded yet.")
	}

	var sb strings.Builder
	sb.WriteString(SessionStyle.Render(IconInfo+" Token Usage") + "\n")
	sb.WriteString(fmt.Sprintf("  Prompt tokens:     %d\n", usage.PromptTokens))
	sb.WriteString(fmt.Sprintf("  Completion tokens: %d\n", usage.CompletionTokens))
	sb.WriteString(fmt.Sprintf("  Total tokens:      %d\n", usage.TotalTokens))

	return sb.String()
}

// ProviderMessage formats provider information for display
func (r *Renderer) ProviderMessage(info *provider.Info) string {
	if info == nil {
		return ""
	}
	return SuccessStyle.Render(fmt.Sprintf("%s Connected to %s", IconSuccess, info.Name)) + "\n"
}

// SearchFallbackMessage formats a search provider fallback notification
func (r *Renderer) SearchFallbackMessage(from, to string, reason error) string {
	var reasonStr string
	if reason != nil {
		errMsg := reason.Error()
		// Simplify common error messages
		switch {
		case strings.Contains(errMsg, "429") || strings.Contains(strings.ToLower(errMsg), "rate limit"):
			reasonStr = "rate limited"
		case strings.Contains(strings.ToLower(errMsg), "timeout"):
			reasonStr = "timed out"
		case strings.Contains(strings.ToLower(errMsg), "connection"):
			reasonStr = "connection error"
		default:
			// Truncate long error messages
			if len(errMsg) > 30 {
				reasonStr = errMsg[:27] + "..."
			} else {
				reasonStr = errMsg
			}
		}
	}

	msg := fmt.Sprintf("Search: %s %s %s", from, IconArrow, to)
	if reasonStr != "" {
		msg += fmt.Sprintf(" (%s)", reasonStr)
	}
	return WarningStyle.Render(fmt.Sprintf("%s %s", IconWarning, msg))
}
