package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/orchestrator"
)

var (
	agentHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#A78BFA"))

	agentActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2dd4bf"))

	agentInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94A3B8"))

	agentModelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#818cf8"))
)

// handleAgentCommand handles the /agent command and subcommands
func handleAgentCommand(args []string, bridge *orchestrator.TaskBridge) {
	if len(args) == 0 {
		// Show agent overview
		showAgentOverview(bridge)
		return
	}

	subCmd := strings.ToLower(args[0])
	subArgs := args[1:]

	switch subCmd {
	case "list":
		showAgentList(bridge)
	case "status":
		showAgentStatus(bridge, subArgs)
	case "config":
		handleAgentConfig(bridge, subArgs)
	case "use":
		handleAgentUse(bridge, subArgs)
	case "help":
		showAgentHelp()
	default:
		fmt.Printf("Unknown agent subcommand: %s\n", subCmd)
		fmt.Println("Use '/agent help' for available commands")
	}
}

// showAgentOverview shows a brief overview of the agent system
func showAgentOverview(bridge *orchestrator.TaskBridge) {
	if bridge == nil || !bridge.IsInitialized() {
		fmt.Println("Agent system not initialized")
		fmt.Println("Use '/agent list' to see available agents")
		return
	}

	orch := bridge.GetOrchestrator()
	if orch == nil {
		fmt.Println("Orchestrator not available")
		return
	}

	fmt.Println(agentHeaderStyle.Render("Multi-Agent System"))
	fmt.Println()

	config := orch.GetConfig()
	fmt.Printf("Status: %s\n", formatEnabled(config.Enabled))
	fmt.Printf("Routing: %s\n", config.DefaultRouting)
	fmt.Printf("Auto-diagnostics: %s\n", formatEnabled(config.AutoDiagnostics))
	fmt.Println()

	// Show agent summary
	infos := bridge.GetAgentInfos()
	activeCount := 0
	totalInvocations := 0
	for _, info := range infos {
		if info.State.Active {
			activeCount++
		}
		totalInvocations += info.State.Invocations
	}

	fmt.Printf("Agents: %d configured, %d active\n", len(infos), activeCount)
	fmt.Printf("Total invocations: %d\n", totalInvocations)
	fmt.Println()
	fmt.Println("Use '/agent list' for detailed agent information")
}

// showAgentList shows all available agents with their configuration
func showAgentList(bridge *orchestrator.TaskBridge) {
	if bridge == nil || !bridge.IsInitialized() {
		// Show default agents even without orchestrator
		showDefaultAgents()
		return
	}

	fmt.Println(agentHeaderStyle.Render("Available Agents"))
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tMODEL\tTOKENS\tSTATUS\tINVOCATIONS")
	fmt.Fprintln(w, "────\t─────\t──────\t──────\t───────────")

	infos := bridge.GetAgentInfos()
	for _, info := range infos {
		status := agentInactiveStyle.Render("idle")
		if info.State.Active {
			status = agentActiveStyle.Render("active")
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\n",
			info.DisplayName,
			agentModelStyle.Render(info.Model),
			info.State.TokensUsed,
			status,
			info.State.Invocations,
		)
	}
	w.Flush()

	fmt.Println()
	fmt.Println("Use '/agent status <type>' for detailed agent information")
}

// showDefaultAgents shows default agent configuration (when orchestrator not initialized)
func showDefaultAgents() {
	fmt.Println(agentHeaderStyle.Render("Available Agents"))
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tDESCRIPTION\tDEFAULT MODEL")
	fmt.Fprintln(w, "────\t───────────\t─────────────")

	for _, agentType := range agent.AllTypes() {
		cfg := agent.DefaultConfig(agentType)
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			agentType.DisplayName(),
			agentType.Description(),
			agentModelStyle.Render(cfg.Model),
		)
	}
	w.Flush()

	fmt.Println()
	fmt.Println("Agent system will be initialized on first task execution")
}

