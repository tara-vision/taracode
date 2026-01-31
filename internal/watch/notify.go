package watch

import (
	"fmt"
	"os/exec"
	"strings"
)

// SendNotification sends a macOS system notification
func SendNotification(title, message string) error {
	// Escape quotes in the message and title
	title = escapeAppleScript(title)
	message = escapeAppleScript(message)

	script := fmt.Sprintf(`display notification "%s" with title "%s"`, message, title)
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// SendNotificationWithSound sends a macOS notification with sound
func SendNotificationWithSound(title, message, sound string) error {
	title = escapeAppleScript(title)
	message = escapeAppleScript(message)

	if sound == "" {
		sound = "default"
	}

	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "%s"`, message, title, sound)
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// SendNotificationWithSubtitle sends a notification with a subtitle
func SendNotificationWithSubtitle(title, subtitle, message string) error {
	title = escapeAppleScript(title)
	subtitle = escapeAppleScript(subtitle)
	message = escapeAppleScript(message)

	script := fmt.Sprintf(`display notification "%s" with title "%s" subtitle "%s"`, message, title, subtitle)
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// SendFindingNotification sends a notification for a watch finding
func SendFindingNotification(finding Finding) error {
	var title string
	switch finding.Type {
	case FindingError:
		title = "taracode: Error Detected"
	case FindingWarning:
		title = "taracode: Warning"
	case FindingImprovement:
		title = "taracode: Suggestion"
	default:
		title = "taracode"
	}

	message := finding.Description
	if len(message) > 100 {
		message = message[:97] + "..."
	}

	return SendNotification(title, message)
}

// SendAnalysisNotification sends a summary notification for analysis results
func SendAnalysisNotification(result *AnalysisResult) error {
	if result == nil || !result.HasFindings() {
		return nil
	}

	errors := result.ErrorCount()
	warnings := result.WarningCount()

	var title, message string

	if errors > 0 {
		title = "taracode: Issues Detected"
		if errors == 1 {
			message = "1 error found on screen"
		} else {
			message = fmt.Sprintf("%d errors found on screen", errors)
		}
		if warnings > 0 {
			message += fmt.Sprintf(", %d warnings", warnings)
		}
		return SendNotificationWithSound(title, message, "Basso")
	}

	if warnings > 0 {
		title = "taracode: Warnings"
		if warnings == 1 {
			message = "1 warning detected"
		} else {
			message = fmt.Sprintf("%d warnings detected", warnings)
		}
		return SendNotification(title, message)
	}

	// Only improvements
	title = "taracode: Suggestions"
	count := len(result.Findings)
	if count == 1 {
		message = "1 improvement suggestion"
	} else {
		message = fmt.Sprintf("%d improvement suggestions", count)
	}
	return SendNotification(title, message)
}

// escapeAppleScript escapes special characters for AppleScript strings
func escapeAppleScript(s string) string {
	// Replace backslashes first, then quotes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// CheckNotificationPermissions checks if notifications are likely to work
// Note: This is a best-effort check; macOS doesn't provide a programmatic way to verify
func CheckNotificationPermissions() bool {
	// Try to run a silent osascript command to check if we have permission
	cmd := exec.Command("osascript", "-e", `tell application "System Events" to return name of current application`)
	err := cmd.Run()
	return err == nil
}
