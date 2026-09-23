// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdStatus is the /status command: show project and session status.
func (r *repl) cmdStatus(_ []string) {
	handleStatus(r.asst, r.absDir)
}

// cmdSession is the /session command: show or manage the current session.
func (r *repl) cmdSession(args []string) {
	if len(args) == 0 {
		// Show current session info
		handleSessionInfo(r.asst)
	} else if args[0] == "new" {
		// Start new session with optional name
		sessionName := ""
		if len(args) > 1 {
			// Join remaining args as the session name (support quoted names)
			sessionName = strings.Join(args[1:], " ")
			// Remove surrounding quotes if present
			sessionName = strings.Trim(sessionName, "\"'")
		}
		if err := r.asst.NewSession(sessionName); err != nil {
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
		if err := r.asst.LoadSession(sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
			return
		}
		session := r.asst.GetSession()
		if session == nil {
			fmt.Fprintf(os.Stderr, "Error: session loaded but not accessible\n")
			return
		}
		fmt.Printf("Loaded session with %d messages.\n", len(session.Messages))
		fmt.Println()
	} else if args[0] == "delete" && len(args) > 1 {
		// Delete session by ID
		sessionID := args[1]
		handleDeleteSession(r.asst, sessionID)
	} else if args[0] == "rename" && len(args) > 2 {
		// Rename session: /session rename <id> <new name>
		sessionID := args[1]
		newName := strings.Join(args[2:], " ")
		newName = strings.Trim(newName, "\"'")
		handleRenameSession(r.asst, sessionID, newName)
	} else {
		fmt.Println("Usage: /session [new [\"name\"]|load <id>|delete <id>|rename <id> <name>]")
		fmt.Println()
	}
}

// cmdSessions is the /sessions command: list all sessions.
func (r *repl) cmdSessions(_ []string) {
	handleListSessions(r.asst)
}

// cmdClear is the /clear command: clear the conversation and start a new session.
func (r *repl) cmdClear(_ []string) {
	if err := r.asst.NewSession(""); err != nil {
		// Fallback to creating new assistant
		newAsst, err := assistant.New(r.host, r.apiKey, r.model, r.vendor, r.streaming, r.spinner, toolConfig(r.renderer))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing: %v\n", err)
			return
		}
		r.asst = newAsst
	}
	fmt.Println("Conversation cleared. Started new session.")
	fmt.Println()
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

	// Check if session exists before asking for confirmation
	if _, err := storage.GetSession(sessionID); err != nil {
		fmt.Fprintf(os.Stderr, "Error: session not found: %s\n", sessionID)
		fmt.Println()
		return
	}

	// Confirm deletion
	fmt.Printf("Delete session %s? [y/N]: ", ui.TruncateID(sessionID, 0))
	var response string
	_, _ = fmt.Scanln(&response)

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
