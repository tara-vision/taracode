package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/manifoldco/promptui"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/mcp"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/orchestrator"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
	"github.com/tara-vision/taracode/internal/upgrade"
	"github.com/tara-vision/taracode/internal/watch"
)

func startREPL() {
	// Check for multi-host configuration first (v2.0)
	hostsCfg := GetHostsConfig()
	useMultiHost := !hostsCfg.IsEmpty() && len(hostsCfg.Hosts) > 1

	// Get configuration from config or environment
	host := viper.GetString("host")
	apiKey := viper.GetString("key")
	model := viper.GetString("model")
	vendor := viper.GetString("vendor")

	// For multi-host mode, use default host settings if not overridden
	if useMultiHost {
		if defaultHost, ok := hostsCfg.GetDefaultHost(); ok {
			// Use host URL if not set via legacy config
			if host == "" {
				host = defaultHost.URL
			}
			// Use host API key if not set
			if apiKey == "" && defaultHost.APIKey != "" {
				apiKey = defaultHost.APIKey
			}
			// Use host vendor if not set
			if vendor == "" && defaultHost.Vendor != "" {
				vendor = defaultHost.Vendor
			}
			// Use first model from host's models list if not set
			if model == "" && len(defaultHost.Models) > 0 {
				model = defaultHost.Models[0]
			}
		}
	}

	// Validate host configuration
	if host == "" {
		fmt.Fprintln(os.Stderr, "Error: LLM server host not found.")
		fmt.Fprintln(os.Stderr, "Set it via:")
		fmt.Fprintln(os.Stderr, "  - Environment variable: export TARACODE_HOST=http://ollama.tara.lab")
		fmt.Fprintln(os.Stderr, "  - Config file: ~/.taracode/config.yaml")
		fmt.Fprintln(os.Stderr, "  - Command flag: --host http://ollama.tara.lab")
		fmt.Fprintln(os.Stderr, "  - Multi-host config: hosts: section in config.yaml")
		os.Exit(1)
	}

	// Streaming is enabled by default (--no-stream to disable)
	streaming := !viper.GetBool("no_stream")

	// Spinner is enabled by default (--no-spinner to disable)
	enableSpinner := !viper.GetBool("no_spinner")

	// Get working directory
	workingDir, _ := os.Getwd()

	// Sandbox directory tracking
	projectRoot := workingDir   // Fixed at startup, becomes sandbox root after init
	currentRelDir := ""         // Relative path from project root (empty = at root)
	currentAbsDir := workingDir // Current absolute path for file operations

	// Create renderer for styled output
	renderer := ui.NewRenderer()

	// Check for project context
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	projectLoaded := false
	isProjectInitialized := false
	if _, err := os.Stat(taracodeFile); err == nil {
		projectLoaded = true
		isProjectInitialized = isInitializedProject(workingDir)
	}

	// Initialize the assistant
	asst, err := assistant.New(host, apiKey, model, vendor, streaming, enableSpinner)
	if err != nil {
		// Use enhanced error message for connection errors
		fmt.Fprintln(os.Stderr, ui.FormatConnectionError(host, err))
		os.Exit(1)
	}

	// Mode initialization is done after auth validation to check plan access

	// Show provider info
	if providerInfo := asst.GetProviderInfo(); providerInfo != nil {
		fmt.Print(renderer.ProviderMessage(providerInfo))
	}

	// Show session info
	if session := asst.GetSession(); session != nil && len(session.Messages) > 0 {
		fmt.Print(renderer.SessionResumeMessage(len(session.Messages)))
	}
	fmt.Println()

	// Set initial mode from config/flag
	if initialMode := viper.GetString("mode"); initialMode != "" {
		if err := asst.SetMode(initialMode); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v, using default mode\n", err)
		}
	}

	// Initialize search orchestrator with config
	initSearchOrchestrator(renderer)

	// Initialize command output streaming (enabled by default)
	initCommandStreaming()

	// Print welcome message (with mode)
	fmt.Print(renderer.WelcomeMessage(asst.GetMode()))
	fmt.Print(renderer.ProjectContextMessage(projectLoaded))

	// Show init prompt if project not initialized
	if !isProjectInitialized {
		fmt.Println()
		fmt.Println("\033[33m┌─────────────────────────────────────────────────────┐\033[0m")
		fmt.Println("\033[33m│  Project Not Initialized                            │\033[0m")
		fmt.Println("\033[33m└─────────────────────────────────────────────────────┘\033[0m")
		fmt.Println()
		fmt.Println("  Run /init to initialize and enable:")
		fmt.Println("    - @ file references with Tab completion")
		fmt.Println("    - Project context awareness")
		fmt.Println("    - Session persistence")
		fmt.Println("    - Directory navigation (cd)")
		fmt.Println()
		fmt.Println("  Available commands: /init, /help, exit")
		fmt.Println()
	}

	// Initialize history manager for operation tracking
	var historyManager *history.Manager
	if isProjectInitialized {
		taracodeDir := filepath.Join(projectRoot, ".taracode")
		session := asst.GetSession()
		sessionID := "default"
		if session != nil {
			sessionID = session.ID
		}
		hm, err := history.NewManager(taracodeDir, sessionID)
		if err == nil {
			historyManager = hm
			// Set history manager on tool registry for automatic tracking
			if registry := asst.GetToolRegistry(); registry != nil {
				registry.SetHistoryManager(hm)
			}
		}
	}

	// Initialize memory manager for project memories
	var memoryManager *memory.Manager
	if isProjectInitialized {
		taracodeDir := filepath.Join(projectRoot, ".taracode")
		mm, err := memory.NewManager(taracodeDir)
		if err == nil {
			memoryManager = mm
			// Show memory count on startup for returning users
			if count := mm.Count(); count > 0 {
				fmt.Printf("%s Loaded %d project memories\n", ui.SuccessStyle.Render(ui.IconSuccess), count)
			}
		}
	}

	// Start async version check (non-blocking)
	updateResultChan := make(chan *upgrade.CheckResult, 1)
	if viper.GetBool("upgrade.auto_check") {
		CheckForUpdateAsync(Version, updateResultChan)
	} else {
		close(updateResultChan)
	}

	// Initialize MCP manager if enabled
	var mcpManager *mcp.Manager
	mcpConfig := GetMCPConfig()
	if mcpConfig.Enabled && len(mcpConfig.Servers) > 0 {
		mcpManager = mcp.NewManager(mcpConfig)
		// Set callback for tool discovery
		mcpManager.SetToolDiscoveryCallback(func(serverName string, mcpTools []mcp.MCPTool) {
			// Register MCP tools with the tool registry and assistant
			if registry := asst.GetToolRegistry(); registry != nil {
				for _, tool := range mcpTools {
					executor := mcp.CreateExecutor(mcpManager, tool)
					registry.RegisterMCPTool(tool.Name, tool.ServerName, executor)
					// Also add tool definition for LLM function calling
					asst.AddMCPToolDefinition(mcp.ToOpenAITool(tool))
				}
			}
		})
		// Auto-connect to servers with auto_connect: true
		if isProjectInitialized {
			mcpManager.AutoConnect(context.Background())
		}
	}

	// Initialize watch monitor for screen monitoring (nil until /watch start)
	var watchMonitor *watch.WatchMonitor

	// Initialize HostPool for multi-host support (v2.0)
	var hostPool *provider.HostPool
	if useMultiHost {
		hostPool = provider.NewHostPool(hostsCfg)
		connectCtx, connectCancel := context.WithTimeout(context.Background(), 60*time.Second)
		if err := hostPool.ConnectAll(connectCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: some hosts failed to connect: %v\n", err)
		}
		connectCancel()

		// Start background health checks
		hostPool.StartHealthChecks(context.Background())

		// Show multi-host status
		fmt.Printf("%s Multi-host mode: %d/%d hosts connected\n",
			ui.SuccessStyle.Render(ui.IconSuccess),
			hostPool.HealthyCount(),
			hostPool.HostCount())

		// Wire HostPool to Assistant for automatic fallback (v2.0)
		asst.SetHostPool(hostPool)
	}

	// Initialize TaskBridge for multi-agent orchestration
	var taskBridge *orchestrator.TaskBridge
	if isProjectInitialized {
		taracodeDir := filepath.Join(projectRoot, ".taracode")
		taskMgr, err := storage.NewTaskManager(taracodeDir)
		if err == nil {
			// Create TaskBridge with appropriate provider source
			if hostPool != nil {
				// Use multi-host pool for per-agent host assignment
				taskBridge = orchestrator.NewTaskBridgeFromHostPool(
					hostPool,
					asst.GetToolRegistry(),
					taskMgr,
					currentAbsDir,
				)
			} else {
				// Use single provider (legacy mode)
				taskBridge = orchestrator.NewTaskBridgeFromProvider(
					asst.GetProvider(),
					asst.GetToolRegistry(),
					taskMgr,
					currentAbsDir,
					host,
					apiKey,
				)
			}
			// Load agent configuration from global config and project overrides
			agentsCfg := agent.LoadAgentsConfig(projectRoot)
			if err := taskBridge.InitializeWithConfig(agentsCfg); err != nil {
				// Log but don't fail - agents will use default config
				fmt.Fprintf(os.Stderr, "Warning: failed to initialize agents with config: %v\n", err)
			}
		}
	}

	// Setup readline for interactive input with slash command and @ file completion
	slashCompleter := NewSlashCompleter(currentAbsDir)
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          FormatPrompt(currentRelDir),
		HistoryFile:     os.Getenv("HOME") + "/.taracode/history",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    slashCompleter,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up readline: %v\n", err)
		os.Exit(1)
	}
	defer rl.Close()

	// Check for update result (non-blocking)
	select {
	case updateResult := <-updateResultChan:
		ShowUpdateBanner(updateResult)
	default:
		// No result yet, continue without blocking
	}

	// Main REPL loop
	for {
		line, err := rl.Readline()
		if err != nil { // io.EOF or Ctrl+C
			// Stop watch monitor if running
			if watchMonitor != nil && watchMonitor.IsRunning() {
				watchMonitor.Stop()
			}
			// Stop host health checks if running
			if hostPool != nil {
				hostPool.Close()
			}
			// Generate summary on exit if session has messages
			handleExitWithSummary(asst)
			break
		}

		// Clear any previous suggestion since user is typing
		slashCompleter.ClearSuggestion()

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle exit commands (always allowed)
		if line == "exit" || line == "quit" {
			// Stop watch monitor if running
			if watchMonitor != nil && watchMonitor.IsRunning() {
				watchMonitor.Stop()
			}
			// Stop host health checks if running
			if hostPool != nil {
				hostPool.Close()
			}
			handleExitWithSummary(asst)
			break
		}

		// Handle cd and pwd commands (require init)
		if strings.HasPrefix(line, "cd") && (line == "cd" || line[2] == ' ') {
			if !isProjectInitialized {
				fmt.Println("Project not initialized. Run /init first to enable cd.")
				fmt.Println()
				continue
			}
			target := ""
			if len(line) > 3 {
				target = strings.TrimSpace(line[3:])
			}
			newRel, newAbs, err := SandboxedPath(target, currentRelDir, projectRoot)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				fmt.Println()
				continue
			}
			currentRelDir = newRel
			currentAbsDir = newAbs
			slashCompleter.UpdateWorkingDir(currentAbsDir)
			if taskBridge != nil {
				taskBridge.SetWorkingDir(currentAbsDir)
			}
			rl.SetPrompt(FormatPrompt(currentRelDir))
			if currentRelDir == "" {
				fmt.Println("Changed to project root")
			} else {
				fmt.Printf("Changed to: %s\n", currentRelDir)
			}
			fmt.Println()
			continue
		}

		if line == "pwd" {
			if !isProjectInitialized {
				fmt.Println("Project not initialized. Run /init first to enable pwd.")
				fmt.Println()
				continue
			}
			fmt.Println(FormatPwd(currentRelDir, projectRoot))
			fmt.Println()
			continue
		}

		// Enforce init for most commands (except /init, /help)
		if !isProjectInitialized {
			if strings.HasPrefix(line, "/") {
				parts := strings.Fields(line)
				cmd := parts[0]
				if cmd != "/init" && cmd != "/help" {
					fmt.Println("Project not initialized. Only /init, /help, and exit are available.")
					fmt.Println("Run /init to enable all features.")
					fmt.Println()
					continue
				}
			} else {
				// Block regular prompts before init
				fmt.Println("Project not initialized. Run /init to enable AI chat.")
				fmt.Println()
				continue
			}
		}

		// Expand @ file references before processing (with image support)
		var images []*assistant.ImageData
		if strings.Contains(line, "@") {
			if !isProjectInitialized {
				fmt.Println("💡 Tip: Run /init to enable @ file references with Tab completion")
				// Continue without expanding - treat @ as literal text
			} else {
				expanded, err := expandFileReferencesWithImages(line, projectRoot, currentAbsDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}
				line = expanded.Text
				images = expanded.Images
			}
		}

		// Handle built-in commands
		if strings.HasPrefix(line, "/") {
			// Special handling for /init - update state after success
			if strings.HasPrefix(line, "/init") {
				handleCommand(line, currentAbsDir, &asst, host, apiKey, model, vendor, streaming, enableSpinner, historyManager, mcpManager, memoryManager, taskBridge, &watchMonitor)
				// Check if init succeeded
				if isInitializedProject(projectRoot) {
					isProjectInitialized = true
					taracodeDir := filepath.Join(projectRoot, ".taracode")
					// Initialize memory manager if not already done
					if memoryManager == nil {
						mm, err := memory.NewManager(taracodeDir)
						if err == nil {
							memoryManager = mm
						}
					}
					// Initialize history manager if not already done
					if historyManager == nil {
						session := asst.GetSession()
						sessionID := "default"
						if session != nil {
							sessionID = session.ID
						}
						hm, err := history.NewManager(taracodeDir, sessionID)
						if err == nil {
							historyManager = hm
							// Set history manager on tool registry for automatic tracking
							if registry := asst.GetToolRegistry(); registry != nil {
								registry.SetHistoryManager(hm)
							}
						}
					}
					// Auto-connect MCP servers after init
					if mcpManager != nil {
						mcpManager.AutoConnect(context.Background())
					}
					// Initialize TaskBridge if not already done
					if taskBridge == nil {
						taskMgr, err := storage.NewTaskManager(taracodeDir)
						if err == nil {
							if hostPool != nil {
								// Use multi-host pool for per-agent host assignment
								taskBridge = orchestrator.NewTaskBridgeFromHostPool(
									hostPool,
									asst.GetToolRegistry(),
									taskMgr,
									currentAbsDir,
								)
							} else {
								// Use single provider (legacy mode)
								taskBridge = orchestrator.NewTaskBridgeFromProvider(
									asst.GetProvider(),
									asst.GetToolRegistry(),
									taskMgr,
									currentAbsDir,
									host,
									apiKey,
								)
							}
							// Load agent configuration
							agentsCfg := agent.LoadAgentsConfig(projectRoot)
							_ = taskBridge.InitializeWithConfig(agentsCfg)
						}
					}
				}
				continue
			}
			handleCommand(line, currentAbsDir, &asst, host, apiKey, model, vendor, streaming, enableSpinner, historyManager, mcpManager, memoryManager, taskBridge, &watchMonitor)
			continue
		}

		// Process the user's message (with images if any)
		if err := asst.ProcessMessageWithImages(line, images); err != nil {
			fmt.Fprintln(os.Stderr, ui.EnhanceError(err))
		}
		fmt.Println()

		// Detect and display AI suggestion (Tab to accept)
		if lastResponse := asst.GetLastResponse(); lastResponse != "" {
			if suggestion := DetectSuggestion(lastResponse); suggestion != "" {
				slashCompleter.SetSuggestion(suggestion)
				// Display hint in muted color
				fmt.Printf("\033[90m  Tab: %s\033[0m\n", suggestion)
			}
		}

		// Auto-capture: check if user message contains memory-worthy content
		if memoryManager != nil && viper.GetBool("memory.auto_capture") {
			checkAutoCapture(memoryManager, line)
		}

		// Update prompt with context budget and mode after each message
		if viper.GetBool("show_context_budget") {
			usage := asst.GetUsage()
			if usage != nil {
				maxTokens := viper.GetInt("max_context_tokens")
				securityMode := asst.GetMode() == storage.ModeSecurity
				rl.SetPrompt(FormatPromptWithMode(currentRelDir, usage.TotalTokens, maxTokens, securityMode))
			}
		}
	}
}

