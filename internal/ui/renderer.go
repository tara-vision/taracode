package ui

import (
	"fmt"
	"path/filepath"
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

	title := TitleStyle.Render(IconStar + " Tara Code")
	subtitle := Subtle.Render("AI-powered CLI assistant")

	sb.WriteString(fmt.Sprintf("%s - %s\n", title, subtitle))
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

// FormatToolStatus returns styled tool execution status
func (r *Renderer) FormatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	if isError {
		return ToolError.Render(IconError + " " + tool + " failed")
	}

	switch tool {
	case "read_file":
		filePath, _ := params["file_path"].(string)
		lines := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Read %s (%d lines)", IconArrow, filepath.Base(filePath), lines))

	case "search_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No matches") {
			return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (no matches)", IconArrow, pattern))
		}
		return ToolRead.Render(fmt.Sprintf("%s Searched for \"%s\" (%d matches)", IconArrow, pattern, matches))

	case "list_files":
		dir, _ := params["directory"].(string)
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		items := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Listed %s (%d items)", IconArrow, dir, items))

	case "execute_command":
		cmd, _ := params["command"].(string)
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}
		return ToolRead.Render(fmt.Sprintf("%s Executed: %s", IconArrow, cmd))

	case "write_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Wrote %s", IconSuccess, filepath.Base(filePath)))

	case "append_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Appended to %s", IconSuccess, filepath.Base(filePath)))

	case "edit_file":
		filePath, _ := params["file_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Edited %s", IconSuccess, filepath.Base(filePath)))

	case "insert_lines":
		filePath, _ := params["file_path"].(string)
		lineNum, _ := params["line_number"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Inserted at line %d in %s", IconSuccess, int(lineNum), filepath.Base(filePath)))

	case "replace_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Replaced lines %d-%d in %s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath)))

	case "delete_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return ToolWrite.Render(fmt.Sprintf("%s Deleted lines %d-%d from %s", IconSuccess, int(startLine), int(endLine), filepath.Base(filePath)))

	case "copy_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Copied %s to %s", IconSuccess, filepath.Base(src), filepath.Base(dst)))

	case "move_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Moved %s to %s", IconSuccess, filepath.Base(src), filepath.Base(dst)))

	case "delete_file":
		filePath, _ := params["file_path"].(string)
		recursive, _ := params["recursive"].(bool)
		if recursive {
			return ToolWrite.Render(fmt.Sprintf("%s Deleted %s (recursive)", IconSuccess, filepath.Base(filePath)))
		}
		return ToolWrite.Render(fmt.Sprintf("%s Deleted %s", IconSuccess, filepath.Base(filePath)))

	case "create_directory":
		dirPath, _ := params["path"].(string)
		return ToolWrite.Render(fmt.Sprintf("%s Created directory %s", IconSuccess, filepath.Base(dirPath)))

	case "find_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No files found") {
			return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (no matches)", IconArrow, pattern))
		}
		return ToolRead.Render(fmt.Sprintf("%s Find \"%s\" (%d files)", IconArrow, pattern, matches))

	case "git_status":
		if strings.Contains(result, "clean") {
			return ToolRead.Render(fmt.Sprintf("%s Git status: clean", IconArrow))
		}
		changes := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git status: %d changes", IconArrow, changes))

	case "git_diff":
		if strings.Contains(result, "No changes") {
			return ToolRead.Render(fmt.Sprintf("%s Git diff: no changes", IconArrow))
		}
		lines := strings.Count(result, "\n")
		return ToolRead.Render(fmt.Sprintf("%s Git diff: %d lines", IconArrow, lines))

	case "git_log":
		commits := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git log: %d commits", IconArrow, commits))

	case "git_add":
		return ToolWrite.Render(fmt.Sprintf("%s Git: staged files", IconSuccess))

	case "git_commit":
		return ToolWrite.Render(fmt.Sprintf("%s Git: commit created", IconSuccess))

	case "git_branch":
		branches := strings.Count(result, "\n") + 1
		return ToolRead.Render(fmt.Sprintf("%s Git branches: %d", IconArrow, branches))

	default:
		return ToolRead.Render(fmt.Sprintf("%s %s completed", IconArrow, tool))
	}
}

// PromptString returns the styled prompt
func (r *Renderer) PromptString() string {
	return PromptStyle.Render("❯") + " "
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
