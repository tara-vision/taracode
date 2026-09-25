package cmd

import (
	"fmt"
	"strings"
)

// command is one slash command: its name, argument hint and one-line summary (for /help and the
// completer), the group /help lists it under, and the method that runs it.
type command struct {
	name    string
	args    string
	summary string
	group   string
	run     func(r *repl, args []string)
}

// commandTable is the single source of truth for dispatch, /help and slash completion. The order is
// the display order; groups must be contiguous.
func commandTable() []command {
	return []command{
		{"/init", "", "Initialize the project (creates TARACODE.md and .taracode/)", "Project", (*repl).cmdInit},
		{"/reload", "", "Reload project context from TARACODE.md", "Project", (*repl).cmdReload},
		{"/status", "", "Show project and session status", "Project", (*repl).cmdStatus},
		{
			"/session", "[new [name]|load <id>|delete <id>|rename <id> <name>]",
			"Show or manage the current session", "Session", (*repl).cmdSession,
		},
		{"/sessions", "", "List all sessions", "Session", (*repl).cmdSessions},
		{"/clear", "", "Clear the conversation (new session)", "Session", (*repl).cmdClear},
		{"/model", "", "Switch between available models", "Model", (*repl).cmdModel},
		{"/think", "[auto|off|on|low|medium|high]", "Show or set the reasoning mode", "Model", (*repl).cmdThink},
		{"/mode", "[investigate|operate]", "Show or switch the operating mode", "Safety", (*repl).cmdMode},
		{
			"/permissions", "[allow|deny|ask <tool|all>|reset]", "Remembered answers for mutations", "Safety",
			(*repl).cmdPermissions,
		},
		{"/audit", "[all|export json|clear]", "Mutations recorded in this project", "Safety", (*repl).cmdAudit},
		{"/policy", "show", "Effective policy and where it comes from", "Safety", (*repl).cmdPolicy},
		{"/plan", "", "Show the active plan", "Context", (*repl).cmdPlan},
		{"/context", "", "Context window budget breakdown", "Context", (*repl).cmdContext},
		{"/compact", "", "Force conversation compaction", "Context", (*repl).cmdCompact},
		{"/stats", "", "Session statistics", "Context", (*repl).cmdStats},
		{"/usage", "", "Token usage for this session", "Context", (*repl).cmdUsage},
		{"/history", "[n|all]", "File operation history", "Files", (*repl).cmdHistory},
		{"/undo", "[n|--dry-run]", "Undo file modifications", "Files", (*repl).cmdUndo},
		{"/diff", "[export]", "Show or export session changes", "Files", (*repl).cmdDiff},
		{"/remember", "<text> [#tag]", "Save a memory about this project", "Memory", (*repl).cmdRemember},
		{
			"/memory", "[search <q>|delete <id>|export|import <file>|stats|cleanup|clear]",
			"Project memories", "Memory", (*repl).cmdMemory,
		},
		{"/mcp", "[connect|disconnect <name>|tools]", "MCP servers and their tools", "Servers", (*repl).cmdMCP},
		{"/tools", "", "List available tools", "Servers", (*repl).cmdTools},
		{"/upgrade", "[check|now|skip|changelog|status]", "Check for and install updates", "Diagnostics", (*repl).cmdUpgrade},
		{"/doctor", "", "Diagnose the LLM server and tools", "Diagnostics", (*repl).cmdDoctor},
		{"/help", "", "Show this help", "Diagnostics", (*repl).cmdHelp},
	}
}

// lookupCommand finds a command by its name, "/mode" for example.
func lookupCommand(name string) (command, bool) {
	for _, c := range commandTable() {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// dispatch runs the slash command on the line, or says it is unknown.
func (r *repl) dispatch(line string) {
	fields := strings.Fields(line)
	c, ok := lookupCommand(fields[0])
	if !ok {
		fmt.Printf("Unknown command: %s (type /help for the list)\n\n", fields[0])
		return
	}
	c.run(r, fields[1:])
}

// helpText renders /help from the table, grouped, followed by the non-slash inputs.
func helpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	group := ""
	for _, c := range commandTable() {
		if c.group != group {
			group = c.group
			fmt.Fprintf(&b, "\n  %s\n", group)
		}
		fmt.Fprintf(&b, "    %-40s %s\n", strings.TrimSpace(c.name+" "+c.args), c.summary)
	}
	b.WriteString("\n  Other\n")
	b.WriteString("    cd <dir> / cd .. / cd                    Navigate within the project (sandboxed)\n")
	b.WriteString("    pwd                                      Show the current directory\n")
	b.WriteString("    @file                                    Reference a file in a prompt (Tab completes)\n")
	b.WriteString("    exit / quit                              Exit with a session summary\n")
	b.WriteString("\n  taracode is free and open source. Sponsor: https://github.com/sponsors/tara-vision\n")
	return b.String()
}

func (r *repl) cmdHelp(_ []string) {
	fmt.Print(helpText())
	fmt.Println()
}
