package ui

import (
	"fmt"
	"sort"
	"strings"
)

// ErrorSuggestion represents an actionable suggestion for an error
type ErrorSuggestion struct {
	Pattern     string   // Pattern to match in error message (lowercase)
	Title       string   // Short title for the error
	Suggestions []string // Actionable suggestions
	DocURL      string   // Optional documentation URL
}

// Common error patterns and their suggestions
var errorSuggestions = []ErrorSuggestion{
	// Connection errors
	{
		Pattern: "connection refused",
		Title:   "Connection Refused",
		Suggestions: []string{
			"Start taraengine: taraengine serve",
			"Check the host in ~/.taracode/config.yaml, or pass --host",
			"Run taracode doctor",
		},
	},
	{
		Pattern: "no such host",
		Title:   "Host Not Found",
		Suggestions: []string{
			"Check your network connection",
			"Verify the hostname in TARACODE_HOST or config.yaml",
			"taraengine listens on http://127.0.0.1:8272 by default",
		},
	},
	{
		Pattern: "connection reset",
		Title:   "Connection Reset",
		Suggestions: []string{
			"The server closed the connection unexpectedly",
			"Check that the machine has the memory for the model: taracode doctor",
			"Try a smaller model: /model",
		},
	},
	{
		Pattern: "timeout",
		Title:   "Request Timeout",
		Suggestions: []string{
			"The request took too long to complete",
			"Try a simpler query or smaller context",
			"Check if the server is under heavy load",
		},
	},
	{
		Pattern: "context deadline exceeded",
		Title:   "Operation Timeout",
		Suggestions: []string{
			"The operation exceeded the time limit",
			"For long commands, increase timeout in params",
			"Break complex tasks into smaller steps",
		},
	},

	// Model errors
	{
		Pattern: "model not found",
		Title:   "Model Not Found",
		Suggestions: []string{
			"The requested model is not available",
			"Pull the model: taraengine pull <model>",
			"List the engine's models: taraengine list",
			"Switch models with: /model",
		},
	},
	{
		Pattern: "no models available",
		Title:   "No Models Available",
		Suggestions: []string{
			"No model is pulled on the server",
			"Pull one: taraengine pull <model> (taracode doctor recommends one for this machine)",
			"List the engine's models: taraengine list",
		},
	},

	// Tool errors
	{
		Pattern: "unknown tool",
		Title:   "Unknown Tool",
		Suggestions: []string{
			"The requested tool doesn't exist",
			"List available tools: /tools",
		},
	},
	{
		Pattern: "permission denied",
		Title:   "Permission Denied",
		Suggestions: []string{
			"You don't have permission for this operation",
			"Check tool permissions: /permissions",
			"Allow the tool: /permissions allow <tool>",
		},
	},

	// File errors
	{
		Pattern: "no such file or directory",
		Title:   "File Not Found",
		Suggestions: []string{
			"The specified file or directory doesn't exist",
			"Check the path and try again",
			"Use @<filename> with Tab for autocomplete",
		},
	},
	{
		Pattern: "file exists",
		Title:   "File Already Exists",
		Suggestions: []string{
			"The target file already exists",
			"Use a different filename or delete the existing file",
		},
	},

	// Rate limiting
	{
		Pattern: "429",
		Title:   "Rate Limited",
		Suggestions: []string{
			"Too many requests - slow down",
			"Wait a moment and try again",
		},
	},
	{
		Pattern: "rate limit",
		Title:   "Rate Limited",
		Suggestions: []string{
			"Request rate limit exceeded",
			"Wait a few seconds before retrying",
		},
	},

	// Git errors
	{
		Pattern: "not a git repository",
		Title:   "Not a Git Repository",
		Suggestions: []string{
			"This directory is not a git repository",
			"Initialize git: git init",
			"Or navigate to a git repository",
		},
	},

	// Command execution
	{
		Pattern: "command not found",
		Title:   "Command Not Found",
		Suggestions: []string{
			"The command is not installed or not in PATH",
			"Install the required tool",
			"Check your PATH environment variable",
		},
	},
}