func handleCommand(cmd string, workingDir string, asst **assistant.Assistant, host, apiKey, model, vendor string, streaming bool, enableSpinner bool, historyManager *history.Manager, mcpManager *mcp.Manager, memoryManager *memory.Manager, taskBridge *orchestrator.TaskBridge, watchMonitor **watch.WatchMonitor) {
	// Handle commands with arguments
	parts := strings.Fields(cmd)
	baseCmd := parts[0]
	args := parts[1:]

	switch baseCmd {
	case "/init":
		if err := assistant.InitProject(workingDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		// Get current host and model from existing assistant to preserve state
		currentHost := host
		currentModel := model
		currentVendor := vendor
		if provInfo := (*asst).GetProviderInfo(); provInfo != nil {
			currentHost = provInfo.Host
			currentModel = provInfo.Model
			currentVendor = provInfo.Type.String()
		}
		// Reinitialize assistant to pick up new context
		newAsst, err := assistant.New(currentHost, apiKey, currentModel, currentVendor, streaming, enableSpinner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reinitializing assistant: %v\n", err)
			return
		}
		*asst = newAsst
		fmt.Println("Assistant reloaded with project context.")
		fmt.Println()

	case "/help":
		fmt.Println("Available commands:")
		fmt.Println()
		fmt.Println("  Project:")
		fmt.Println("    /init      - Initialize project (creates TARACODE.md and .taracode/)")
		fmt.Println("    /reload    - Reload project context from TARACODE.md")
		fmt.Println("    /status    - Show project and session status")
		fmt.Println()
		fmt.Println("  Navigation (requires /init):")
		fmt.Println("    cd <dir>  - Change directory within project")
		fmt.Println("    cd ..     - Go up one directory")
		fmt.Println("    cd        - Return to project root")
		fmt.Println("    pwd       - Show current directory")
		fmt.Println()
		fmt.Println("  Model & Mode:")
		fmt.Println("    /model        - Switch between available Ollama models")
		fmt.Println("    /mode         - Show current operating mode")
		fmt.Println("    /mode <name>  - Switch mode (devops, security)")
		fmt.Println()
		fmt.Println("  Sessions:")
		fmt.Println("    /session              - Show current session info")
		fmt.Println("    /sessions             - List all conversation sessions")
		fmt.Println("    /session new [\"name\"] - Start a new session (optionally named)")
		fmt.Println("    /session load <id>    - Load a previous session")
		fmt.Println("    /session delete <id>  - Delete a session")
		fmt.Println("    /session rename <id> <name> - Rename a session")
		fmt.Println("    /clear                - Clear current conversation (start new session)")
		fmt.Println()
		fmt.Println("  Tasks:")
		fmt.Println("    /task \"<description>\" - Plan and execute a multi-step task")
		fmt.Println("    /task list            - List all tasks")
		fmt.Println("    /task status [id]     - Show task status")
		fmt.Println("    /task resume [id]     - Resume a paused task")
		fmt.Println("    /task pause           - Pause the active task")
		fmt.Println("    /task abort           - Abort the active task")
		fmt.Println("    /task rollback [id]   - Rollback to last checkpoint")
		fmt.Println("    /task templates       - List available task templates")
		fmt.Println("    /task run <template>  - Run a task from a template")
		fmt.Println()
		fmt.Println("  Plans:")
		fmt.Println("    /plan         - Show active task plan")
		fmt.Println()
		fmt.Println("  Permissions:")
		fmt.Println("    /permissions              - Show current permission settings")
		fmt.Println("    /permissions reset        - Reset all to default")
		fmt.Println("    /permissions allow <t|c>  - Always allow tool or category")
		fmt.Println("    /permissions deny <t|c>   - Always deny tool or category")
		fmt.Println()
		fmt.Println("  Security Audit (security mode only):")
		fmt.Println("    /audit                    - View security audit log")
		fmt.Println("    /audit export json        - Export audit log to JSON file")
		fmt.Println("    /audit export html        - Export audit log to HTML report")
		fmt.Println("    /audit clear              - Clear the audit log")
		fmt.Println()
		fmt.Println("  History & Undo:")
		fmt.Println("    /history                  - Show last 20 file operations")
		fmt.Println("    /history all              - Show all operations")
		fmt.Println("    /history <n>              - Show last n operations")
		fmt.Println("    /undo                     - Undo last file modification")
		fmt.Println("    /undo <n>                 - Undo last n modifications")
		fmt.Println("    /undo --dry-run           - Preview what would be undone")
		fmt.Println("    /diff                     - Show file changes as unified diff")
		fmt.Println("    /diff export              - Export changes to .patch file")
		fmt.Println()
		fmt.Println("  MCP (Model Context Protocol):")
		fmt.Println("    /mcp                      - List configured MCP servers and status")
		fmt.Println("    /mcp connect <name>       - Connect to an MCP server")
		fmt.Println("    /mcp disconnect <name>    - Disconnect from an MCP server")
		fmt.Println("    /mcp tools                - List tools from connected servers")
		fmt.Println()
		fmt.Println("  Agents (Multi-Agent System):")
		fmt.Println("    /agent                    - Show agent system overview")
		fmt.Println("    /agent list               - List all available agents")
		fmt.Println("    /agent status             - Show status of all agents")
		fmt.Println("    /agent status <type>      - Show detailed status for an agent")
		fmt.Println("    /agent config <type>      - Show agent configuration")
		fmt.Println("    /agent use <type>         - Route next prompt to specific agent")
		fmt.Println()
		fmt.Println("  Memory (Project Knowledge):")
		fmt.Println("    /remember <text>          - Save a memory about this project")
		fmt.Println("    /memory                   - List all project memories")
		fmt.Println("    /memory search <query>    - Search memories by keyword")
		fmt.Println("    /memory delete <id>       - Delete a memory by ID")
		fmt.Println("    /memory export [file]     - Export memories to JSON file")
		fmt.Println("    /memory import <file>     - Import memories from file")
		fmt.Println("    /memory stats             - Show memory statistics")
		fmt.Println("    /memory cleanup [days]    - Remove old unused memories")
		fmt.Println("    /memory clear             - Clear all memories")
		fmt.Println()
		fmt.Println("  Watch (Screen Monitoring - macOS):")
		fmt.Println("    /watch                    - Show watch command help")
		fmt.Println("    /watch this               - Capture and analyze screens now")
		fmt.Println("    /watch start              - Start continuous monitoring")
		fmt.Println("    /watch stop               - Stop monitoring")
		fmt.Println("    /watch status             - Show monitoring state")
		fmt.Println()
		fmt.Println("  Other:")
		fmt.Println("    /context             - Show what's in the LLM context window")
		fmt.Println("    /context --agents    - Show per-agent context usage")
		fmt.Println("    /tools               - List available AI tools")
		fmt.Println("    /usage        - Show token usage statistics")
		fmt.Println("    /upgrade      - Check for and install updates")
		fmt.Println("    /help         - Show this help message")
		fmt.Println("    exit          - Exit Tara Code")
		fmt.Println()
		fmt.Println("  File References (requires /init):")
		fmt.Println("    @<Tab>   - Show file completion list")
		fmt.Println("    @path    - Include text file (e.g., @src/main.go)")
		fmt.Println("    @image   - Include image for vision (e.g., @screenshot.png)")
		fmt.Println()
		fmt.Println("  Vision Support (requires Gemma3):")
		fmt.Println("    Supported formats: .png, .jpg, .jpeg, .gif, .webp, .bmp")
		fmt.Println("    Example: What's in this image? @screenshot.png")
		fmt.Println()

	case "/tools":
		toolInfoList := tools.GetToolInfoList()

		// Group tools by category
		categories := make(map[string][]tools.ToolInfo)
		categoryOrder := []string{"file", "command", "git", "web", "utility", "kubernetes", "terraform", "docker", "cloud", "security"}
		categoryNames := map[string]string{
			"file":       "File Operations",
			"command":    "Command Execution",
			"git":        "Git",
			"web":        "Web",
			"utility":    "Utility",
			"kubernetes": "Kubernetes",
			"terraform":  "Terraform",
			"docker":     "Docker",
			"cloud":      "Cloud (AWS/Azure/GCP)",
			"security":   "Security",
		}

		for _, tool := range toolInfoList {
			categories[tool.Category] = append(categories[tool.Category], tool)
		}

		// Count MCP tools from registry
		mcpToolCount := 0
		if registry := (*asst).GetToolRegistry(); registry != nil {
			mcpToolsByServer := registry.GetMCPTools()
			for _, serverTools := range mcpToolsByServer {
				mcpToolCount += len(serverTools)
			}
		}

		totalTools := len(toolInfoList) + mcpToolCount
		fmt.Printf("Available Tools (%d total):\n\n", totalTools)

		for _, cat := range categoryOrder {
			toolList := categories[cat]
			if len(toolList) == 0 {
				continue
			}
			catName := categoryNames[cat]
			fmt.Printf("  %s:\n", catName)
			for _, t := range toolList {
				fmt.Printf("    %-20s %s\n", t.Name, t.Description)
			}
			fmt.Println()
		}

		// Show MCP tools if any are connected
		if registry := (*asst).GetToolRegistry(); registry != nil {
			mcpToolsByServer := registry.GetMCPTools()
			if len(mcpToolsByServer) > 0 {
				fmt.Printf("  MCP (Model Context Protocol):\n")
				for serverName, serverTools := range mcpToolsByServer {
					for _, toolName := range serverTools {
						fmt.Printf("    %-20s %s\n", toolName, fmt.Sprintf("[%s]", serverName))
					}
				}
				fmt.Println()
			}
		}

	case "/usage":
		usage := (*asst).GetUsage()

		// Session usage
		fmt.Println("Session Usage:")
		fmt.Printf("  Prompt tokens:     %d\n", usage.PromptTokens)
		fmt.Printf("  Completion tokens: %d\n", usage.CompletionTokens)
		fmt.Printf("  Total tokens:      %d\n", usage.TotalTokens)
		fmt.Println()

	case "/reload":
		newAsst, err := assistant.New(host, apiKey, model, vendor, streaming, enableSpinner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
			return
		}
		*asst = newAsst
		fmt.Println("Project context reloaded.")
		fmt.Println()

	case "/clear":
		if err := (*asst).NewSession(""); err != nil {
			// Fallback to creating new assistant
			newAsst, err := assistant.New(host, apiKey, model, vendor, streaming, enableSpinner)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error clearing: %v\n", err)
				return
			}
			*asst = newAsst
		}
		fmt.Println("Conversation cleared. Started new session.")
		fmt.Println()

	case "/session":
		if len(args) == 0 {
			// Show current session info
			handleSessionInfo(*asst)
		} else if args[0] == "new" {
			// Start new session with optional name
			sessionName := ""
			if len(args) > 1 {
				// Join remaining args as the session name (support quoted names)
				sessionName = strings.Join(args[1:], " ")
				// Remove surrounding quotes if present
				sessionName = strings.Trim(sessionName, "\"'")
			}
			if err := (*asst).NewSession(sessionName); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
				return
			}
			if sessionName != "" {
				fmt.Printf("Started new session: %s\n", sessionName)
			} else {
				fmt.Println("Started new conversation session.")
			}
			fmt.Println()
		} else if args[0] == "load" && len(args) > 1 {
			// Load session by ID
			sessionID := args[1]
			if err := (*asst).LoadSession(sessionID); err != nil {
				fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
				return
			}
			session := (*asst).GetSession()
			if session == nil {
				fmt.Fprintf(os.Stderr, "Error: session loaded but not accessible\n")
				return
			}
			fmt.Printf("Loaded session with %d messages.\n", len(session.Messages))
			fmt.Println()
		} else if args[0] == "delete" && len(args) > 1 {
			// Delete session by ID
			sessionID := args[1]
			handleDeleteSession(*asst, sessionID)
		} else if args[0] == "rename" && len(args) > 2 {
			// Rename session: /session rename <id> <new name>
			sessionID := args[1]
			newName := strings.Join(args[2:], " ")
			newName = strings.Trim(newName, "\"'")
			handleRenameSession(*asst, sessionID, newName)
		} else {
			fmt.Println("Usage: /session [new [\"name\"]|load <id>|delete <id>|rename <id> <name>]")
			fmt.Println()
		}

	case "/sessions":
		handleListSessions(*asst)

	case "/status":
		handleStatus(*asst, workingDir)

	case "/plan":
		handleShowPlan(*asst)

	case "/model":
		handleModelSwitch(asst, taskBridge, streaming, enableSpinner)

	case "/permissions":
		handlePermissions(*asst, args)

	case "/mode":
		if len(args) == 0 {
			// Show current mode and available modes
			currentMode := (*asst).GetMode()
			if currentMode == storage.ModeSecurity {
				fmt.Printf("Current mode: %s %s\n", ui.IconShield, currentMode)
			} else {
				fmt.Printf("Current mode: %s\n", currentMode)
			}
			fmt.Println("Available modes: devops, security")
		} else {
			newMode := args[0]
			previousMode := (*asst).GetMode()
			if err := (*asst).SetMode(newMode); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return
			}
			// Show mode activation message
			modeRenderer := ui.NewRenderer()
			targetMode := storage.OperatingMode(newMode)
			if targetMode == storage.ModeSecurity && previousMode != storage.ModeSecurity {
				fmt.Print(modeRenderer.SecurityModeActivatedMessage())
			} else if targetMode == storage.ModeDevOps && previousMode == storage.ModeSecurity {
				fmt.Print(modeRenderer.SecurityModeDeactivatedMessage())
			} else {
				fmt.Printf("Switched to %s mode.\n", newMode)
			}
		}
		fmt.Println()

	case "/audit":
		handleAudit(*asst, args)

	case "/history":
		handleHistory(historyManager, args)

	case "/undo":
		handleUndo(historyManager, args)

	case "/context":
		handleContext(*asst, memoryManager, args, taskBridge)

	case "/mcp":
		handleMCP(mcpManager, args, *asst)

	case "/task":
		handleTask(*asst, args, workingDir)

	case "/diff":
		handleDiff(historyManager, args, workingDir)

	case "/remember":
		handleRemember(memoryManager, args, asst)

	case "/memory":
		handleMemory(memoryManager, args)

	case "/agent":
		// Agent system commands
		handleAgentCommand(args, taskBridge)

	case "/watch":
		// Screen monitoring and analysis
		handleWatchCommand(args, *asst, watchMonitor, os.TempDir())

	case "/upgrade":
		// Check for and install updates
		handleUpgradeCommand(args)

	case "/hosts":
		// Multi-host status and management (v2.0)
		handleHostsCommand(args, taskBridge)

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		fmt.Println("Type '/help' for available commands.")
		fmt.Println()
	}
}

