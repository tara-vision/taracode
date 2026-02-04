package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/manifoldco/promptui"
	"github.com/tara-vision/taracode/internal/ui"
	"github.com/tara-vision/taracode/internal/upgrade"
)

var (
	upgradeHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#A78BFA"))

	upgradeVersionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2dd4bf")).
				Bold(true)

	upgradeNewVersionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FCD34D")).
				Bold(true)

	upgradeInfoStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#94A3B8"))

	upgradeSuccessStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2dd4bf")).
				Bold(true)

	upgradeChangelogStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#93C5FD")).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#94A3B8")).
				Padding(1, 2).
				MarginTop(1).
				MarginBottom(1)
)

// handleUpgradeCommand handles the /upgrade command and subcommands
func handleUpgradeCommand(args []string) {
	if len(args) == 0 {
		// Default: check for updates
		handleUpgradeCheck(true)
		return
	}

	subCmd := strings.ToLower(args[0])

	switch subCmd {
	case "check":
		handleUpgradeCheck(true)
	case "now":
		handleUpgradeNow()
	case "skip":
		handleUpgradeSkip()
	case "changelog":
		handleUpgradeChangelog()
	case "status":
		handleUpgradeStatus()
	case "help":
		showUpgradeHelp()
	default:
		fmt.Printf("Unknown upgrade subcommand: %s\n", subCmd)
		fmt.Println("Use '/upgrade help' for available commands")
		fmt.Println()
	}
}

// handleUpgradeCheck checks for available updates
func handleUpgradeCheck(verbose bool) {
	fmt.Println()
	fmt.Println(upgradeHeaderStyle.Render("Checking for Updates"))
	fmt.Println()

	checker := upgrade.NewChecker(Version)
	result, err := checker.CheckForUpdate()
	if err != nil {
		fmt.Printf("%s Failed to check for updates: %v\n", ui.IconError, err)
		fmt.Println()
		return
	}

	fmt.Printf("  Current version:  %s\n", upgradeVersionStyle.Render(result.CurrentVersion))
	fmt.Printf("  Latest version:   %s\n", upgradeNewVersionStyle.Render(result.LatestVersion))
	fmt.Printf("  Install method:   %s\n", upgradeInfoStyle.Render(result.InstallMethod))
	fmt.Println()

	if !result.UpdateAvailable {
		fmt.Printf("%s You're running the latest version!\n", ui.IconSuccess)
		fmt.Println()
		return
	}

	if result.SkippedByUser {
		fmt.Printf("%s Update available (you previously skipped this version)\n", ui.IconInfo)
		fmt.Println("  Use '/upgrade now' to upgrade anyway")
		fmt.Println()
		return
	}

	fmt.Printf("%s A new version is available!\n", ui.IconStar)
	fmt.Println()

	// Show changelog preview
	if result.Changelog != "" && verbose {
		fmt.Println(upgradeInfoStyle.Render("Changelog:"))
		changelog := truncateChangelog(result.Changelog, 500)
		fmt.Println(upgradeChangelogStyle.Render(changelog))
	}

	// Show upgrade command
	method := upgrade.InstallMethod(result.InstallMethod)
	fmt.Println("  To upgrade, run:")
	fmt.Printf("    %s\n", upgradeVersionStyle.Render(upgrade.GetUpgradeCommand(method)))
	fmt.Println()
	fmt.Println("  Or use '/upgrade now' to upgrade automatically")
	fmt.Println()
}