// EnhanceError takes an error and returns an enhanced error message with suggestions
func EnhanceError(err error) string {
	if err == nil {
		return ""
	}

	if s, ok := serverSuggestion(err); ok {
		return FormatEnhancedError(err.Error(), s)
	}

	errStr := err.Error()
	errLower := strings.ToLower(errStr)

	// Find matching suggestion
	for _, suggestion := range errorSuggestions {
		if strings.Contains(errLower, suggestion.Pattern) {
			return FormatEnhancedError(errStr, suggestion)
		}
	}

	// No enhancement found, return original error
	return ErrorStyle.Render("Error: " + errStr)
}

// FormatEnhancedError formats an error with its suggestion
func FormatEnhancedError(errMsg string, suggestion ErrorSuggestion) string {
	var sb strings.Builder

	// Error header
	sb.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %s", suggestion.Title)))
	sb.WriteString("\n")

	// Original error (dimmed)
	sb.WriteString(Subtle.Render(fmt.Sprintf("  %s", errMsg)))
	sb.WriteString("\n\n")

	// Suggestions
	sb.WriteString(WarningStyle.Render("Suggestions:"))
	sb.WriteString("\n")
	for _, s := range suggestion.Suggestions {
		sb.WriteString(fmt.Sprintf("  %s %s\n", IconTip, s))
	}

	// Documentation URL if available
	if suggestion.DocURL != "" {
		sb.WriteString("\n")
		sb.WriteString(Subtle.Render(fmt.Sprintf("  Learn more: %s", suggestion.DocURL)))
		sb.WriteString("\n")
	}

	return sb.String()
}

// SuggestSimilarTools returns tool names similar to the given name
func SuggestSimilarTools(name string, availableTools []string) []string {
	if len(availableTools) == 0 {
		return nil
	}

	type scored struct {
		name  string
		score int
	}

	var matches []scored
	nameLower := strings.ToLower(name)

	for _, tool := range availableTools {
		toolLower := strings.ToLower(tool)
		score := 0

		// Exact prefix match
		if strings.HasPrefix(toolLower, nameLower) {
			score += 100
		}

		// Contains match
		if strings.Contains(toolLower, nameLower) {
			score += 50
		}

		// Partial word match
		nameParts := strings.Split(nameLower, "_")
		toolParts := strings.Split(toolLower, "_")
		for _, np := range nameParts {
			for _, tp := range toolParts {
				if strings.HasPrefix(tp, np) || strings.HasPrefix(np, tp) {
					score += 25
				}
			}
		}

		// Levenshtein-like: count common characters
		for _, c := range nameLower {
			if strings.ContainsRune(toolLower, c) {
				score += 1
			}
		}

		if score > 10 { // Minimum threshold
			matches = append(matches, scored{name: tool, score: score})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	// Return top 3 suggestions
	var result []string
	for i, m := range matches {
		if i >= 3 {
			break
		}
		result = append(result, m.name)
	}

	return result
}

// EnhanceUnknownToolError provides a better error for unknown tools
func EnhanceUnknownToolError(toolName string, availableTools []string) string {
	var sb strings.Builder

	sb.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: Unknown tool '%s'", toolName)))
	sb.WriteString("\n\n")

	// Suggest similar tools
	similar := SuggestSimilarTools(toolName, availableTools)
	if len(similar) > 0 {
		sb.WriteString(WarningStyle.Render("Did you mean:"))
		sb.WriteString("\n")
		for _, s := range similar {
			sb.WriteString(fmt.Sprintf("  %s %s\n", IconTip, s))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(Subtle.Render("Run /tools to see all available tools"))
	sb.WriteString("\n")

	return sb.String()
}

// FormatConnectionError provides a detailed connection error message
func FormatConnectionError(host string, err error) string {
	var sb strings.Builder

	sb.WriteString(ErrorStyle.Render("Error: Cannot connect to LLM server"))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render(fmt.Sprintf("  Host: %s", host)))
	sb.WriteString("\n")
	sb.WriteString(Subtle.Render(fmt.Sprintf("  %s", err.Error())))
	sb.WriteString("\n\n")

	sb.WriteString(WarningStyle.Render("Suggestions:"))
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "  %s Start taraengine: taraengine serve\n", IconTip)
	fmt.Fprintf(&sb, "  %s Check config: ~/.taracode/config.yaml\n", IconTip)
	fmt.Fprintf(&sb, "  %s Test the connection: curl %s/api/health\n", IconTip, host)

	return sb.String()
}
