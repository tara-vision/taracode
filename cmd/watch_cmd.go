package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/ui"
	"github.com/tara-vision/taracode/internal/watch"
)

var (
	watchHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#A78BFA"))

	watchActiveStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2dd4bf"))

	watchInactiveStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#94A3B8"))

	watchErrorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FCA5A5")).
		Bold(true)

	watchWarningStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FCD34D"))

	watchImprovementStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#93C5FD"))
)

// handleWatchCommand handles the /watch command and subcommands
func handleWatchCommand(args []string, asst *assistant.Assistant, monitor **watch.WatchMonitor, tempDir string) {
	if len(args) == 0 {
		showWatchHelp()
		return
	}

	subCmd := strings.ToLower(args[0])

	switch subCmd {
	case "this":
		handleWatchThis(asst)
	case "start":
		handleWatchStart(asst, monitor, tempDir)
	case "stop":
		handleWatchStop(monitor)
	case "status":
		handleWatchStatus(monitor)
	case "help":
		showWatchHelp()
	default:
		fmt.Printf("Unknown watch subcommand: %s\n", subCmd)
		fmt.Println("Use '/watch help' for available commands")
		fmt.Println()
	}
}

// handleWatchThis performs a one-time screen capture and analysis
func handleWatchThis(asst *assistant.Assistant) {
	fmt.Println()
	fmt.Println(watchHeaderStyle.Render("Screen Capture & Analysis"))
	fmt.Println()

	// Show display info
	displayInfo := watch.GetDisplayInfo()
	fmt.Printf("Capturing: %s\n", displayInfo)
	fmt.Println()

	// Create analyze function that uses the assistant
	analyzeFunc := func(prompt string, images []*assistant.ImageData) (string, error) {
		return asst.AnalyzeImages(prompt, images)
	}

	// Create temp directory for screenshots
	tempDir := os.TempDir()

	// Show spinner while analyzing
	spinner := ui.NewStatusLineSpinner()
	spinner.Start("Analyzing screen...")

	// Perform one-time analysis
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := watch.AnalyzeOnce(ctx, analyzeFunc, tempDir)

	spinner.Stop()

	if err != nil {
		fmt.Printf("%s %s\n", ui.IconError, watchErrorStyle.Render(err.Error()))
		fmt.Println()

		// Check for common errors
		if strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "Screen Recording") {
			fmt.Println("To grant permission:")
			fmt.Println("  1. Open System Preferences > Privacy & Security > Screen Recording")
			fmt.Println("  2. Enable screen recording for your terminal application")
			fmt.Println("  3. Restart your terminal and try again")
			fmt.Println()
		}
		return
	}

	// Display results
	displayAnalysisResult(result)
}