// handleAudit handles the /audit command for viewing and exporting security audit logs
func handleAudit(asst *assistant.Assistant, args []string) {
	// Check if in security mode
	if asst.GetMode() != storage.ModeSecurity {
		fmt.Println("Audit log is only available in security mode.")
		fmt.Println("Switch to security mode with: /mode security")
		fmt.Println()
		return
	}

	session := asst.GetSessionFresh()
	if session == nil {
		fmt.Println("No active session.")
		fmt.Println()
		return
	}

	auditLog := session.AuditLog
	hasEntries := auditLog != nil && len(auditLog.Entries) > 0

	// Handle subcommands first (some work even without entries)
	if len(args) > 0 {
		switch args[0] {
		case "export":
			if len(args) < 2 {
				fmt.Println("Usage: /audit export <json|html>")
				fmt.Println()
				return
			}
			format := args[1]
			if format != "json" && format != "html" {
				fmt.Printf("Unknown export format: %s\n", format)
				fmt.Println("Supported formats: json, html")
				fmt.Println()
				return
			}
			// Check for entries before export
			if !hasEntries {
				fmt.Println("No audit entries to export.")
				fmt.Println("Audit entries are created when you allow or deny tool operations.")
				fmt.Println()
				return
			}
			if format == "json" {
				exportAuditJSON(session, auditLog)
			} else {
				exportAuditHTML(session, auditLog)
			}
		case "clear":
			if !hasEntries {
				fmt.Println("Audit log is already empty.")
				fmt.Println()
				return
			}
			if err := asst.ClearAuditLog(); err != nil {
				fmt.Fprintf(os.Stderr, "Error clearing audit log: %v\n", err)
				return
			}
			fmt.Println("Audit log cleared.")
			fmt.Println()
		default:
			fmt.Printf("Unknown audit subcommand: %s\n", args[0])
			fmt.Println("Usage: /audit [export <json|html>|clear]")
			fmt.Println()
		}
		return
	}

	// No args - display audit log
	if !hasEntries {
		fmt.Println("No audit entries recorded in this session.")
		fmt.Println("Audit entries are created when you allow or deny tool operations.")
		fmt.Println()
		return
	}

	displayAuditLog(auditLog)
}

// displayAuditLog shows the audit log in a formatted table
func displayAuditLog(log *storage.AuditLog) {
	fmt.Println()
	fmt.Printf("%s Security Audit Log\n", ui.IconShield)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Total entries: %d (Allowed: %d, Denied: %d)\n", len(log.Entries), log.TotalAllow, log.TotalDeny)
	fmt.Println()

	// Display entries in reverse chronological order (most recent first)
	for i := len(log.Entries) - 1; i >= 0; i-- {
		entry := log.Entries[i]
		timestamp := entry.Timestamp.Format("15:04:05")

		// Action indicator with color
		var actionSymbol string
		switch entry.Action {
		case storage.AuditActionAllow:
			actionSymbol = fmt.Sprintf("\033[32m%s ALLOW\033[0m", ui.IconSuccess)
		case storage.AuditActionAllowAll:
			actionSymbol = fmt.Sprintf("\033[32m%s ALLOW ALL\033[0m", ui.IconSuccess)
		case storage.AuditActionDeny:
			actionSymbol = fmt.Sprintf("\033[31m%s DENY\033[0m", ui.IconError)
		case storage.AuditActionDenyAll:
			actionSymbol = fmt.Sprintf("\033[31m%s DENY ALL\033[0m", ui.IconError)
		default:
			actionSymbol = string(entry.Action)
		}

		// Category with color
		var categoryColor string
		switch entry.Category {
		case "destructive":
			categoryColor = "\033[31m" // Red
		case "execute":
			categoryColor = "\033[33m" // Yellow
		case "write", "git":
			categoryColor = "\033[36m" // Cyan
		default:
			categoryColor = "\033[0m"
		}

		fmt.Printf("[%s] %s %s%s\033[0m: %s\n", timestamp, actionSymbol, categoryColor, entry.Category, entry.ToolName)
		if entry.Target != "" {
			targetDisplay := entry.Target
			if len(targetDisplay) > 50 {
				targetDisplay = targetDisplay[:47] + "..."
			}
			fmt.Printf("         └─ %s\n", targetDisplay)
		}
	}
	fmt.Println()
}

// handleHistory displays the operation history
func handleHistory(hm *history.Manager, args []string) {
	if hm == nil {
		fmt.Println("History tracking not available.")
		fmt.Println("Run /init to initialize project and enable history.")
		fmt.Println()
		return
	}

	// Parse limit argument
	limit := 20 // default
	showAll := false

	if len(args) > 0 {
		if args[0] == "all" {
			showAll = true
		} else if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}

	var ops []history.Operation
	if showAll {
		ops = hm.GetAllHistory()
	} else {
		ops = hm.GetHistory(limit)
	}

	if len(ops) == 0 {
		fmt.Println("No operations recorded in this session.")
		fmt.Println("File modifications will be tracked automatically.")
		fmt.Println()
		return
	}

	// Get stats
	stats := hm.GetStats()

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Operation History                                                  │")
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")

	for _, op := range ops {
		status := "✓"
		if !op.Success {
			status = "✗"
		}
		if op.Undone {
			status = "↩"
		}

		// Format timestamp
		timeStr := op.Timestamp.Format("15:04:05")

		// Format target (truncate if long)
		target := op.Target
		if len(target) > 35 {
			target = "..." + target[len(target)-32:]
		}

		// Format the operation line with proper padding
		opLine := fmt.Sprintf("#%-3d %s %-14s %-35s %s", op.ID, status, op.Tool, target, timeStr)
		fmt.Println(formatBoxLine(opLine))

		// Show backup info if available
		if op.BackupPath != "" && !op.Undone {
			backupLine := fmt.Sprintf("     └─ backup: %s", filepath.Base(op.BackupPath))
			fmt.Println(formatBoxLine(backupLine))
		}
	}

	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	statsLine := fmt.Sprintf("Total: %d  |  Undoable: %d  |  Undone: %d", stats["total"], stats["undoable"], stats["undone"])
	fmt.Println(formatBoxLine(statsLine))
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	if stats["undoable"] > 0 {
		fmt.Println("Use /undo to revert the last file modification.")
		fmt.Println()
	}
}

