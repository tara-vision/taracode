package cmd

import (
	"regexp"
	"strings"

	"github.com/chzyer/readline"
)

// DetectSuggestion analyzes AI response and returns a suggested user reply
func DetectSuggestion(response string) string {
	// Trim and get the last part of the response
	response = strings.TrimSpace(response)

	// Check for [y/N] or [Y/n] patterns (case indicates default)
	ynPattern := regexp.MustCompile(`\[(y/N|Y/n|y/n|Y/N)\]`)
	if match := ynPattern.FindString(response); match != "" {
		// Default to lowercase option (the recommended one)
		if strings.Contains(match, "y/N") {
			return "n"
		}
		if strings.Contains(match, "Y/n") {
			return "y"
		}
		return "y" // Default to yes for other patterns
	}

	// Check for (y/n) pattern
	if strings.Contains(response, "(y/n)") || strings.Contains(response, "(Y/N)") {
		return "y"
	}

	// Check for "Would you like me to..." or "Should I..." or "Do you want..."
	// These typically expect "yes" as the positive response
	lowerResp := strings.ToLower(response)
	if strings.Contains(lowerResp, "would you like") ||
		strings.Contains(lowerResp, "should i ") ||
		strings.Contains(lowerResp, "do you want") ||
		strings.Contains(lowerResp, "shall i ") {
		// Only suggest if it ends with a question mark
		if strings.HasSuffix(strings.TrimSpace(response), "?") {
			return "yes"
		}
	}

	return ""
}

// SlashCommand represents a REPL command with description
type SlashCommand struct {
	Command     string
	Description string
}

// GetSlashCommands returns all available slash commands
func GetSlashCommands() []SlashCommand {
	return []SlashCommand{
		// Project
		{"/init", "Initialize project (creates TARACODE.md and .taracode/)"},
		{"/reload", "Reload project context from TARACODE.md"},
		{"/status", "Show project and session status"},
		// Model & Mode
		{"/model", "Switch between available Ollama models"},
		{"/mode", "Show or switch operating mode"},
		{"/mode devops", "Switch to DevOps mode"},
		{"/mode security", "Switch to Security mode (Pro/Max only)"},
		// Sessions
		{"/session", "Show current session info"},
		{"/sessions", "List all conversation sessions"},
		{"/session new", "Start a new session"},
		{"/session load", "Load a previous session by ID"},
		{"/session delete", "Delete a session by ID"},
		{"/session rename", "Rename a session"},
		{"/clear", "Clear conversation and start new session"},
		// Tasks
		{"/task", "Plan and execute a multi-step task"},
		{"/task list", "List all tasks"},
		{"/task status", "Show task status"},
		{"/task resume", "Resume a paused task"},
		{"/task pause", "Pause the active task"},
		{"/task abort", "Abort the active task"},
		{"/task rollback", "Rollback to last checkpoint"},
		{"/task templates", "List available task templates"},
		{"/task run", "Run a task from a template"},
		// Plans
		{"/plan", "Show active task plan"},
		{"/diff", "Show file changes as unified diff"},
		{"/diff export", "Export changes to .patch file"},
		// Permissions
		{"/permissions", "Show current permission settings"},
		{"/permissions reset", "Reset permissions to default"},
		{"/permissions allow", "Always allow tool or category"},
		{"/permissions deny", "Always deny tool or category"},
		{"/permissions ask", "Always ask for tool or category"},
		// Security Audit
		{"/audit", "View security audit log (security mode only)"},
		{"/audit export json", "Export audit log to JSON file"},
		{"/audit export html", "Export audit log to HTML report"},
		{"/audit clear", "Clear the audit log"},
		// History & Undo
		{"/history", "Show last 20 file operations"},
		{"/history all", "Show all operations"},
		{"/undo", "Undo last file modification"},
		{"/undo --dry-run", "Preview what would be undone"},
		// MCP
		{"/mcp", "List configured MCP servers and status"},
		{"/mcp connect", "Connect to an MCP server"},
		{"/mcp disconnect", "Disconnect from an MCP server"},
		{"/mcp tools", "List tools from connected servers"},
		// Agents
		{"/agent", "Show agent system overview"},
		{"/agent list", "List all available agents"},
		{"/agent status", "Show status of all agents"},
		{"/agent config", "Show agent configuration"},
		{"/agent config example", "Show example agents.yaml content"},
		{"/agent config init", "Create .taracode/agents.yaml template"},
		{"/agent use", "Route next prompt to specific agent"},
		{"/agent help", "Show agent command help"},
		// Memory
		{"/remember", "Save a memory about this project"},
		{"/memory", "List all project memories"},
		{"/memory search", "Search memories by keyword"},
		{"/memory delete", "Delete a memory by ID"},
		{"/memory export", "Export memories to JSON file"},
		{"/memory import", "Import memories from file"},
		{"/memory stats", "Show memory statistics"},
		{"/memory cleanup", "Remove old unused memories"},
		{"/memory clear", "Clear all memories"},
		// Watch (Screen Monitoring)
		{"/watch", "Screen monitoring and analysis"},
		{"/watch this", "Capture and analyze all screens now"},
		{"/watch start", "Start continuous screen monitoring"},
		{"/watch stop", "Stop screen monitoring"},
		{"/watch status", "Show monitoring status"},
		// Other
		{"/context", "Show what's in the LLM context window"},
		{"/context --agents", "Show per-agent context usage"},
		{"/tools", "List available AI tools"},
		{"/usage", "Show token usage statistics"},
		{"/help", "Show help message"},
		// Hosts (Multi-Host v2.0)
		{"/hosts", "Show all configured hosts and their status"},
		{"/hosts check", "Force health check on all hosts"},
		{"/hosts reconnect", "Reconnect to unhealthy hosts"},
		// Upgrade
		{"/upgrade", "Check for and install updates"},
		{"/upgrade check", "Check for new version"},
		{"/upgrade now", "Upgrade to latest version"},
		{"/upgrade skip", "Skip the current available update"},
		{"/upgrade changelog", "Show full release notes"},
		{"/upgrade status", "Show upgrade state information"},
	}
}