// handleWatchStart starts continuous screen monitoring
func handleWatchStart(asst *assistant.Assistant, monitor **watch.WatchMonitor, tempDir string) {
	// Check if already running
	if *monitor != nil && (*monitor).IsRunning() {
		fmt.Println("Screen monitoring is already running.")
		fmt.Println("Use '/watch stop' to stop, or '/watch status' to check status.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Println(watchHeaderStyle.Render("Starting Screen Monitor"))
	fmt.Println()

	// Show display info
	displayInfo := watch.GetDisplayInfo()
	fmt.Printf("Watching: %s\n", displayInfo)
	fmt.Println("Mode: Continuous (analyzing on significant changes)")
	fmt.Println()

	// Create analyze function
	analyzeFunc := func(prompt string, images []*assistant.ImageData) (string, error) {
		return asst.AnalyzeImages(prompt, images)
	}

	// Create monitor with default config
	config := watch.DefaultConfig()
	*monitor = watch.NewWatchMonitor(config, analyzeFunc, tempDir)

	// Start monitoring
	if err := (*monitor).Start(); err != nil {
		fmt.Printf("%s Failed to start monitor: %s\n", ui.IconError, err)
		fmt.Println()
		return
	}

	fmt.Printf("%s Monitor started\n", watchActiveStyle.Render(ui.IconSuccess))
	fmt.Println("I'll notify you when I spot issues.")
	fmt.Println()
	fmt.Println("Use '/watch stop' to stop monitoring")
	fmt.Println("Use '/watch status' to check current status")
	fmt.Println()

	// Start a goroutine to handle incoming results
	go handleWatchResults(*monitor)
}

// handleWatchResults processes results from the monitor
func handleWatchResults(monitor *watch.WatchMonitor) {
	for result := range monitor.Results() {
		if result != nil && result.HasFindings() {
			fmt.Println()
			fmt.Println(watchHeaderStyle.Render("Spotted an issue!"))
			displayAnalysisResult(result)
		}
	}
}

// handleWatchStop stops the monitoring loop
func handleWatchStop(monitor **watch.WatchMonitor) {
	if *monitor == nil || !(*monitor).IsRunning() {
		fmt.Println("Screen monitoring is not running.")
		fmt.Println()
		return
	}

	// Get final stats before stopping
	status := (*monitor).Status()

	(*monitor).Stop()
	*monitor = nil

	fmt.Println()
	fmt.Printf("%s Monitor stopped\n", watchInactiveStyle.Render(ui.IconSuccess))
	fmt.Printf("Session: %d captures, %d analyses in %s\n",
		status.TotalCaptures, status.TotalAnalyses, formatDuration(status.Uptime))
	fmt.Println()
}

// handleWatchStatus shows the current monitoring state
func handleWatchStatus(monitor **watch.WatchMonitor) {
	fmt.Println()
	fmt.Println(watchHeaderStyle.Render("Watch Monitor Status"))
	fmt.Println()

	if *monitor == nil || !(*monitor).IsRunning() {
		fmt.Printf("Status: %s\n", watchInactiveStyle.Render("idle"))
		fmt.Println("Use '/watch start' to begin monitoring")
		fmt.Println()
		return
	}

	status := (*monitor).Status()

	// Status
	stateStr := watchActiveStyle.Render(status.State.String())
	fmt.Printf("Status: %s\n", stateStr)

	// Displays
	fmt.Printf("Displays: %d\n", status.DisplayCount)

	// Stats
	fmt.Printf("Uptime: %s\n", formatDuration(status.Uptime))
	fmt.Printf("Captures: %d\n", status.TotalCaptures)
	fmt.Printf("Analyses: %d (rate: %d/min)\n", status.TotalAnalyses, status.AnalysesThisMin)

	// Last activity
	if !status.LastCapture.IsZero() {
		fmt.Printf("Last capture: %s\n", status.LastCapture.Format("15:04:05"))
	}
	if !status.LastAnalysis.IsZero() {
		fmt.Printf("Last analysis: %s\n", status.LastAnalysis.Format("15:04:05"))
	}

	fmt.Println()
}

// showWatchHelp displays help for watch commands
func showWatchHelp() {
	fmt.Println()
	fmt.Println(watchHeaderStyle.Render("Watch Commands"))
	fmt.Println()
	fmt.Println("  /watch this      Capture and analyze all screens now")
	fmt.Println("  /watch start     Start continuous screen monitoring")
	fmt.Println("  /watch stop      Stop monitoring")
	fmt.Println("  /watch status    Show current monitoring state")
	fmt.Println("  /watch help      Show this help")
	fmt.Println()
	fmt.Println("The /watch command captures your screen and uses AI vision to detect:")
	fmt.Println("  - Error messages and stack traces")
	fmt.Println("  - Warnings and alerts")
	fmt.Println("  - Improvement opportunities")
	fmt.Println()
	fmt.Println("Note: Requires Screen Recording permission in System Preferences.")
	fmt.Println()
}

// displayAnalysisResult shows the analysis results in a formatted way
func displayAnalysisResult(result *watch.AnalysisResult) {
	if result == nil {
		return
	}

	if !result.HasFindings() {
		fmt.Println(watchActiveStyle.Render("No issues detected."))
		fmt.Println()
		return
	}

	for _, finding := range result.Findings {
		var prefix, style string
		switch finding.Type {
		case watch.FindingError:
			prefix = "[ERROR]"
			style = watchErrorStyle.Render(prefix)
		case watch.FindingWarning:
			prefix = "[WARNING]"
			style = watchWarningStyle.Render(prefix)
		case watch.FindingImprovement:
			prefix = "[IMPROVEMENT]"
			style = watchImprovementStyle.Render(prefix)
		default:
			prefix = "[INFO]"
			style = prefix
		}

		fmt.Printf("%s %s\n", style, finding.Description)
		if finding.Suggestion != "" {
			fmt.Printf("  %s %s\n", ui.IconArrow, finding.Suggestion)
		}
	}

	fmt.Println()

	// Summary line
	errors := result.ErrorCount()
	warnings := result.WarningCount()
	improvements := len(result.Findings) - errors - warnings

	parts := []string{}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", errors))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warnings", warnings))
	}
	if improvements > 0 {
		parts = append(parts, fmt.Sprintf("%d suggestions", improvements))
	}

	if len(parts) > 0 {
		fmt.Printf("Summary: %s (analyzed in %s)\n", strings.Join(parts, ", "), result.AnalysisTime.Round(time.Millisecond))
		fmt.Println()
	}

	// Offer to help
	if errors > 0 {
		fmt.Println("Would you like me to help with any of these issues?")
		fmt.Println()
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}
