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
			"Ensure Ollama is running: ollama serve",
			"Check the host URL in your config: ~/.taracode/config.yaml",
			"Verify the server is accessible: curl <host>/api/tags",
		},
	},
	{
		Pattern: "no such host",
		Title:   "Host Not Found",
		Suggestions: []string{
			"Check your network connection",
			"Verify the hostname in TARACODE_HOST or config.yaml",
			"Try using localhost:11434 for local Ollama",
		},
	},
	{
		Pattern: "connection reset",
		Title:   "Connection Reset",
		Suggestions: []string{
			"The server closed the connection unexpectedly",
			"Check if Ollama has enough memory for the model",
			"Try a smaller model: taracode --model gemma3:12b",
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
			"Pull the model: ollama pull <model>",
			"List available models: ollama list",
			"Switch models with: /model",
		},
	},
	{
		Pattern: "no models available",
		Title:   "No Models Available",
		Suggestions: []string{
			"No models are loaded on the server",
			"Pull a model: ollama pull gemma3:27b",
			"Check Ollama status: ollama list",
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
	sb.WriteString(fmt.Sprintf("  %s Start Ollama: ollama serve\n", IconTip))
	sb.WriteString(fmt.Sprintf("  %s Check config: ~/.taracode/config.yaml\n", IconTip))
	sb.WriteString(fmt.Sprintf("  %s Test connection: curl %s/api/tags\n", IconTip, host))

	return sb.String()
}


// VerboseToolError represents a detailed tool error for verbose mode
type VerboseToolError struct {
	ToolName    string
	Params      map[string]interface{}
	Error       string
	ExitCode    int
	Stderr      string
	RootCause   string
	Suggestion  string
	Context     string
	Severity    string
	Recoverable bool
}

// FormatVerboseToolError formats a tool error with detailed diagnostics
func FormatVerboseToolError(err VerboseToolError) string {
	var sb strings.Builder

	// Header with severity color
	severityStyle := WarningStyle
	switch err.Severity {
	case "critical":
		severityStyle = ErrorStyle
	case "high":
		severityStyle = ErrorStyle
	case "low":
		severityStyle = Subtle
	}

	sb.WriteString(ErrorStyle.Render(fmt.Sprintf("%s Tool Failure: %s", IconError, err.ToolName)))
	sb.WriteString("\n\n")

	// Parameters if present
	if len(err.Params) > 0 {
		sb.WriteString(Bold.Render("Parameters:"))
		sb.WriteString("\n")
		for k, v := range err.Params {
			paramVal := fmt.Sprintf("%v", v)
			if len(paramVal) > 60 {
				paramVal = paramVal[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, Subtle.Render(paramVal)))
		}
		sb.WriteString("\n")
	}

	// Error message
	sb.WriteString(Bold.Render("Error:"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s\n", err.Error))

	// Exit code if present
	if err.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf("  Exit code: %d\n", err.ExitCode))
	}
	sb.WriteString("\n")

	// Stderr if present and verbose
	if err.Stderr != "" {
		sb.WriteString(Bold.Render("stderr:"))
		sb.WriteString("\n")
		// Limit to 10 lines
		lines := strings.Split(err.Stderr, "\n")
		displayLines := lines
		if len(lines) > 10 {
			displayLines = lines[:10]
		}
		for _, line := range displayLines {
			if len(line) > 80 {
				line = line[:77] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s\n", Subtle.Render(line)))
		}
		if len(lines) > 10 {
			sb.WriteString(fmt.Sprintf("  %s\n", Subtle.Render(fmt.Sprintf("... and %d more lines", len(lines)-10))))
		}
		sb.WriteString("\n")
	}

	// Analysis
	sb.WriteString(IconDiagnostics + " " + Bold.Render("Analysis:"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  Root Cause: %s\n", err.RootCause))
	sb.WriteString(fmt.Sprintf("  Severity: %s\n", severityStyle.Render(err.Severity)))

	// Context if present
	if err.Context != "" {
		sb.WriteString(fmt.Sprintf("  Context: %s\n", err.Context))
	}

	// Recovery status
	if err.Recoverable {
		sb.WriteString(fmt.Sprintf("  Status: %s\n", SuccessStyle.Render("Recoverable")))
	} else {
		sb.WriteString(fmt.Sprintf("  Status: %s\n", ErrorStyle.Render("Not recoverable")))
	}
	sb.WriteString("\n")

	// Suggestion
	sb.WriteString(IconTip + " " + Bold.Render("Suggestion:"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  %s\n", err.Suggestion))

	return sb.String()
}

// FormatStepFailure formats a task step failure with optional verbose details
func FormatStepFailure(stepNum int, stepName string, err string, verbose bool) string {
	var sb strings.Builder

	sb.WriteString(ErrorStyle.Render(fmt.Sprintf("%s Step %d failed: %s", IconError, stepNum, stepName)))
	sb.WriteString("\n")

	if verbose {
		sb.WriteString(Subtle.Render(fmt.Sprintf("  Error: %s", err)))
		sb.WriteString("\n")
	}

	return sb.String()
}
