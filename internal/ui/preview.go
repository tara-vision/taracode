package ui

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"
)

// EditPreview represents a preview of an edit operation
type EditPreview struct {
	FilePath     string
	OldContent   string
	NewContent   string
	OldString    string
	NewString    string
	LinesAdded   int
	LinesRemoved int
}

// EditPreviewChoice represents the user's choice after seeing a preview
type EditPreviewChoice int

const (
	EditPreviewApply EditPreviewChoice = iota
	EditPreviewCancel
	EditPreviewBackupThenApply
)

// GenerateUnifiedDiff creates a simple unified diff between old and new content
func GenerateUnifiedDiff(oldContent, newContent, oldString, newString string) string {
	var sb strings.Builder

	// Find the location of the change for context
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Find first differing line
	startLine := 0
	for i := 0; i < len(oldLines) && i < len(newLines); i++ {
		if oldLines[i] != newLines[i] {
			startLine = i
			break
		}
	}

	// Show context around the change (3 lines before and after)
	contextBefore := 3
	contextAfter := 3

	displayStart := startLine - contextBefore
	if displayStart < 0 {
		displayStart = 0
	}

	// Calculate how many lines changed
	oldStringLines := strings.Count(oldString, "\n") + 1
	newStringLines := strings.Count(newString, "\n") + 1

	displayEndOld := startLine + oldStringLines + contextAfter
	if displayEndOld > len(oldLines) {
		displayEndOld = len(oldLines)
	}

	displayEndNew := startLine + newStringLines + contextAfter
	if displayEndNew > len(newLines) {
		displayEndNew = len(newLines)
	}

	// Write diff header
	sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
		displayStart+1, displayEndOld-displayStart,
		displayStart+1, displayEndNew-displayStart))

	// Write context before
	for i := displayStart; i < startLine && i < len(oldLines); i++ {
		sb.WriteString(fmt.Sprintf(" %s\n", oldLines[i]))
	}

	// Write removed lines (old)
	for i := startLine; i < startLine+oldStringLines && i < len(oldLines); i++ {
		sb.WriteString(fmt.Sprintf("-%s\n", oldLines[i]))
	}

	// Write added lines (new)
	for i := startLine; i < startLine+newStringLines && i < len(newLines); i++ {
		sb.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
	}

	// Write context after
	afterStart := startLine + oldStringLines
	for i := afterStart; i < displayEndOld && i < len(oldLines); i++ {
		sb.WriteString(fmt.Sprintf(" %s\n", oldLines[i]))
	}

	return sb.String()
}

// DisplayEditPreview shows the edit preview to the user and gets their choice
func DisplayEditPreview(preview *EditPreview) EditPreviewChoice {
	// Calculate stats
	oldLineCount := strings.Count(preview.OldString, "\n") + 1
	newLineCount := strings.Count(preview.NewString, "\n") + 1

	linesAdded := 0
	linesRemoved := 0
	if newLineCount > oldLineCount {
		linesAdded = newLineCount - oldLineCount
	} else if oldLineCount > newLineCount {
		linesRemoved = oldLineCount - newLineCount
	}

	// Generate diff
	diff := GenerateUnifiedDiff(preview.OldContent, preview.NewContent, preview.OldString, preview.NewString)

	// Display the preview box
	fmt.Println()
	fmt.Println(WarningStyle.Render("┌─────────────────────────────────────────────────────────────────┐"))
	fmt.Println(WarningStyle.Render(fmt.Sprintf("│ Edit Preview: %s", TruncateString(preview.FilePath, 48))))
	fmt.Println(WarningStyle.Render("├─────────────────────────────────────────────────────────────────┤"))

	// Display diff with colors
	for _, line := range strings.Split(diff, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			fmt.Println(SessionStyle.Render("│ " + line))
		case strings.HasPrefix(line, "-"):
			fmt.Println(ErrorStyle.Render("│ " + line))
		case strings.HasPrefix(line, "+"):
			fmt.Println(SuccessStyle.Render("│ " + line))
		default:
			fmt.Println(Subtle.Render("│ " + line))
		}
	}

	fmt.Println(WarningStyle.Render("└─────────────────────────────────────────────────────────────────┘"))

	// Show stats
	var statsMsg string
	if linesAdded > 0 && linesRemoved > 0 {
		statsMsg = fmt.Sprintf("+%d lines, -%d lines", linesAdded, linesRemoved)
	} else if linesAdded > 0 {
		statsMsg = fmt.Sprintf("+%d lines", linesAdded)
	} else if linesRemoved > 0 {
		statsMsg = fmt.Sprintf("-%d lines", linesRemoved)
	} else {
		statsMsg = "modified"
	}
	fmt.Println(Subtle.Render(fmt.Sprintf("  Changes: %s", statsMsg)))
	fmt.Println()

	// Prompt for action
	type choice struct {
		Label string
		Short string
	}

	choices := []choice{
		{Label: "Yes, apply this edit", Short: "y"},
		{Label: "No, cancel this edit", Short: "n"},
		{Label: "Save backup first, then apply", Short: "b"},
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "\U0001F449 {{ .Label | cyan }}",
		Inactive: "   {{ .Label }}",
		Selected: "\U00002705 {{ .Label | green }}",
	}

	prompt := promptui.Select{
		Label:     "Apply this edit?",
		Items:     choices,
		Templates: templates,
		Size:      3,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		// User cancelled - treat as cancel
		return EditPreviewCancel
	}

	switch idx {
	case 0:
		return EditPreviewApply
	case 1:
		return EditPreviewCancel
	case 2:
		return EditPreviewBackupThenApply
	}

	return EditPreviewCancel
}

// DisplayEditApplied shows confirmation after an edit is applied
func DisplayEditApplied(filePath string, linesAdded, linesRemoved int) {
	var statsMsg string
	if linesAdded > 0 && linesRemoved > 0 {
		statsMsg = fmt.Sprintf("+%d, -%d lines", linesAdded, linesRemoved)
	} else if linesAdded > 0 {
		statsMsg = fmt.Sprintf("+%d lines", linesAdded)
	} else if linesRemoved > 0 {
		statsMsg = fmt.Sprintf("-%d lines", linesRemoved)
	} else {
		statsMsg = "modified"
	}
	fmt.Println(SuccessStyle.Render(fmt.Sprintf("%s Applied edit to %s (%s)", IconSuccess, filePath, statsMsg)))
}

// DisplayEditCancelled shows message when edit is cancelled
func DisplayEditCancelled(filePath string) {
	fmt.Println(WarningStyle.Render(fmt.Sprintf("%s Edit cancelled: %s", IconWarning, filePath)))
}

// DisplayBackupCreated shows confirmation of backup creation
func DisplayBackupCreated(backupPath string) {
	fmt.Println(Subtle.Render(fmt.Sprintf("  Backup saved: %s", backupPath)))
}