// SlashCompleter provides autocompletion for slash commands
type SlashCompleter struct {
	fileCompleter *FileCompleter
	suggestion    string // AI-suggested response (Tab to accept)
}

// NewSlashCompleter creates a new slash command completer
func NewSlashCompleter(workingDir string) *SlashCompleter {
	return &SlashCompleter{
		fileCompleter: NewFileCompleter(workingDir),
	}
}

// SetSuggestion sets the AI-suggested response that can be accepted with Tab
func (s *SlashCompleter) SetSuggestion(suggestion string) {
	s.suggestion = suggestion
}

// ClearSuggestion removes the current suggestion
func (s *SlashCompleter) ClearSuggestion() {
	s.suggestion = ""
}

// GetSuggestion returns the current suggestion
func (s *SlashCompleter) GetSuggestion() string {
	return s.suggestion
}

// UpdateWorkingDir updates the working directory for file completion
func (s *SlashCompleter) UpdateWorkingDir(newDir string) {
	s.fileCompleter.UpdateWorkingDir(newDir)
}

// Do implements readline.AutoCompleter interface
func (s *SlashCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])

	// If input is empty and we have a suggestion, offer it
	if lineStr == "" && s.suggestion != "" {
		return [][]rune{[]rune(s.suggestion)}, 0
	}

	// Check if we're completing a slash command
	if strings.HasPrefix(lineStr, "/") {
		return s.completeSlashCommand(lineStr)
	}

	// Check if we're completing a file reference
	if strings.Contains(lineStr, "@") {
		return s.fileCompleter.Do(line, pos)
	}

	return nil, 0
}

// completeSlashCommand provides completion for slash commands
func (s *SlashCompleter) completeSlashCommand(prefix string) ([][]rune, int) {
	commands := GetSlashCommands()

	var matches [][]rune
	for _, cmd := range commands {
		if strings.HasPrefix(cmd.Command, prefix) {
			// Return the part after the prefix
			suffix := cmd.Command[len(prefix):]
			matches = append(matches, []rune(suffix))
		}
	}

	if len(matches) == 0 {
		return nil, 0
	}

	return matches, len(prefix)
}

// GetPromptCompleter creates a PrefixCompleter for readline with slash commands
func GetPromptCompleter(workingDir string) readline.AutoCompleter {
	return NewSlashCompleter(workingDir)
}