// handleUpgradeNow performs the upgrade
func handleUpgradeNow() {
	fmt.Println()
	fmt.Println(upgradeHeaderStyle.Render("Upgrading taracode"))
	fmt.Println()

	checker := upgrade.NewChecker(Version)
	result, err := checker.CheckForUpdate()
	if err != nil {
		fmt.Printf("%s Failed to check for updates: %v\n", ui.IconError, err)
		fmt.Println()
		return
	}

	if !result.UpdateAvailable {
		fmt.Printf("%s You're already running the latest version (%s)\n",
			ui.IconSuccess, upgradeVersionStyle.Render(result.CurrentVersion))
		fmt.Println()
		return
	}

	fmt.Printf("  Upgrading from %s to %s\n",
		upgradeVersionStyle.Render(result.CurrentVersion),
		upgradeNewVersionStyle.Render(result.LatestVersion))
	fmt.Println()

	// Confirm upgrade
	prompt := promptui.Prompt{
		Label:     "Proceed with upgrade",
		IsConfirm: true,
		Default:   "y",
	}

	_, err = prompt.Run()
	if err != nil {
		fmt.Println("Upgrade cancelled")
		fmt.Println()
		return
	}

	fmt.Println()

	// Perform upgrade
	installer := upgrade.NewInstaller(checker)
	if err := installer.Upgrade(result); err != nil {
		fmt.Printf("%s Upgrade failed: %v\n", ui.IconError, err)
		fmt.Println()
		fmt.Println("You can try upgrading manually:")
		method := upgrade.InstallMethod(result.InstallMethod)
		fmt.Printf("  %s\n", upgrade.GetUpgradeCommand(method))
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("%s Successfully upgraded to %s!\n",
		ui.IconSuccess, upgradeNewVersionStyle.Render(result.LatestVersion))
	fmt.Println()
	fmt.Println(upgradeInfoStyle.Render("Please restart taracode to use the new version."))
	fmt.Println()
}

// handleUpgradeSkip skips the current available update
func handleUpgradeSkip() {
	checker := upgrade.NewChecker(Version)
	result, err := checker.CheckForUpdate()
	if err != nil {
		fmt.Printf("%s Failed to check for updates: %v\n", ui.IconError, err)
		fmt.Println()
		return
	}

	if !result.UpdateAvailable {
		fmt.Printf("%s No update available to skip\n", ui.IconInfo)
		fmt.Println()
		return
	}

	if err := checker.SkipVersion(result.LatestVersion); err != nil {
		fmt.Printf("%s Failed to skip version: %v\n", ui.IconError, err)
		fmt.Println()
		return
	}

	fmt.Printf("%s Skipped version %s\n", ui.IconSuccess, result.LatestVersion)
	fmt.Println("  You won't be notified about this version again")
	fmt.Println("  Use '/upgrade now' to upgrade anyway")
	fmt.Println()
}

// handleUpgradeChangelog shows the full changelog
func handleUpgradeChangelog() {
	fmt.Println()
	fmt.Println(upgradeHeaderStyle.Render("Release Changelog"))
	fmt.Println()

	checker := upgrade.NewChecker(Version)
	result, err := checker.CheckForUpdate()
	if err != nil {
		fmt.Printf("%s Failed to fetch changelog: %v\n", ui.IconError, err)
		fmt.Println()
		return
	}

	if result.ReleaseInfo == nil {
		fmt.Println("No changelog available")
		fmt.Println()
		return
	}

	fmt.Printf("  Version: %s\n", upgradeNewVersionStyle.Render(result.LatestVersion))
	if !result.ReleaseInfo.PublishedAt.IsZero() {
		fmt.Printf("  Released: %s\n", upgradeInfoStyle.Render(result.ReleaseInfo.PublishedAt.Format("2006-01-02")))
	}
	fmt.Println()

	if result.Changelog != "" {
		fmt.Println(result.Changelog)
	} else {
		fmt.Println("No changelog available")
	}

	if result.ReleaseInfo.HTMLURL != "" {
		fmt.Println()
		fmt.Printf("  View on GitHub: %s\n", upgradeInfoStyle.Render(result.ReleaseInfo.HTMLURL))
	}
	fmt.Println()
}

// handleUpgradeStatus shows upgrade state
func handleUpgradeStatus() {
	fmt.Println()
	fmt.Println(upgradeHeaderStyle.Render("Upgrade Status"))
	fmt.Println()

	checker := upgrade.NewChecker(Version)
	state := checker.LoadState()

	fmt.Printf("  Current version:    %s\n", upgradeVersionStyle.Render(Version))
	fmt.Printf("  Install method:     %s\n", upgradeInfoStyle.Render(string(checker.DetectInstallMethod())))

	if !state.LastCheckTime.IsZero() {
		fmt.Printf("  Last check:         %s\n", upgradeInfoStyle.Render(state.LastCheckTime.Format("2006-01-02 15:04:05")))
	} else {
		fmt.Printf("  Last check:         %s\n", upgradeInfoStyle.Render("never"))
	}

	if state.LastCheckVersion != "" {
		fmt.Printf("  Latest known:       %s\n", upgradeInfoStyle.Render(state.LastCheckVersion))
	}

	if state.SkippedVersion != "" {
		fmt.Printf("  Skipped version:    %s\n", upgradeInfoStyle.Render(state.SkippedVersion))
	}

	fmt.Println()
}

// showUpgradeHelp displays help for the upgrade command
func showUpgradeHelp() {
	fmt.Println()
	fmt.Println(upgradeHeaderStyle.Render("Upgrade Commands"))
	fmt.Println()
	fmt.Println("  /upgrade           Check for available updates (default)")
	fmt.Println("  /upgrade check     Check for available updates")
	fmt.Println("  /upgrade now       Download and install the latest version")
	fmt.Println("  /upgrade skip      Skip the current available update")
	fmt.Println("  /upgrade changelog Show full release notes")
	fmt.Println("  /upgrade status    Show upgrade state information")
	fmt.Println("  /upgrade help      Show this help message")
	fmt.Println()
}

// ShowUpdateBanner displays a banner when an update is available (called at startup)
func ShowUpdateBanner(result *upgrade.CheckResult) {
	if result == nil || !result.UpdateAvailable || result.SkippedByUser {
		return
	}

	fmt.Println()
	bannerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FCD34D")).
		Padding(0, 2)

	content := fmt.Sprintf(
		"%s Update available: %s %s %s\n   Run '/upgrade' for details or '/upgrade now' to install",
		ui.IconStar,
		upgradeVersionStyle.Render(result.CurrentVersion),
		upgradeInfoStyle.Render("->"),
		upgradeNewVersionStyle.Render(result.LatestVersion),
	)

	fmt.Println(bannerStyle.Render(content))
	fmt.Println()
}

// CheckForUpdateAsync checks for updates in the background
// Returns the result via the provided channel
func CheckForUpdateAsync(currentVersion string, resultChan chan<- *upgrade.CheckResult) {
	go func() {
		checker := upgrade.NewChecker(currentVersion)

		// Only check if enough time has passed (24 hours by default)
		if !checker.ShouldCheck(24 * time.Hour) {
			// Load last known state if we have a recent check
			state := checker.LoadState()
			if state.LastCheckVersion != "" {
				// Check if update is available based on stored state
				current := strings.TrimPrefix(currentVersion, "v")
				latest := strings.TrimPrefix(state.LastCheckVersion, "v")
				if upgrade.CompareVersions(latest, current) > 0 {
					resultChan <- &upgrade.CheckResult{
						CurrentVersion:  currentVersion,
						LatestVersion:   state.LastCheckVersion,
						UpdateAvailable: true,
						SkippedByUser:   state.SkippedVersion == state.LastCheckVersion,
					}
					return
				}
			}
			resultChan <- nil
			return
		}

		result, err := checker.CheckForUpdate()
		if err != nil {
			resultChan <- nil
			return
		}

		resultChan <- result
	}()
}

// truncateChangelog truncates the changelog to a reasonable length
func truncateChangelog(changelog string, maxLen int) string {
	if len(changelog) <= maxLen {
		return changelog
	}

	// Try to cut at a newline
	truncated := changelog[:maxLen]
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > maxLen/2 {
		truncated = truncated[:lastNewline]
	}

	return truncated + "\n..."
}
