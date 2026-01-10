package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/ui"
)

func startREPL() {
	// Get configuration from config or environment
	host := viper.GetString("host")
	if host == "" {
		fmt.Fprintln(os.Stderr, "Error: LLM server host not found.")
		fmt.Fprintln(os.Stderr, "Set it via:")
		fmt.Fprintln(os.Stderr, "  - Environment variable: export TARACODE_HOST=http://ollama.tara.lab")
		fmt.Fprintln(os.Stderr, "  - Config file: ~/.taracode/config.yaml")
		fmt.Fprintln(os.Stderr, "  - Command flag: --host http://ollama.tara.lab")
		os.Exit(1)
	}

	// API key is optional for local servers
	apiKey := viper.GetString("key")

	// Model is optional - will be auto-detected from server
	model := viper.GetString("model")

	// Vendor is optional - will be auto-detected from host URL
	vendor := viper.GetString("vendor")

	// Streaming is enabled by default (--no-stream to disable)
	streaming := !viper.GetBool("no_stream")

	// Spinner is enabled by default (--no-spinner to disable)
	enableSpinner := !viper.GetBool("no_spinner")

	// Get working directory
	workingDir, _ := os.Getwd()

	// Create renderer for styled output
	renderer := ui.NewRenderer()

	// Print welcome message
	fmt.Print(renderer.WelcomeMessage())

	// Check for project context
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	projectLoaded := false
	if _, err := os.Stat(taracodeFile); err == nil {
		projectLoaded = true
	}
	fmt.Print(renderer.ProjectContextMessage(projectLoaded))

	// Initialize the assistant
	asst, err := assistant.New(host, apiKey, model, vendor, streaming, enableSpinner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing assistant: %v\n", err)
		os.Exit(1)
	}

	// Show provider info
	if providerInfo := asst.GetProviderInfo(); providerInfo != nil {
		fmt.Print(renderer.ProviderMessage(providerInfo))
	}

	// Show session info
	if session := asst.GetSession(); session != nil && len(session.Messages) > 0 {
		fmt.Print(renderer.SessionResumeMessage(len(session.Messages)))
	}
	fmt.Println()

	// Setup readline for interactive input with @ file completion
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "\033[34m❯\033[0m ",
		HistoryFile:     os.Getenv("HOME") + "/.taracode/history",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    NewFileCompleter(workingDir),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up readline: %v\n", err)
		os.Exit(1)
	}
	defer rl.Close()

	// Main REPL loop
	for {
		line, err := rl.Readline()
		if err != nil { // io.EOF or Ctrl+C
			fmt.Println("\nGoodbye!")
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Expand @ file references before processing
		if strings.Contains(line, "@") {
			if !isInitializedProject(workingDir) {
				fmt.Println("💡 Tip: Run /init to enable @ file references with Tab completion")
				// Continue without expanding - treat @ as literal text
			} else {
				expandedLine, err := expandFileReferences(line, workingDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					continue
				}
				line = expandedLine
			}
		}

		// Handle built-in commands
		if strings.HasPrefix(line, "/") {
			handleCommand(line, workingDir, &asst, host, apiKey, model, vendor, streaming, enableSpinner)
			continue
		}

		if line == "exit" || line == "quit" {
			fmt.Println("Goodbye!")
			break
		}

		// Process the user's message
		if err := asst.ProcessMessage(line); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		fmt.Println()
	}
}

func handleCommand(cmd string, workingDir string, asst **assistant.Assistant, host, apiKey, model, vendor string, streaming bool, enableSpinner bool) {
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
		// Reinitialize assistant to pick up new context
		newAsst, err := assistant.New(host, apiKey, model, vendor, streaming, enableSpinner)
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
		fmt.Println("  Sessions:")
		fmt.Println("    /session      - Show current session info")
		fmt.Println("    /sessions     - List all conversation sessions")
		fmt.Println("    /session new  - Start a new conversation session")
		fmt.Println("    /session load <id> - Load a previous session")
		fmt.Println("    /clear        - Clear current conversation (start new session)")
		fmt.Println()
		fmt.Println("  Plans:")
		fmt.Println("    /plan         - Show active task plan")
		fmt.Println()
		fmt.Println("  Other:")
		fmt.Println("    /usage        - Show token usage statistics")
		fmt.Println("    /help         - Show this help message")
		fmt.Println("    exit          - Exit Tara Code")
		fmt.Println()
		fmt.Println("  File References (requires /init):")
		fmt.Println("    @<Tab>   - Show file completion list")
		fmt.Println("    @path    - Include specific file (e.g., @src/main.go)")
		fmt.Println()

	case "/usage":
		usage := (*asst).GetUsage()
		r := ui.NewRenderer()
		fmt.Println(r.FormatUsage(usage))

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
			// Start new session
			if err := (*asst).NewSession(""); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
				return
			}
			fmt.Println("Started new conversation session.")
			fmt.Println()
		} else if args[0] == "load" && len(args) > 1 {
			// Load session by ID
			sessionID := args[1]
			if err := (*asst).LoadSession(sessionID); err != nil {
				fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
				return
			}
			session := (*asst).GetSession()
			fmt.Printf("Loaded session with %d messages.\n", len(session.Messages))
			fmt.Println()
		} else {
			fmt.Println("Usage: /session [new|load <id>]")
			fmt.Println()
		}

	case "/sessions":
		handleListSessions(*asst)

	case "/status":
		handleStatus(*asst, workingDir)

	case "/plan":
		handleShowPlan(*asst)

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		fmt.Println("Type '/help' for available commands.")
		fmt.Println()
	}
}

// handleSessionInfo displays current session information
func handleSessionInfo(asst *assistant.Assistant) {
	session := asst.GetSession()
	if session == nil {
		fmt.Println("No active session.")
		fmt.Println()
		return
	}

	fmt.Println("Current Session:")
	fmt.Printf("  ID: %s\n", session.ID[:8])
	fmt.Printf("  Messages: %d\n", len(session.Messages))
	fmt.Printf("  Created: %s\n", session.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Updated: %s\n", session.UpdatedAt.Format(time.RFC3339))
	fmt.Println()
}

// handleListSessions displays all available sessions
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
	for _, s := range sessions {
		active := ""
		if currentSession != nil && s.ID == currentSession.ID {
			active = " (active)"
		}
		fmt.Printf("  %s - %d messages - %s%s\n",
			s.ID[:8], s.MessageCount, s.UpdatedAt.Format("2006-01-02 15:04"), active)
	}
	fmt.Println()
	fmt.Println("Use '/session load <id>' to load a session.")
	fmt.Println()
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
		fmt.Printf("  Session: %s (%d messages)\n", session.ID[:8], len(session.Messages))
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