// handleUndo reverts file operations
func handleUndo(hm *history.Manager, args []string) {
	if hm == nil {
		fmt.Println("History tracking not available.")
		fmt.Println("Run /init to initialize project and enable undo.")
		fmt.Println()
		return
	}

	// Parse arguments
	count := 1
	dryRun := false

	for _, arg := range args {
		if arg == "--dry-run" {
			dryRun = true
		} else if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			count = n
		}
	}

	if dryRun {
		// Preview what would be undone
		results, err := hm.UndoDryRun(count)
		if err != nil {
			fmt.Printf("Nothing to undo: %v\n", err)
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Println("Dry run - would undo the following operations:")
		fmt.Println()
		for _, r := range results {
			status := "✓"
			if !r.Success {
				status = "✗"
			}
			fmt.Printf("  %s #%d %s: %s\n", status, r.OperationID, r.Tool, r.Message)
		}
		fmt.Println()
		fmt.Println("Run /undo without --dry-run to apply changes.")
		fmt.Println()
		return
	}

	// Perform actual undo
	if count == 1 {
		result, err := hm.Undo()
		if err != nil {
			fmt.Printf("Cannot undo: %v\n", err)
			fmt.Println()
			return
		}

		if result.Success {
			fmt.Printf("✓ Reverted %s on %s\n", result.Tool, filepath.Base(result.Target))
			if result.RestoredFrom != "" {
				fmt.Printf("  Restored from: %s\n", filepath.Base(result.RestoredFrom))
			}
		} else {
			fmt.Printf("✗ Failed to undo: %s\n", result.Message)
		}
	} else {
		results, err := hm.UndoN(count)
		if err != nil {
			fmt.Printf("Cannot undo: %v\n", err)
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Printf("Undoing %d operations:\n", len(results))
		fmt.Println()
		for _, r := range results {
			if r.Success {
				fmt.Printf("  ✓ Reverted %s on %s\n", r.Tool, filepath.Base(r.Target))
			} else {
				fmt.Printf("  ✗ Failed: %s - %s\n", r.Tool, r.Message)
			}
		}
	}
	fmt.Println()
}

// handleContext displays what's in the LLM context window
// boxWidth is the content width for /context and /history boxes (excluding borders)
const boxWidth = 67

// formatBoxLine formats a line to fit within the box, with proper padding
func formatBoxLine(content string) string {
	if len(content) > boxWidth {
		content = content[:boxWidth-3] + "..."
	}
	return fmt.Sprintf("│  %-*s│", boxWidth, content)
}

func handleContext(asst *assistant.Assistant, mm *memory.Manager, args []string, taskBridge *orchestrator.TaskBridge) {
	// Check for --agents flag
	showAgents := false
	for _, arg := range args {
		if arg == "--agents" || arg == "-a" {
			showAgents = true
			break
		}
	}

	if showAgents {
		handleContextAgents(taskBridge)
		return
	}

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Context Window Contents                                            │")
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")

	// Session info
	session := asst.GetSession()
	if session != nil {
		// Count user/assistant messages
		userMsgs := 0
		assistMsgs := 0
		toolCalls := 0
		for _, msg := range session.Messages {
			switch msg.Role {
			case "user":
				userMsgs++
			case "assistant":
				assistMsgs++
			}
			toolCalls += len(msg.ToolCalls)
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("Session: %s", ui.TruncateID(session.ID, 0))))
		fmt.Println(formatBoxLine(fmt.Sprintf("  Messages: %d user, %d assistant", userMsgs, assistMsgs)))
		if toolCalls > 0 {
			fmt.Println(formatBoxLine(fmt.Sprintf("  Tool calls: %d", toolCalls)))
		}
	} else {
		fmt.Println(formatBoxLine("Session: None"))
	}

	// Token usage
	usage := asst.GetUsage()
	if usage != nil && usage.TotalTokens > 0 {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine(fmt.Sprintf("Tokens Used: %d (prompt: %d, completion: %d)",
			usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)))
	}

	// Project context
	projectCtx := asst.GetProjectContext()
	if projectCtx != nil {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine("Project Context (from TARACODE.md)"))
		fmt.Println(formatBoxLine(fmt.Sprintf("  Type: %s", projectCtx.ProjectType)))
		if projectCtx.ModuleName != "" {
			moduleName := projectCtx.ModuleName
			if len(moduleName) > 50 {
				moduleName = moduleName[:47] + "..."
			}
			fmt.Println(formatBoxLine(fmt.Sprintf("  Module: %s", moduleName)))
		}
		if len(projectCtx.ImportantFiles) > 0 {
			fmt.Println(formatBoxLine(fmt.Sprintf("  Important files: %d", len(projectCtx.ImportantFiles))))
		}
		if len(projectCtx.DetectedTools) > 0 {
			toolList := strings.Join(projectCtx.DetectedTools, ", ")
			if len(toolList) > 45 {
				toolList = toolList[:42] + "..."
			}
			fmt.Println(formatBoxLine(fmt.Sprintf("  Detected tools: %s", toolList)))
		}
	}

	// Project memories
	if mm != nil {
		memCount := mm.Count()
		if memCount > 0 {
			maxTokens := viper.GetInt("memory.max_context_tokens")
			if maxTokens <= 0 {
				maxTokens = 2000
			}
			relevantMems := mm.GetRelevantMemories("", maxTokens)
			fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
			fmt.Println(formatBoxLine(fmt.Sprintf("Project Memories: %d total, %d in context", memCount, len(relevantMems))))
			if len(relevantMems) > 0 {
				// Show first 3 memories
				showCount := len(relevantMems)
				if showCount > 3 {
					showCount = 3
				}
				for i := 0; i < showCount; i++ {
					mem := relevantMems[i]
					preview := mem.Content
					if len(preview) > 50 {
						preview = preview[:47] + "..."
					}
					fmt.Println(formatBoxLine(fmt.Sprintf("  [%s] %s", mem.Category, preview)))
				}
				if len(relevantMems) > 3 {
					fmt.Println(formatBoxLine(fmt.Sprintf("  ... and %d more", len(relevantMems)-3)))
				}
			}
		}
	}

	// Files read in this session (extract from tool calls)
	if session != nil && len(session.Messages) > 0 {
		filesRead := extractFilesRead(session.Messages)
		if len(filesRead) > 0 {
			fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
			fmt.Println(formatBoxLine("Files Read This Session"))
			// Show up to 10 most recent
			displayCount := len(filesRead)
			if displayCount > 10 {
				displayCount = 10
			}
			for i := 0; i < displayCount; i++ {
				file := filesRead[i]
				if len(file) > 60 {
					file = "..." + file[len(file)-57:]
				}
				fmt.Println(formatBoxLine(fmt.Sprintf("  %s", file)))
			}
			if len(filesRead) > 10 {
				fmt.Println(formatBoxLine(fmt.Sprintf("  ... and %d more", len(filesRead)-10)))
			}
		}
	}

	// Operating mode
	mode := asst.GetMode()
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	if mode == storage.ModeSecurity {
		fmt.Println(formatBoxLine(fmt.Sprintf("Mode: %s security (audit-first)", ui.IconShield)))
	} else {
		fmt.Println(formatBoxLine(fmt.Sprintf("Mode: %s (%d DevOps tools)", mode, tools.GetToolCount())))
	}

	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// handleContextAgents displays per-agent context usage
func handleContextAgents(taskBridge *orchestrator.TaskBridge) {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Agent Context Budget                                              │")
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")

	if taskBridge == nil || !taskBridge.IsInitialized() {
		fmt.Println(formatBoxLine("Agent system not initialized"))
		fmt.Println(formatBoxLine("Run a /task command to initialize agents"))
		fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
		fmt.Println()
		return
	}

	orch := taskBridge.GetOrchestrator()
	if orch == nil {
		fmt.Println(formatBoxLine("Orchestrator not available"))
		fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
		fmt.Println()
		return
	}

	// Get agent context usage
	usages := orch.GetAgentContextUsage()

	// Calculate total
	totalUsed := 0
	totalBudget := 0
	for _, u := range usages {
		totalUsed += u.TokensUsed
		totalBudget += u.TokensBudget
	}

	// Show total
	totalPct := 0.0
	if totalBudget > 0 {
		totalPct = float64(totalUsed) / float64(totalBudget) * 100
	}
	fmt.Println(formatBoxLine(fmt.Sprintf("Total: %d / %d tokens (%.1f%%)", totalUsed, totalBudget, totalPct)))
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine("Agent Allocations:"))

	// Show per-agent usage
	for _, u := range usages {
		pct := 0.0
		if u.TokensBudget > 0 {
			pct = float64(u.TokensUsed) / float64(u.TokensBudget) * 100
		}

		// Create a progress bar
		barWidth := 16
		filledWidth := int(pct / 100 * float64(barWidth))
		if filledWidth > barWidth {
			filledWidth = barWidth
		}
		bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)

		line := fmt.Sprintf("  %-12s %5d / %5d (%5.1f%%) %s",
			u.AgentType.DisplayName()+":",
			u.TokensUsed,
			u.TokensBudget,
			pct,
			bar,
		)
		fmt.Println(formatBoxLine(line))
	}

	// Show shared context info
	state := orch.GetCurrentTaskState()
	if state != nil && state.Context != nil {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine("Shared Context:"))
		fmt.Println(formatBoxLine(fmt.Sprintf("  Items: %d", len(state.Context.Items))))
		fmt.Println(formatBoxLine(fmt.Sprintf("  Tokens: %d / %d", state.Context.TokensUsed, state.Context.TokenBudget)))
	}

	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// extractFilesRead extracts unique file paths from read_file tool calls
func extractFilesRead(messages []storage.ConversationMessage) []string {
	seen := make(map[string]bool)
	var files []string

	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.Tool == "read_file" {
				if filePath, ok := tc.Params["file_path"].(string); ok && filePath != "" {
					if !seen[filePath] {
						seen[filePath] = true
						files = append(files, filePath)
					}
				}
			}
		}
	}

	return files
}

// exportAuditJSON exports the audit log to a JSON file
func exportAuditJSON(session *storage.Session, log *storage.AuditLog) {
	// Create export structure
	export := struct {
		SessionID   string `json:"session_id"`
		SessionName string `json:"session_name"`
		ExportedAt  string `json:"exported_at"`
		Mode        string `json:"mode"`
		Summary     struct {
			TotalEntries int `json:"total_entries"`
			TotalAllow   int `json:"total_allow"`
			TotalDeny    int `json:"total_deny"`
		} `json:"summary"`
		Entries []storage.AuditEntry `json:"entries"`
	}{
		SessionID:   session.ID,
		SessionName: session.Name,
		ExportedAt:  time.Now().Format(time.RFC3339),
		Mode:        log.SessionMode,
		Entries:     log.Entries,
	}
	export.Summary.TotalEntries = len(log.Entries)
	export.Summary.TotalAllow = log.TotalAllow
	export.Summary.TotalDeny = log.TotalDeny

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling audit log: %v\n", err)
		return
	}

	// Generate filename
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("audit-%s-%s.json", ui.TruncateID(session.ID, 8), timestamp)

	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		return
	}

	fmt.Printf("%s Audit log exported to: %s\n", ui.IconSuccess, filename)
	fmt.Println()
}

