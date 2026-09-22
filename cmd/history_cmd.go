// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/history"
)

// cmdHistory is the /history command: file operation history.
func (r *repl) cmdHistory(args []string) {
	handleHistory(r.history, args)
}

// cmdUndo is the /undo command: undo file modifications.
func (r *repl) cmdUndo(args []string) {
	handleUndo(r.history, args)
}

// cmdDiff is the /diff command: show or export session changes.
func (r *repl) cmdDiff(args []string) {
	handleDiff(r.history, args, r.absDir)
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
	statsLine := fmt.Sprintf(
		"Total: %d  |  Undoable: %d  |  Undone: %d", stats["total"], stats["undoable"], stats["undone"])
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

	//nolint:gosec // writes an export the user asked for, inside the project sandbox
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