// showAgentStatus shows detailed status for a specific agent
func showAgentStatus(bridge *orchestrator.TaskBridge, args []string) {
	if bridge == nil || !bridge.IsInitialized() {
		fmt.Println("Agent system not initialized")
		return
	}

	orch := bridge.GetOrchestrator()
	if orch == nil {
		fmt.Println("Orchestrator not available")
		return
	}

	if len(args) == 0 {
		// Show all agent statuses
		showAllAgentStatuses(orch)
		return
	}

	// Find the specified agent
	agentTypeName := strings.ToLower(args[0])
	var targetType agent.Type

	for _, t := range agent.AllTypes() {
		if strings.ToLower(string(t)) == agentTypeName ||
			strings.ToLower(t.DisplayName()) == agentTypeName {
			targetType = t
			break
		}
	}

	if targetType == "" {
		fmt.Printf("Unknown agent type: %s\n", args[0])
		fmt.Println("Available types:", getAgentTypeNames())
		return
	}

	ag, err := orch.GetRegistry().Get(targetType)
	if err != nil {
		fmt.Printf("Error getting agent: %v\n", err)
		return
	}

	cfg := ag.Config()
	state := ag.GetState()

	fmt.Println(agentHeaderStyle.Render(targetType.DisplayName() + " Agent"))
	fmt.Println()

	fmt.Println("Configuration:")
	fmt.Printf("  Model: %s\n", agentModelStyle.Render(cfg.Model))
	fmt.Printf("  Temperature: %.2f\n", cfg.Temperature)
	fmt.Printf("  Top P: %.2f\n", cfg.TopP)
	fmt.Printf("  Num Predict: %d\n", cfg.NumPredict)
	fmt.Printf("  Max Context: %d tokens\n", cfg.MaxContextTokens)
	fmt.Printf("  Max Tool Iterations: %d\n", cfg.MaxToolIter)
	fmt.Printf("  Timeout: %ds\n", cfg.Timeout)
	if len(cfg.ToolCategories) > 0 {
		fmt.Printf("  Tool Categories: %s\n", strings.Join(cfg.ToolCategories, ", "))
	}
	fmt.Println()

	fmt.Println("State:")
	status := agentInactiveStyle.Render("idle")
	if state.Active {
		status = agentActiveStyle.Render("active")
	}
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Invocations: %d\n", state.Invocations)
	fmt.Printf("  Tokens Used: %d\n", state.TokensUsed)
	fmt.Printf("  Errors: %d\n", state.ErrorCount)
	if !state.LastActivity.IsZero() {
		fmt.Printf("  Last Activity: %s\n", state.LastActivity.Format("15:04:05"))
	}
}

// showAllAgentStatuses shows status summary for all agents
func showAllAgentStatuses(orch *orchestrator.Orchestrator) {
	fmt.Println(agentHeaderStyle.Render("Agent Status Overview"))
	fmt.Println()

	states := orch.GetAgentStates()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tSTATUS\tINVOKES\tTOKENS\tERRORS\tLAST ACTIVE")
	fmt.Fprintln(w, "─────\t──────\t───────\t──────\t──────\t───────────")

	for _, agentType := range agent.AllTypes() {
		state, ok := states[agentType]
		if !ok {
			continue
		}

		status := agentInactiveStyle.Render("idle")
		if state.Active {
			status = agentActiveStyle.Render("active")
		}

		lastActive := "-"
		if !state.LastActivity.IsZero() {
			lastActive = state.LastActivity.Format("15:04:05")
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\n",
			agentType.DisplayName(),
			status,
			state.Invocations,
			state.TokensUsed,
			state.ErrorCount,
			lastActive,
		)
	}
	w.Flush()
}