// exportAuditHTML exports the audit log to an HTML report
func exportAuditHTML(session *storage.Session, log *storage.AuditLog) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("audit-%s-%s.html", ui.TruncateID(session.ID, 8), timestamp)

	var html strings.Builder

	// HTML header with embedded styles
	html.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Security Audit Report</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            max-width: 900px;
            margin: 0 auto;
            padding: 20px;
            background: #1a1a2e;
            color: #eee;
        }
        h1 {
            color: #f97316;
            border-bottom: 2px solid #f97316;
            padding-bottom: 10px;
        }
        .summary {
            background: #16213e;
            padding: 15px;
            border-radius: 8px;
            margin-bottom: 20px;
        }
        .summary-stats {
            display: flex;
            gap: 20px;
            margin-top: 10px;
        }
        .stat {
            padding: 10px 20px;
            border-radius: 4px;
            font-weight: bold;
        }
        .stat-allow { background: #065f46; }
        .stat-deny { background: #991b1b; }
        .stat-total { background: #1e40af; }
        table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 20px;
        }
        th, td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #333;
        }
        th {
            background: #16213e;
            color: #f97316;
        }
        tr:hover {
            background: #16213e;
        }
        .action-allow { color: #10b981; font-weight: bold; }
        .action-deny { color: #ef4444; font-weight: bold; }
        .category-destructive { color: #ef4444; }
        .category-execute { color: #f59e0b; }
        .category-write, .category-git { color: #06b6d4; }
        .target {
            font-family: monospace;
            font-size: 0.9em;
            color: #9ca3af;
            max-width: 300px;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
        .footer {
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #333;
            color: #6b7280;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <h1>🛡️ Security Audit Report</h1>
`)

	// Summary section
	html.WriteString(fmt.Sprintf(`    <div class="summary">
        <strong>Session:</strong> %s<br>
        <strong>Session ID:</strong> %s<br>
        <strong>Mode:</strong> %s<br>
        <strong>Generated:</strong> %s
        <div class="summary-stats">
            <span class="stat stat-total">Total: %d</span>
            <span class="stat stat-allow">Allowed: %d</span>
            <span class="stat stat-deny">Denied: %d</span>
        </div>
    </div>
`,
		escapeHTML(session.Name),
		session.ID,
		log.SessionMode,
		time.Now().Format("2006-01-02 15:04:05"),
		len(log.Entries),
		log.TotalAllow,
		log.TotalDeny,
	))

	// Entries table
	html.WriteString(`    <table>
        <thead>
            <tr>
                <th>Time</th>
                <th>Action</th>
                <th>Category</th>
                <th>Tool</th>
                <th>Target</th>
            </tr>
        </thead>
        <tbody>
`)

	for _, entry := range log.Entries {
		actionClass := "action-allow"
		if entry.Action == storage.AuditActionDeny || entry.Action == storage.AuditActionDenyAll {
			actionClass = "action-deny"
		}

		categoryClass := fmt.Sprintf("category-%s", entry.Category)

		target := entry.Target
		if len(target) > 50 {
			target = target[:47] + "..."
		}

		html.WriteString(fmt.Sprintf(`            <tr>
                <td>%s</td>
                <td class="%s">%s</td>
                <td class="%s">%s</td>
                <td>%s</td>
                <td class="target" title="%s">%s</td>
            </tr>
`,
			entry.Timestamp.Format("15:04:05"),
			actionClass,
			escapeHTML(string(entry.Action)),
			categoryClass,
			escapeHTML(entry.Category),
			escapeHTML(entry.ToolName),
			escapeHTML(entry.Target),
			escapeHTML(target),
		))
	}

	html.WriteString(`        </tbody>
    </table>
    <div class="footer">
        Generated by Tara Code Security Mode
    </div>
</body>
</html>
`)

	if err := os.WriteFile(filename, []byte(html.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		return
	}

	fmt.Printf("%s Audit report exported to: %s\n", ui.IconSuccess, filename)
	fmt.Println()
}

// escapeHTML escapes special HTML characters
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// handleSessionInfo displays current session information
func handleSessionInfo(asst *assistant.Assistant) {
	// Use GetSessionFresh to get updated message count from storage
	session := asst.GetSessionFresh()
	if session == nil {
		fmt.Println("No active session.")
		fmt.Println()
		return
	}

	fmt.Println("Current Session:")
	fmt.Printf("  ID: %s\n", ui.TruncateID(session.ID, 0))
	if session.Name != "" {
		fmt.Printf("  Name: %s\n", session.Name)
	}
	fmt.Printf("  Messages: %d\n", len(session.Messages))
	fmt.Printf("  Created: %s\n", session.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Updated: %s\n", session.UpdatedAt.Format(time.RFC3339))
	if session.Summary != "" {
		fmt.Printf("  Summary: %s\n", session.Summary)
	}
	fmt.Println()
}

// handleListSessions displays all available sessions with names and summaries
func handleListSessions(asst *assistant.Assistant) {
	sessions, err := asst.ListSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
		return
	}

	if len(sessions) == 0 {
		fmt.Println("No saved sessions.")
		fmt.Println()
		return
	}

	currentSession := asst.GetSession()
	fmt.Println("Sessions:")
	fmt.Println()
	for _, s := range sessions {
		active := ""
		if currentSession != nil && s.ID == currentSession.ID {
			active = " *"
		}

		// Format name (or show unnamed)
		name := s.Name
		if name == "" {
			name = "(unnamed)"
		} else if len(name) > 30 {
			name = name[:27] + "..."
		}

		// Format the session line
		fmt.Printf("  %s  %-30s %3d msgs  %s%s\n",
			ui.TruncateID(s.ID, 0), name, s.MessageCount, s.UpdatedAt.Format("2006-01-02 15:04"), active)

		// Show summary if available
		if s.Summary != "" {
			summary := s.Summary
			if len(summary) > 70 {
				summary = summary[:67] + "..."
			}
			fmt.Printf("            \"%s\"\n", summary)
		}
	}
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /session load <id>       - Load a session")
	fmt.Println("  /session delete <id>     - Delete a session")
	fmt.Println("  /session rename <id> <n> - Rename a session")
	fmt.Println()
}

// handleDeleteSession deletes a session by ID with confirmation
func handleDeleteSession(asst *assistant.Assistant, sessionID string) {
	storage := asst.GetStorage()
	if storage == nil {
		fmt.Fprintf(os.Stderr, "Error: storage not initialized\n")
		return
	}

	// Confirm deletion
	fmt.Printf("Delete session %s? [y/N]: ", ui.TruncateID(sessionID, 0))
	var response string
	fmt.Scanln(&response)

	if strings.ToLower(response) != "y" {
		fmt.Println("Cancelled.")
		fmt.Println()
		return
	}

	if err := storage.DeleteSession(sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting session: %v\n", err)
		return
	}

	fmt.Println("Session deleted.")
	fmt.Println()
}

// handleRenameSession renames a session
func handleRenameSession(asst *assistant.Assistant, sessionID, newName string) {
	storage := asst.GetStorage()
	if storage == nil {
		fmt.Fprintf(os.Stderr, "Error: storage not initialized\n")
		return
	}

	if err := storage.RenameSession(sessionID, newName); err != nil {
		fmt.Fprintf(os.Stderr, "Error renaming session: %v\n", err)
		return
	}

	fmt.Printf("Session renamed to: %s\n", newName)
	fmt.Println()
}

// handleExitWithSummary handles exit by generating session summary
func handleExitWithSummary(asst *assistant.Assistant) {
	// Generate summary for current session if it has enough messages
	session := asst.GetSession()
	if session != nil && len(session.Messages) > 2 && session.Summary == "" {
		fmt.Print("Generating summary... ")
		if _, err := asst.GenerateSummary(); err != nil {
			fmt.Println("skipped.")
		} else {
			fmt.Println("done.")
		}
	}

	fmt.Println("Goodbye!")
}

// handleStatus displays project and session status
func handleStatus(asst *assistant.Assistant, workingDir string) {
	fmt.Println("Status:")
	fmt.Println()

	// Provider info
	providerInfo := asst.GetProviderInfo()
	if providerInfo != nil {
		fmt.Printf("  Provider: %s (%s)\n", providerInfo.Name, providerInfo.Type)
		fmt.Printf("  Host: %s\n", providerInfo.Host)
		fmt.Printf("  Model: %s\n", providerInfo.Model)
	}

	// Project info
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	if _, err := os.Stat(taracodeFile); err == nil {
		fmt.Println("  Project: Initialized")

		// Try to read project.json for more info
		projectFile := filepath.Join(workingDir, ".taracode", "context", "project.json")
		if _, err := os.Stat(projectFile); err == nil {
			fmt.Println("  Context: Cached in .taracode/context/")
		}
	} else {
		fmt.Println("  Project: Not initialized (run /init)")
	}

	// Session info
	session := asst.GetSession()
	if session != nil {
		fmt.Printf("  Session: %s (%d messages)\n", ui.TruncateID(session.ID, 0), len(session.Messages))
	} else {
		fmt.Println("  Session: None")
	}

	// Storage info
	storage := asst.GetStorage()
	if storage != nil {
		fmt.Printf("  Storage: %s\n", storage.GetRootDir())
	} else {
		fmt.Println("  Storage: Not available")
	}

	fmt.Println()
}

// handleShowPlan displays the active task plan
func handleShowPlan(asst *assistant.Assistant) {
	storage := asst.GetStorage()
	if storage == nil {
		fmt.Println("Storage not initialized. Run /init first.")
		fmt.Println()
		return
	}

	plan, err := storage.GetActivePlan()
	if err != nil || plan == nil {
		fmt.Println("No active plan.")
		fmt.Println()
		return
	}

	fmt.Printf("Plan: %s\n", plan.Title)
	fmt.Printf("Status: %s\n", plan.Status)
	fmt.Println()

	for i, task := range plan.Tasks {
		status := "[ ]"
		switch task.Status {
		case "completed":
			status = "[x]"
		case "in_progress":
			status = "[>]"
		case "skipped":
			status = "[-]"
		}
		fmt.Printf("  %d. %s %s\n", i+1, status, task.Content)
	}
	fmt.Println()
}

// modelWithHost combines model info with host name for multi-host display
type modelWithHost struct {
	provider.ModelInfo
	HostName string
	HostURL  string // URL for recreating assistant if host changes
	APIKey   string
	Vendor   string
}

// handleModelSwitch lists available models and allows switching
func handleModelSwitch(asst **assistant.Assistant, taskBridge *orchestrator.TaskBridge, streaming, enableSpinner bool) {
	// Get current model
	currentModel := (*asst).GetCurrentModel()
	currentHost := ""
	if provInfo := (*asst).GetProviderInfo(); provInfo != nil {
		currentHost = provInfo.Host
	}
	fmt.Printf("Current model: %s\n\n", currentModel)

	var allModels []modelWithHost

	// Check for multi-host mode
	if taskBridge != nil && taskBridge.HasHostPool() {
		hostPool := taskBridge.GetHostPool()
		hostInfos := hostPool.GetHostInfo()

		// Collect models from all healthy hosts
		for _, hostInfo := range hostInfos {
			if hostInfo.Status != provider.HostStatusHealthy {
				continue
			}
			conn, ok := hostPool.GetConnection(hostInfo.Name)
			if !ok || conn.Provider == nil {
				continue
			}

			// Get models from this host's provider
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			hostModels, err := conn.Provider.DetectModels(ctx)
			cancel()

			if err != nil {
				continue
			}

			// Get detailed model info if provider supports it
			if mm, ok := conn.Provider.(provider.ModelManager); ok {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				detailedModels, err := mm.ListModels(ctx)
				cancel()

				if err == nil {
					for _, dm := range detailedModels {
						allModels = append(allModels, modelWithHost{
							ModelInfo: provider.ModelInfo{
								Name:   dm.Name,
								Size:   dm.Size,
								Params: dm.Params,
							},
							HostName: hostInfo.Name,
							HostURL:  conn.Config.URL,
							APIKey:   conn.Config.APIKey,
							Vendor:   conn.Config.Vendor,
						})
					}
					continue
				}
			}

			// Fall back to basic model list
			for _, modelName := range hostModels {
				allModels = append(allModels, modelWithHost{
					ModelInfo: provider.ModelInfo{Name: modelName},
					HostName:  hostInfo.Name,
					HostURL:   conn.Config.URL,
					APIKey:    conn.Config.APIKey,
					Vendor:    conn.Config.Vendor,
				})
			}
		}
	} else {
		// Single host mode - use assistant's ListModels
		models, err := (*asst).ListModels()
		if err != nil {
			fmt.Fprintln(os.Stderr, ui.EnhanceError(err))
			fmt.Println()
			return
		}
		for _, m := range models {
			allModels = append(allModels, modelWithHost{ModelInfo: m, HostName: ""})
		}
	}

	if len(allModels) == 0 {
		fmt.Println("No models available.")
		fmt.Println("Pull a model with: ollama pull gemma3:27b")
		fmt.Println()
		return
	}

	// Build items for the selector
	items := make([]string, len(allModels))
	for i, m := range allModels {
		indicator := "  "
		if m.Name == currentModel {
			indicator = "* "
		}

		// Include host name for multi-host mode
		hostLabel := ""
		if m.HostName != "" {
			hostLabel = fmt.Sprintf(" [%s]", m.HostName)
		}

		if m.Params != "" && m.Size > 0 {
			items[i] = fmt.Sprintf("%s%s (%s, %s)%s", indicator, m.Name, m.Params, m.FormatSize(), hostLabel)
		} else if m.Params != "" {
			items[i] = fmt.Sprintf("%s%s (%s)%s", indicator, m.Name, m.Params, hostLabel)
		} else {
			items[i] = fmt.Sprintf("%s%s%s", indicator, m.Name, hostLabel)
		}
	}

	// Show interactive selector
	prompt := promptui.Select{
		Label: "Select a model",
		Items: items,
		Size:  15,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		// User cancelled
		fmt.Println("Model switch cancelled.")
		fmt.Println()
		return
	}

	selected := allModels[idx]
	selectedModel := selected.Name

	if selectedModel == currentModel && (selected.HostURL == "" || selected.HostURL == currentHost) {
		fmt.Printf("Already using %s\n", selectedModel)
		fmt.Println()
		return
	}

	// Check if we need to switch hosts (multi-host mode)
	needsHostSwitch := selected.HostURL != "" && selected.HostURL != currentHost

	if needsHostSwitch {
		// Update persisted model BEFORE creating new assistant
		// This prevents the warning about saved model not being available
		if storage := (*asst).GetStorage(); storage != nil {
			_ = storage.SetPreferredModel(selectedModel)
		}

		// Recreate assistant with new host
		fmt.Printf("Switching to %s on host %s...\n", selectedModel, selected.HostName)

		newAsst, err := assistant.New(selected.HostURL, selected.APIKey, selectedModel, selected.Vendor, streaming, enableSpinner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error switching host: %v\n", err)
			return
		}
		*asst = newAsst
		fmt.Printf("Now using: %s [%s]\n", selectedModel, selected.HostName)
	} else {
		// Same host, just switch model
		fmt.Printf("Switching from %s to %s...\n", currentModel, selectedModel)

		if err := (*asst).SwitchModel(selectedModel); err != nil {
			fmt.Fprintf(os.Stderr, "Error switching model: %v\n", err)
			return
		}
		fmt.Printf("Now using: %s\n", selectedModel)
	}
	fmt.Println()
}

// handlePermissions handles the /permissions command
func handlePermissions(asst *assistant.Assistant, args []string) {
	permMgr := asst.GetPermissionManager()
	if permMgr == nil {
		fmt.Println("Permission manager not initialized. Run /init first.")
		fmt.Println()
		return
	}

	if len(args) == 0 {
		// Show current permissions
		config := permMgr.GetConfig()

		fmt.Println("Permission Settings:")
		fmt.Println()

		// Show category settings
		fmt.Println("  Categories:")
		for _, cat := range permissions.GetAllCategories() {
			perm := permissions.GetDefaultPermission(cat)
			if customPerm, ok := config.Categories[cat]; ok {
				perm = customPerm
			}
			desc := permissions.GetCategoryDescription(cat)
			fmt.Printf("    %-12s → %-5s  %s\n", cat, perm, desc)
		}
		fmt.Println()

		// Show tool-specific settings
		if len(config.Tools) > 0 {
			fmt.Println("  Tool Overrides:")
			for tool, perm := range config.Tools {
				fmt.Printf("    %-20s → %s\n", tool, perm)
			}
			fmt.Println()
		}

		fmt.Println("  Commands:")
		fmt.Println("    /permissions reset              - Reset all to default")
		fmt.Println("    /permissions allow <tool|cat>   - Always allow")
		fmt.Println("    /permissions deny <tool|cat>    - Always deny")
		fmt.Println("    /permissions ask <tool|cat>     - Always ask")
		fmt.Println()
		return
	}

	subCmd := args[0]

	switch subCmd {
	case "reset":
		if err := permMgr.Reset(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resetting permissions: %v\n", err)
			return
		}
		fmt.Println("Permissions reset to default.")
		fmt.Println()

	case "allow", "deny", "ask":
		if len(args) < 2 {
			fmt.Printf("Usage: /permissions %s <tool|category>\n", subCmd)
			fmt.Println()
			return
		}

		target := args[1]
		var perm permissions.Permission
		switch subCmd {
		case "allow":
			perm = permissions.PermissionAllow
		case "deny":
			perm = permissions.PermissionDeny
		case "ask":
			perm = permissions.PermissionAsk
		}

		// Check if it's a category
		if permissions.IsValidCategory(target) {
			cat := permissions.PermissionCategory(target)
			if err := permMgr.SetCategoryPermission(cat, perm); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting permission: %v\n", err)
				return
			}
			fmt.Printf("Category '%s' → %s\n", target, perm)
		} else if permissions.IsValidTool(target) {
			if err := permMgr.SetToolPermission(target, perm); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting permission: %v\n", err)
				return
			}
			fmt.Printf("Tool '%s' → %s\n", target, perm)
		} else {
			fmt.Printf("Unknown tool or category: %s\n", target)
			fmt.Println("Valid categories: read, write, execute, git, destructive")
			fmt.Println("Use /tools to see valid tool names.")
		}
		fmt.Println()

	default:
		fmt.Printf("Unknown subcommand: %s\n", subCmd)
		fmt.Println("Usage: /permissions [reset|allow|deny|ask] [target]")
		fmt.Println()
	}
}

// initSearchOrchestrator initializes the search orchestrator with config from viper
func initSearchOrchestrator(renderer *ui.Renderer) {
	cfg := GetSearchConfig()

	// Parse timeout
	timeout := 10 * time.Second
	if cfg.Timeout != "" {
		if parsed, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsed
		}
	}

	// Set up provider switch callback for UI feedback
	tools.SetProviderSwitchCallback(func(from, to string, reason error) {
		if renderer != nil {
			fmt.Println()
			fmt.Println(renderer.SearchFallbackMessage(from, to, reason))
		}
	})

	// Initialize the orchestrator with config
	tools.InitSearchOrchestrator(tools.SearchOrchestratorConfig{
		Primary:         cfg.Primary,
		Fallback:        cfg.Fallback,
		Timeout:         timeout,
		RetryCount:      cfg.RetryCount,
		SearXNGInstance: cfg.SearXNGInstance,
		BraveAPIKey:     cfg.BraveAPIKey,
	})
}

// initCommandStreaming initializes command output streaming
func initCommandStreaming() {
	// Check if streaming is disabled via config
	if viper.GetBool("no_stream_commands") {
		tools.DisableStreaming()
		return
	}

	// Enable streaming to stdout with sensible defaults
	tools.SetStreamingConfig(tools.StreamingConfig{
		Enabled:       true,
		Writer:        os.Stdout,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    256,
	})
}

// handleMCP handles the /mcp command for managing MCP server connections
func handleMCP(mgr *mcp.Manager, args []string, asst *assistant.Assistant) {
	if mgr == nil {
		fmt.Println("MCP is not enabled. Add MCP servers to your config file.")
		fmt.Println()
		fmt.Println("Example ~/.taracode/config.yaml:")
		fmt.Println("  mcp:")
		fmt.Println("    enabled: true")
		fmt.Println("    servers:")
		fmt.Println("      - name: github")
		fmt.Println("        command: npx")
		fmt.Println("        args: [\"-y\", \"@modelcontextprotocol/server-github\"]")
		fmt.Println("        env:")
		fmt.Println("          GITHUB_TOKEN: \"${GITHUB_TOKEN}\"")
		fmt.Println()
		return
	}

	if len(args) == 0 {
		// Show configured servers and their status
		servers := mgr.GetConfiguredServers()
		connections := mgr.GetAllConnections()

		if len(servers) == 0 {
			fmt.Println("No MCP servers configured.")
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Println("MCP Servers:")
		fmt.Println()

		for _, srv := range servers {
			status := "disconnected"
			toolCount := 0

			if conn, ok := connections[srv.Name]; ok {
				status = string(conn.Status)
				toolCount = len(conn.Tools)
			}

			// Color-coded status
			var statusDisplay string
			switch status {
			case "connected":
				statusDisplay = fmt.Sprintf("\033[32m%s\033[0m", status)
			case "connecting":
				statusDisplay = fmt.Sprintf("\033[33m%s\033[0m", status)
			case "error":
				statusDisplay = fmt.Sprintf("\033[31m%s\033[0m", status)
			default:
				statusDisplay = fmt.Sprintf("\033[90m%s\033[0m", status)
			}

			autoConnect := ""
			if srv.AutoConnect {
				autoConnect = " (auto-connect)"
			}

			fmt.Printf("  %-15s %s%s", srv.Name, statusDisplay, autoConnect)
			if toolCount > 0 {
				fmt.Printf("  [%d tools]", toolCount)
			}
			fmt.Println()
		}

		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  /mcp connect <name>    - Connect to a server")
		fmt.Println("  /mcp disconnect <name> - Disconnect from a server")
		fmt.Println("  /mcp tools             - List tools from connected servers")
		fmt.Println()
		return
	}

	subCmd := args[0]

	switch subCmd {
	case "connect":
		if len(args) < 2 {
			fmt.Println("Usage: /mcp connect <server-name>")
			fmt.Println()
			return
		}
		serverName := args[1]
		fmt.Printf("Connecting to %s...\n", serverName)

		if err := mgr.Connect(context.Background(), serverName); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}

		// Get tool count
		mcpTools := mgr.GetToolsByServer(serverName)
		fmt.Printf("Connected to %s. Discovered %d tools.\n", serverName, len(mcpTools))

		// Register tools with the assistant's tool registry and add tool definitions
		if registry := asst.GetToolRegistry(); registry != nil {
			for _, tool := range mcpTools {
				executor := mcp.CreateExecutor(mgr, tool)
				registry.RegisterMCPTool(tool.Name, tool.ServerName, executor)
				// Add tool definition for LLM function calling
				asst.AddMCPToolDefinition(mcp.ToOpenAITool(tool))
			}
		}
		fmt.Println()

	case "disconnect":
		if len(args) < 2 {
			fmt.Println("Usage: /mcp disconnect <server-name>")
			fmt.Println()
			return
		}
		serverName := args[1]

		// Unregister tools and remove tool definitions
		if registry := asst.GetToolRegistry(); registry != nil {
			registry.UnregisterMCPTools(serverName)
		}
		asst.RemoveMCPToolDefinitions(serverName)

		if err := mgr.Disconnect(serverName); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}

		fmt.Printf("Disconnected from %s.\n", serverName)
		fmt.Println()

	case "tools":
		mcpTools := mgr.GetAllTools()
		if len(mcpTools) == 0 {
			fmt.Println("No MCP tools available. Connect to a server first.")
			fmt.Println("  /mcp connect <name>")
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Printf("MCP Tools (%d total):\n", len(mcpTools))
		fmt.Println()

		// Group tools by server
		toolsByServer := make(map[string][]mcp.MCPTool)
		for _, tool := range mcpTools {
			toolsByServer[tool.ServerName] = append(toolsByServer[tool.ServerName], tool)
		}

		for serverName, serverTools := range toolsByServer {
			fmt.Printf("  %s:\n", serverName)
			for _, t := range serverTools {
				desc := t.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Printf("    %-30s %s\n", t.Name, desc)
			}
			fmt.Println()
		}

	default:
		fmt.Printf("Unknown subcommand: %s\n", subCmd)
		fmt.Println("Usage: /mcp [connect|disconnect|tools]")
		fmt.Println()
	}
}

// handleTask handles the /task command for autonomous task execution
func handleTask(asst *assistant.Assistant, args []string, workingDir string) {
	// Get or create task manager
	taracodeDir := filepath.Join(workingDir, ".taracode")
	taskManager, err := storage.NewTaskManager(taracodeDir)
	if err != nil {
		fmt.Printf("Error initializing task manager: %v\n", err)
		fmt.Println()
		return
	}

	// If no arguments, show usage
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  /task \"<description>\" - Plan and execute a multi-step task")
		fmt.Println("  /task list            - List all tasks")
		fmt.Println("  /task status [id]     - Show task status")
		fmt.Println("  /task resume [id]     - Resume a paused task")
		fmt.Println("  /task pause           - Pause the active task")
		fmt.Println("  /task abort           - Abort the active task")
		fmt.Println("  /task rollback [id]   - Rollback to last checkpoint")
		fmt.Println("  /task templates       - List available task templates")
		fmt.Println("  /task run <template>  - Run a task from template")
		fmt.Println()
		return
	}

	subCmd := args[0]

	switch subCmd {
	case "list":
		tasks := taskManager.ListTasks()
		ui.DisplayTaskList(tasks)

	case "templates":
		templateLoader := assistant.NewTemplateLoader(taracodeDir)
		templates, err := templateLoader.ListTemplates()
		if err != nil {
			fmt.Printf("Error listing templates: %v\n", err)
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Println("Available Task Templates:")
		fmt.Println()

		if len(templates) == 0 {
			fmt.Println("  No templates found")
		} else {
			for _, t := range templates {
				source := "built-in"
				if !t.BuiltIn {
					source = "custom"
				}
				fmt.Printf("  %-20s [%s]\n", t.Name, source)
			}
		}
		fmt.Println()
		fmt.Println("Run a template with: /task run <template-name>")
		fmt.Println()

	case "run":
		if len(args) < 2 {
			fmt.Println("Usage: /task run <template-name> [var=value ...]")
			fmt.Println()
			fmt.Println("Examples:")
			fmt.Println("  /task run docker-build image_name=myapp tag=v1.0")
			fmt.Println("  /task run k8s-deploy namespace=production")
			fmt.Println("  /task run terraform-apply working_dir=./infra")
			fmt.Println()
			return
		}

		templateName := args[1]

		// Parse variable arguments
		variables := make(map[string]string)
		for _, arg := range args[2:] {
			if parts := strings.SplitN(arg, "=", 2); len(parts) == 2 {
				variables[parts[0]] = parts[1]
			}
		}

		templateLoader := assistant.NewTemplateLoader(taracodeDir)
		template, err := templateLoader.LoadTemplate(templateName)
		if err != nil {
			fmt.Printf("Error loading template: %v\n", err)
			fmt.Println()
			return
		}

		// Show template variables
		fmt.Println()
		fmt.Printf("Template: %s\n", template.Name)
		if template.Description != "" {
			fmt.Printf("Description: %s\n", template.Description)
		}

		// Show variables with defaults and overrides
		if len(template.Variables) > 0 {
			fmt.Println("\nVariables:")
			for k, defaultVal := range template.Variables {
				if override, ok := variables[k]; ok {
					fmt.Printf("  %s = %s (overridden from: %s)\n", k, override, defaultVal)
				} else {
					fmt.Printf("  %s = %s (default)\n", k, defaultVal)
				}
			}
		}

		// Create task from template
		task, err := templateLoader.CreateTaskFromTemplate(template, variables)
		if err != nil {
			fmt.Printf("Error creating task: %v\n", err)
			fmt.Println()
			return
		}

		// Save and set as active
		if err := taskManager.SaveTask(task); err != nil {
			fmt.Printf("Error saving task: %v\n", err)
			fmt.Println()
			return
		}
		taskManager.SetActiveTask(task.ID)

		// Display the plan
		ui.DisplayTaskPlan(task)

		// Ask for approval
		choice := ui.DisplayTaskApprovalPrompt()

		switch choice {
		case "r", "run":
			executor := assistant.NewTaskExecutor(asst, taskManager)
			fmt.Println()
			fmt.Println("Executing task...")
			fmt.Println()

			if err := executor.RunTask(task); err != nil {
				fmt.Printf("Error: %v\n", err)
				fmt.Println()
				return
			}

			if task.Status == storage.TaskExecStatusCompleted {
				ui.DisplayTaskComplete(task)
			} else if task.Status == storage.TaskExecStatusFailed {
				ui.DisplayTaskFailed(task)
			} else {
				ui.DisplayTaskStatus(task)
			}

		case "c", "cancel":
			taskManager.DeleteTask(task.ID)
			fmt.Println("Task cancelled.")
			fmt.Println()

		default:
			fmt.Println("Task saved but not executed.")
			fmt.Println("Resume with: /task resume")
			fmt.Println()
		}

	case "status":
		var task *storage.TaskExecution
		if len(args) > 1 {
			// Load specific task
			task, err = taskManager.LoadTask(args[1])
		} else {
			// Show active task
			task, err = taskManager.GetActiveTask()
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		if task == nil {
			fmt.Println("No active task. Start one with: /task \"<description>\"")
			fmt.Println()
			return
		}
		ui.DisplayTaskStatus(task)

	case "resume":
		var task *storage.TaskExecution
		if len(args) > 1 {
			task, err = taskManager.LoadTask(args[1])
		} else {
			task, err = taskManager.GetActiveTask()
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		if task == nil {
			fmt.Println("No task to resume.")
			fmt.Println()
			return
		}
		if task.Status != storage.TaskExecStatusPaused && task.Status != storage.TaskExecStatusPending {
			fmt.Printf("Cannot resume task: status is %s\n", task.Status)
			fmt.Println()
			return
		}

		// Run the task
		executor := assistant.NewTaskExecutor(asst, taskManager)
		if err := executor.RunTask(task); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}

		if task.Status == storage.TaskExecStatusCompleted {
			ui.DisplayTaskComplete(task)
		} else if task.Status == storage.TaskExecStatusFailed {
			ui.DisplayTaskFailed(task)
		} else {
			ui.DisplayTaskStatus(task)
		}

	case "pause":
		task, err := taskManager.GetActiveTask()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		if task == nil {
			fmt.Println("No active task to pause.")
			fmt.Println()
			return
		}

		executor := assistant.NewTaskExecutor(asst, taskManager)
		if err := executor.PauseTask(task); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		fmt.Println("Task paused. Resume with: /task resume")
		fmt.Println()

	case "abort":
		task, err := taskManager.GetActiveTask()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		if task == nil {
			fmt.Println("No active task to abort.")
			fmt.Println()
			return
		}

		executor := assistant.NewTaskExecutor(asst, taskManager)
		if err := executor.AbortTask(task); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		fmt.Println("Task aborted.")
		fmt.Println()

	case "rollback":
		var task *storage.TaskExecution
		if len(args) > 1 {
			task, err = taskManager.LoadTask(args[1])
		} else {
			task, err = taskManager.GetActiveTask()
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		if task == nil {
			fmt.Println("No task to rollback.")
			fmt.Println()
			return
		}

		checkpoint := taskManager.GetLatestCheckpoint(task)
		if checkpoint == nil {
			fmt.Println("No checkpoints available for rollback.")
			fmt.Println()
			return
		}

		if err := taskManager.RollbackToCheckpoint(task, checkpoint.ID); err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			return
		}
		fmt.Printf("Rolled back to checkpoint at step %d.\n", checkpoint.StepIndex)
		fmt.Println()

	default:
		// Treat as task description - plan and execute
		taskDescription := strings.Join(args, " ")
		// Remove quotes if present
		taskDescription = strings.Trim(taskDescription, "\"'")

		if taskDescription == "" {
			fmt.Println("Please provide a task description.")
			fmt.Println("Example: /task \"Add authentication to the API\"")
			fmt.Println()
			return
		}

		// Create task planner and generate plan
		planner := assistant.NewTaskPlanner(asst)
		fmt.Println()
		fmt.Printf("Planning task: %s\n", taskDescription)

		spinner := ui.NewThinkingSpinner()
		spinner.Start("Generating execution plan...")

		task, err := planner.PlanTask(taskDescription)
		spinner.Stop()

		if err != nil {
			fmt.Printf("Error creating plan: %v\n", err)
			fmt.Println()
			return
		}

		// Save task
		if err := taskManager.SaveTask(task); err != nil {
			fmt.Printf("Error saving task: %v\n", err)
			fmt.Println()
			return
		}
		taskManager.SetActiveTask(task.ID)

		// Display the plan
		ui.DisplayTaskPlan(task)

		// Ask for approval
		choice := ui.DisplayTaskApprovalPrompt()

		switch choice {
		case "r", "run":
			// Run the task
			executor := assistant.NewTaskExecutor(asst, taskManager)
			fmt.Println()
			fmt.Println("Executing task...")
			fmt.Println()

			if err := executor.RunTask(task); err != nil {
				fmt.Printf("Error: %v\n", err)
				fmt.Println()
				return
			}

			if task.Status == storage.TaskExecStatusCompleted {
				ui.DisplayTaskComplete(task)
			} else if task.Status == storage.TaskExecStatusFailed {
				ui.DisplayTaskFailed(task)
			} else {
				ui.DisplayTaskStatus(task)
			}

		case "e", "edit":
			fmt.Println("Edit mode not yet implemented.")
			fmt.Println("You can modify the task by running /task again with a more specific description.")
			fmt.Println()

		case "c", "cancel":
			if err := taskManager.DeleteTask(task.ID); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
			fmt.Println("Task cancelled.")
			fmt.Println()

		default:
			fmt.Println("Unknown choice. Task saved but not executed.")
			fmt.Println("Resume with: /task resume")
			fmt.Println()
		}
	}
}

// handleDiff handles the /diff command for showing file changes
func handleDiff(historyManager *history.Manager, args []string, workingDir string) {
	if historyManager == nil {
		fmt.Println("History tracking not available.")
		fmt.Println("Initialize the project with /init first.")
		fmt.Println()
		return
	}

	// Check for subcommands
	if len(args) > 0 && args[0] == "export" {
		exportDiffToPatch(historyManager, workingDir)
		return
	}

	// Show diff in terminal
	diffs, err := historyManager.GenerateDiff()
	if err != nil {
		fmt.Printf("Error generating diff: %v\n", err)
		fmt.Println()
		return
	}

	if len(diffs) == 0 {
		fmt.Println("No file changes in this session.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("File changes in session (%d files):\n", len(diffs))
	fmt.Println(strings.Repeat("─", 60))

	for _, d := range diffs {
		// Show header with operation type
		var opColor string
		switch d.Operation {
		case "created":
			opColor = "\033[32m" // Green
		case "deleted":
			opColor = "\033[31m" // Red
		case "modified":
			opColor = "\033[33m" // Yellow
		case "moved", "copied":
			opColor = "\033[34m" // Blue
		default:
			opColor = "\033[0m"
		}

		fmt.Printf("\n%s[%s]\033[0m %s\n", opColor, d.Operation, d.Path)
		if d.OriginalPath != "" {
			fmt.Printf("  (from %s)\n", d.OriginalPath)
		}

		// Show diff with syntax highlighting
		if d.Diff != "" {
			for _, line := range strings.Split(d.Diff, "\n") {
				if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
					fmt.Printf("\033[1m%s\033[0m\n", line)
				} else if strings.HasPrefix(line, "@@") {
					fmt.Printf("\033[36m%s\033[0m\n", line) // Cyan
				} else if strings.HasPrefix(line, "+") {
					fmt.Printf("\033[32m%s\033[0m\n", line) // Green
				} else if strings.HasPrefix(line, "-") {
					fmt.Printf("\033[31m%s\033[0m\n", line) // Red
				} else {
					fmt.Println(line)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Total: %d file(s) changed\n", len(diffs))
	fmt.Println("Export with: /diff export")
	fmt.Println()
}

// exportDiffToPatch exports the diff to a .patch file
func exportDiffToPatch(historyManager *history.Manager, workingDir string) {
	diffContent, err := historyManager.ExportDiff()
	if err != nil {
		fmt.Printf("Error generating diff: %v\n", err)
		fmt.Println()
		return
	}

	if diffContent == "" {
		fmt.Println("No file changes to export.")
		fmt.Println()
		return
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("session-%s.patch", timestamp)
	patchPath := filepath.Join(workingDir, filename)

	if err := os.WriteFile(patchPath, []byte(diffContent), 0644); err != nil {
		fmt.Printf("Error writing patch file: %v\n", err)
		fmt.Println()
		return
	}

	// Count files
	lines := strings.Split(diffContent, "\n")
	fileCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			fileCount++
		}
	}

	fmt.Printf("Exported %d file change(s) to: %s\n", fileCount, filename)
	fmt.Println()
}

// handleRemember saves a new memory about the project
func handleRemember(mm *memory.Manager, args []string, asst **assistant.Assistant) {
	if mm == nil {
		fmt.Println("Memory not available.")
		fmt.Println("Initialize the project with /init first.")
		fmt.Println()
		return
	}

	if len(args) == 0 {
		fmt.Println("Usage: /remember <text to remember>")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  /remember Use snake_case for all variable names")
		fmt.Println("  /remember Database is PostgreSQL on port 5432")
		fmt.Println("  /remember Always run tests before committing")
		fmt.Println()
		return
	}

	content := strings.Join(args, " ")

	// Detect category from content
	category := detectMemoryCategory(content)

	// Extract tags from content (words starting with #)
	var tags []string
	words := strings.Fields(content)
	cleanedWords := make([]string, 0, len(words))
	for _, word := range words {
		if strings.HasPrefix(word, "#") && len(word) > 1 {
			tags = append(tags, strings.TrimPrefix(word, "#"))
		} else {
			cleanedWords = append(cleanedWords, word)
		}
	}
	content = strings.Join(cleanedWords, " ")

	mem, err := mm.Create(category, content, "", tags, storage.MemorySourceManual)
	if err != nil {
		fmt.Printf("Error saving memory: %v\n", err)
		fmt.Println()
		return
	}

	categoryIcon := getCategoryIcon(category)
	fmt.Printf("%s Saved [%s] (ID: %s)\n", categoryIcon, category, mem.ID)
	if len(tags) > 0 {
		fmt.Printf("   Tags: %s\n", strings.Join(tags, ", "))
	}
	fmt.Println()

	// Refresh system prompt to include the new memory
	if asst != nil && *asst != nil {
		(*asst).RefreshSystemPrompt()
	}
}

// detectMemoryCategory determines the category based on content keywords
func detectMemoryCategory(content string) storage.MemoryCategory {
	lower := strings.ToLower(content)

	// Decision indicators
	decisionKeywords := []string{"decided", "decision", "chose", "chosen", "will use", "using", "selected", "architecture", "design"}
	for _, kw := range decisionKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryDecision
		}
	}

	// Pattern indicators
	patternKeywords := []string{"always", "never", "convention", "style", "naming", "format", "prefer", "use case", "camelcase", "snake_case", "pattern"}
	for _, kw := range patternKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryPattern
		}
	}

	// Error indicators
	errorKeywords := []string{"error", "bug", "fix", "issue", "problem", "crash", "fail", "exception", "timeout", "solution"}
	for _, kw := range errorKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryError
		}
	}

	// Default to learning
	return storage.MemoryCategoryLearning
}

// getCategoryIcon returns an icon for the memory category
func getCategoryIcon(cat storage.MemoryCategory) string {
	switch cat {
	case storage.MemoryCategoryDecision:
		return "🏛️"
	case storage.MemoryCategoryPattern:
		return "📐"
	case storage.MemoryCategoryError:
		return "🐛"
	case storage.MemoryCategoryLearning:
		return "💡"
	default:
		return "📝"
	}
}

// handleMemory handles the /memory command and its subcommands
func handleMemory(mm *memory.Manager, args []string) {
	if mm == nil {
		fmt.Println("Memory not available.")
		fmt.Println("Initialize the project with /init first.")
		fmt.Println()
		return
	}

	// No args - list all memories
	if len(args) == 0 {
		listMemories(mm)
		return
	}

	switch args[0] {
	case "search":
		if len(args) < 2 {
			fmt.Println("Usage: /memory search <query>")
			fmt.Println()
			return
		}
		query := strings.Join(args[1:], " ")
		searchMemories(mm, query)

	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: /memory delete <id>")
			fmt.Println()
			return
		}
		deleteMemory(mm, args[1])

	case "export":
		filename := ""
		if len(args) > 1 {
			filename = args[1]
		}
		exportMemories(mm, filename)

	case "import":
		if len(args) < 2 {
			fmt.Println("Usage: /memory import <file>")
			fmt.Println()
			return
		}
		importMemories(mm, args[1])

	case "stats":
		showMemoryStats(mm)

	case "cleanup":
		days := 90 // Default retention
		if len(args) > 1 {
			if d, err := strconv.Atoi(args[1]); err == nil && d > 0 {
				days = d
			}
		}
		cleanupMemories(mm, days)

	case "clear":
		clearMemories(mm)

	default:
		fmt.Printf("Unknown subcommand: %s\n", args[0])
		fmt.Println("Usage: /memory [search|delete|export|import|stats|cleanup|clear]")
		fmt.Println()
	}
}

// listMemories displays all project memories
func listMemories(mm *memory.Manager) {
	memories := mm.List()

	if len(memories) == 0 {
		fmt.Println("No memories saved for this project.")
		fmt.Println("Use /remember <text> to save project knowledge.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("Project Memories (%d total):\n", len(memories))
	fmt.Println(strings.Repeat("─", 60))

	for _, meta := range memories {
		icon := getCategoryIcon(meta.Category)
		age := formatAge(meta.CreatedAt)
		useInfo := ""
		if meta.UseCount > 0 {
			useInfo = fmt.Sprintf(" (used %dx)", meta.UseCount)
		}
		fmt.Printf("\n%s [%s] %s%s\n", icon, meta.ID, string(meta.Category), useInfo)
		fmt.Printf("   %s\n", meta.Preview)
		fmt.Printf("   \033[90m%s\033[0m\n", age)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("Commands: /memory search <query> | /memory delete <id>")
	fmt.Println()
}

// searchMemories searches for memories matching a query
func searchMemories(mm *memory.Manager, query string) {
	results, err := mm.Search(query, nil)
	if err != nil {
		fmt.Printf("Error searching: %v\n", err)
		fmt.Println()
		return
	}

	if len(results) == 0 {
		fmt.Printf("No memories found matching: %s\n", query)
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("Search Results for \"%s\" (%d matches):\n", query, len(results))
	fmt.Println(strings.Repeat("─", 60))

	for _, meta := range results {
		icon := getCategoryIcon(meta.Category)
		fmt.Printf("\n%s [%s] %s\n", icon, meta.ID, string(meta.Category))
		fmt.Printf("   %s\n", meta.Preview)
	}

	fmt.Println()
}

// deleteMemory removes a memory by ID
func deleteMemory(mm *memory.Manager, id string) {
	// Show the memory first
	mem, err := mm.Get(id)
	if err != nil {
		fmt.Printf("Memory not found: %s\n", id)
		fmt.Println()
		return
	}

	fmt.Printf("Delete memory [%s]?\n", mem.ID)
	fmt.Printf("  %s\n", mem.Content)
	fmt.Print("Confirm (y/N): ")

	var confirm string
	fmt.Scanln(&confirm)

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("Cancelled.")
		fmt.Println()
		return
	}

	if err := mm.Delete(id); err != nil {
		fmt.Printf("Error deleting: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Println("Memory deleted.")
	fmt.Println()
}

// exportMemories exports all memories to a JSON file
func exportMemories(mm *memory.Manager, filename string) {
	data, err := mm.ExportJSON()
	if err != nil {
		fmt.Printf("Error exporting: %v\n", err)
		fmt.Println()
		return
	}

	if filename == "" {
		timestamp := time.Now().Format("20060102-150405")
		filename = fmt.Sprintf("memories-%s.json", timestamp)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Exported %d memories to: %s\n", mm.Count(), filename)
	fmt.Println()
}

// importMemories imports memories from a JSON file
func importMemories(mm *memory.Manager, filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		fmt.Println()
		return
	}

	imported, err := mm.ImportJSON(data)
	if err != nil {
		fmt.Printf("Error importing: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Imported %d memories from: %s\n", imported, filename)
	fmt.Println()
}

// showMemoryStats displays memory statistics
func showMemoryStats(mm *memory.Manager) {
	stats := mm.GetStats()

	fmt.Println()
	fmt.Println("Memory Statistics")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("Total memories:     %d\n", stats.TotalMemories)
	fmt.Printf("Total context uses: %d\n", stats.TotalUseCount)
	fmt.Printf("Unused memories:    %d\n", stats.UnusedCount)
	fmt.Printf("Est. tokens:        ~%d\n", stats.EstimatedTokens)
	fmt.Println()

	if len(stats.ByCategory) > 0 {
		fmt.Println("By Category:")
		for cat, count := range stats.ByCategory {
			icon := getCategoryIcon(storage.MemoryCategory(cat))
			fmt.Printf("  %s %-12s %d\n", icon, cat, count)
		}
		fmt.Println()
	}

	if len(stats.BySource) > 0 {
		fmt.Println("By Source:")
		for src, count := range stats.BySource {
			fmt.Printf("  %-12s %d\n", src, count)
		}
		fmt.Println()
	}

	if stats.MostUsedID != "" {
		fmt.Printf("Most Used: [%s] (%dx)\n", stats.MostUsedID, stats.MostUsedCount)
		if len(stats.MostUsedContent) > 50 {
			fmt.Printf("  %s...\n", stats.MostUsedContent[:50])
		} else {
			fmt.Printf("  %s\n", stats.MostUsedContent)
		}
		fmt.Println()
	}
}

// cleanupMemories removes old unused memories
func cleanupMemories(mm *memory.Manager, days int) {
	deleted, err := mm.Cleanup(days)
	if err != nil {
		fmt.Printf("Error during cleanup: %v\n", err)
		fmt.Println()
		return
	}

	if deleted == 0 {
		fmt.Printf("No memories older than %d days to clean up.\n", days)
	} else {
		fmt.Printf("Removed %d memories not used in %d days.\n", deleted, days)
	}
	fmt.Println()
}

// clearMemories removes all project memories
func clearMemories(mm *memory.Manager) {
	count := mm.Count()
	if count == 0 {
		fmt.Println("No memories to clear.")
		fmt.Println()
		return
	}

	fmt.Printf("Clear all %d memories? This cannot be undone.\n", count)
	fmt.Print("Type 'yes' to confirm: ")

	var confirm string
	fmt.Scanln(&confirm)

	if strings.ToLower(strings.TrimSpace(confirm)) != "yes" {
		fmt.Println("Cancelled.")
		fmt.Println()
		return
	}

	if err := mm.Clear(); err != nil {
		fmt.Printf("Error clearing memories: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Cleared %d memories.\n", count)
	fmt.Println()
}

// formatAge returns a human-readable age string
func formatAge(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Hour {
		mins := int(duration.Minutes())
		if mins <= 1 {
			return "just now"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}

	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(duration.Hours() / 24)
	if days == 1 {
		return "yesterday"
	}
	if days < 30 {
		return fmt.Sprintf("%d days ago", days)
	}

	months := days / 30
	if months == 1 {
		return "1 month ago"
	}
	return fmt.Sprintf("%d months ago", months)
}

// checkAutoCapture checks if user message contains patterns that suggest memory-worthy content
func checkAutoCapture(mm *memory.Manager, userMessage string) {
	lower := strings.ToLower(userMessage)

	// Skip short messages
	if len(userMessage) < 20 {
		return
	}

	// Skip questions
	if strings.HasSuffix(strings.TrimSpace(userMessage), "?") {
		return
	}

	var suggestion string
	var category storage.MemoryCategory

	// Correction patterns: user is correcting AI's understanding
	correctionPatterns := []string{
		"no,", "that's wrong", "actually,", "i meant", "not like that",
		"don't do", "never do", "stop doing", "instead of", "wrong approach",
	}
	for _, pattern := range correctionPatterns {
		if strings.Contains(lower, pattern) {
			suggestion = extractMemorySuggestion(userMessage)
			category = storage.MemoryCategoryPattern
			break
		}
	}

	// Convention patterns: user is stating a convention
	if suggestion == "" {
		conventionPatterns := []string{
			"always use", "we always", "never use", "we never", "our convention",
			"in this project", "our style", "our pattern", "we prefer", "standard is",
		}
		for _, pattern := range conventionPatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryPattern
				break
			}
		}
	}

	// Knowledge patterns: user is sharing important info
	if suggestion == "" {
		knowledgePatterns := []string{
			"remember that", "note that", "important:", "keep in mind",
			"database is", "server is", "port is", "api key", "config",
		}
		for _, pattern := range knowledgePatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryLearning
				break
			}
		}
	}

	// Decision patterns: user is stating a decision
	if suggestion == "" {
		decisionPatterns := []string{
			"we decided", "decision is", "chosen approach", "will use",
			"going with", "selected", "architecture is", "design is",
		}
		for _, pattern := range decisionPatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryDecision
				break
			}
		}
	}

	if suggestion != "" && len(suggestion) > 10 {
		// Clean up the suggestion
		suggestion = strings.TrimSpace(suggestion)
		if len(suggestion) > 200 {
			suggestion = suggestion[:197] + "..."
		}

		icon := getCategoryIcon(category)
		fmt.Printf("\n%s Remember: \"%s\"? [y/N] ", icon, suggestion)

		var confirm string
		fmt.Scanln(&confirm)

		if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
			mem, err := mm.Create(category, suggestion, "", nil, storage.MemorySourceAuto)
			if err != nil {
				fmt.Printf("Error saving: %v\n", err)
			} else {
				fmt.Printf("Saved [%s] (ID: %s)\n", category, mem.ID)
			}
		}
		fmt.Println()
	}
}

// extractMemorySuggestion extracts the key content from a correction message
func extractMemorySuggestion(message string) string {
	lower := strings.ToLower(message)

	// Remove common correction prefixes
	prefixes := []string{
		"no,", "no ", "that's wrong,", "that's wrong ", "actually,", "actually ",
		"i meant", "not like that,", "not like that ",
	}

	result := message
	for _, prefix := range prefixes {
		idx := strings.Index(lower, prefix)
		if idx == 0 {
			result = strings.TrimSpace(message[len(prefix):])
			break
		}
	}

	// Clean up
	result = strings.TrimLeft(result, " ,.-")

	return result
}