// handleAgentConfig handles agent configuration commands
func handleAgentConfig(bridge *orchestrator.TaskBridge, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage:")
		fmt.Println("  /agent config <agent_type>  - Show configuration for an agent")
		fmt.Println("  /agent config example       - Show example agents.yaml content")
		fmt.Println("  /agent config init          - Create .taracode/agents.yaml template")
		fmt.Println()
		fmt.Println("Available agent types:", getAgentTypeNames())
		return
	}

	subCmd := strings.ToLower(args[0])

	// Handle special subcommands
	switch subCmd {
	case "example":
		fmt.Println(agent.GenerateExampleConfig())
		return

	case "init":
		handleAgentConfigInit()
		return
	}

	// Try to find agent type
	var targetType agent.Type
	for _, t := range agent.AllTypes() {
		if strings.ToLower(string(t)) == subCmd ||
			strings.ToLower(t.DisplayName()) == subCmd {
			targetType = t
			break
		}
	}

	if targetType == "" {
		fmt.Printf("Unknown agent type: %s\n", args[0])
		fmt.Println("Available types:", getAgentTypeNames())
		return
	}

	// Show current config for the agent
	var cfg agent.Config
	if bridge != nil && bridge.IsInitialized() {
		orch := bridge.GetOrchestrator()
		if orch != nil {
			if ag, err := orch.GetRegistry().Get(targetType); err == nil {
				cfg = ag.Config()
			}
		}
	}
	if cfg.Model == "" {
		cfg = agent.DefaultConfig(targetType)
	}

	fmt.Println(agentHeaderStyle.Render(targetType.DisplayName() + " Agent Configuration"))
	fmt.Println()
	fmt.Printf("  Model:              %s\n", agentModelStyle.Render(cfg.Model))
	fmt.Printf("  Temperature:        %.2f\n", cfg.Temperature)
	fmt.Printf("  Top P:              %.2f\n", cfg.TopP)
	if cfg.NumPredict > 0 {
		fmt.Printf("  Num Predict:        %d\n", cfg.NumPredict)
	} else {
		fmt.Printf("  Num Predict:        0 (model default)\n")
	}
	fmt.Printf("  Max Context Tokens: %d\n", cfg.MaxContextTokens)
	fmt.Printf("  Max Tool Iterations: %d\n", cfg.MaxToolIter)
	fmt.Printf("  Timeout:            %ds\n", cfg.Timeout)

	if len(cfg.ToolCategories) > 0 {
		fmt.Printf("  Tool Categories:    %s\n", strings.Join(cfg.ToolCategories, ", "))
	}

	fmt.Println()
	fmt.Println("To modify, edit ~/.taracode/config.yaml or .taracode/agents.yaml")
}

// handleAgentConfigInit creates an example agents.yaml file
func handleAgentConfigInit() {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	agentsFile := filepath.Join(wd, ".taracode", "agents.yaml")

	// Check if file already exists
	if _, err := os.Stat(agentsFile); err == nil {
		fmt.Printf("File already exists: %s\n", agentsFile)
		fmt.Println("Use '/agent config example' to see the example content")
		return
	}

	// Create directory if needed
	if err := os.MkdirAll(filepath.Join(wd, ".taracode"), 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		return
	}

	// Write example config
	content := agent.GenerateExampleConfig()
	if err := os.WriteFile(agentsFile, []byte(content), 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	fmt.Printf("Created: %s\n", agentsFile)
	fmt.Println("Edit this file to customize agent behavior for this project")
}

// handleAgentUse handles manual agent selection
func handleAgentUse(bridge *orchestrator.TaskBridge, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: /agent use <agent_type>")
		fmt.Println("Available agents:", getAgentTypeNames())
		return
	}

	agentTypeName := strings.ToLower(args[0])
	var targetType agent.Type

	for _, t := range agent.AllTypes() {
		if strings.ToLower(string(t)) == agentTypeName ||
			strings.ToLower(t.DisplayName()) == agentTypeName {
			targetType = t
			break
		}
	}

	if targetType == "" {
		fmt.Printf("Unknown agent type: %s\n", args[0])
		fmt.Println("Available agents:", getAgentTypeNames())
		return
	}

	fmt.Printf("Next prompt will be routed to %s agent\n", targetType.DisplayName())
	fmt.Println("(Manual agent routing not yet fully implemented)")
}

// showAgentHelp shows help for agent commands
func showAgentHelp() {
	fmt.Println(agentHeaderStyle.Render("Agent Commands"))
	fmt.Println()
	fmt.Println("  /agent                      - Show agent system overview")
	fmt.Println("  /agent list                 - List all available agents")
	fmt.Println("  /agent status               - Show status of all agents")
	fmt.Println("  /agent status <type>        - Show detailed status for an agent")
	fmt.Println("  /agent config <type>        - Show/set agent configuration")
	fmt.Println("  /agent use <type>           - Route next prompt to specific agent")
	fmt.Println("  /agent help                 - Show this help")
	fmt.Println()
	fmt.Println("Agent Types:")
	for _, t := range agent.AllTypes() {
		fmt.Printf("  %-12s - %s\n", t.DisplayName(), t.Description())
	}
}

// formatEnabled returns a colored enabled/disabled string
func formatEnabled(enabled bool) string {
	if enabled {
		return agentActiveStyle.Render("enabled")
	}
	return agentInactiveStyle.Render("disabled")
}

// getAgentTypeNames returns a comma-separated list of agent type names
func getAgentTypeNames() string {
	var names []string
	for _, t := range agent.AllTypes() {
		names = append(names, string(t))
	}
	return strings.Join(names, ", ")
}
