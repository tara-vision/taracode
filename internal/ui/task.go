package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/tara-vision/taracode/internal/storage"
)

// Task display styles
var (
	// Task header style
	TaskHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(Primary).
		Padding(0, 1)

	// Task box style
	TaskBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Primary).
		Padding(0, 1).
		Width(70)

	// Step status styles
	StepPending = lipgloss.NewStyle().
		Foreground(Muted)

	StepRunning = lipgloss.NewStyle().
		Foreground(Info).
		Bold(true)

	StepCompleted = lipgloss.NewStyle().
		Foreground(Success)

	StepFailed = lipgloss.NewStyle().
		Foreground(Error)

	StepSkipped = lipgloss.NewStyle().
		Foreground(Warning)

	// Step number style
	StepNumber = lipgloss.NewStyle().
		Foreground(Muted).
		Width(4)

	// Step name style
	StepName = lipgloss.NewStyle().
		Bold(true)

	// Task status styles
	TaskStatusPending = lipgloss.NewStyle().
		Foreground(Muted).
		Bold(true)

	TaskStatusRunning = lipgloss.NewStyle().
		Foreground(Info).
		Bold(true)

	TaskStatusCompleted = lipgloss.NewStyle().
		Foreground(Success).
		Bold(true)

	TaskStatusFailed = lipgloss.NewStyle().
		Foreground(Error).
		Bold(true)

	TaskStatusPaused = lipgloss.NewStyle().
		Foreground(Warning).
		Bold(true)
)

// Step status icons
const (
	IconStepPending   = "○"
	IconStepRunning   = "◐"
	IconStepCompleted = "●"
	IconStepFailed    = "✗"
	IconStepSkipped   = "○"
	IconCheckpoint    = "⚑"
)

// GetStepIcon returns the icon for a step status
func GetStepIcon(status storage.TaskStepStatus) string {
	switch status {
	case storage.StepStatusPending:
		return IconStepPending
	case storage.StepStatusRunning, storage.StepStatusVerifying, storage.StepStatusRetrying:
		return IconStepRunning
	case storage.StepStatusCompleted:
		return IconStepCompleted
	case storage.StepStatusFailed:
		return IconStepFailed
	case storage.StepStatusSkipped:
		return IconStepSkipped
	default:
		return IconStepPending
	}
}

// GetStepStyle returns the style for a step status
func GetStepStyle(status storage.TaskStepStatus) lipgloss.Style {
	switch status {
	case storage.StepStatusPending:
		return StepPending
	case storage.StepStatusRunning, storage.StepStatusVerifying, storage.StepStatusRetrying:
		return StepRunning
	case storage.StepStatusCompleted:
		return StepCompleted
	case storage.StepStatusFailed:
		return StepFailed
	case storage.StepStatusSkipped:
		return StepSkipped
	default:
		return StepPending
	}
}

// GetTaskStatusStyle returns the style for a task status
func GetTaskStatusStyle(status storage.TaskExecutionStatus) lipgloss.Style {
	switch status {
	case storage.TaskExecStatusPending, storage.TaskExecStatusPlanning:
		return TaskStatusPending
	case storage.TaskExecStatusRunning:
		return TaskStatusRunning
	case storage.TaskExecStatusCompleted:
		return TaskStatusCompleted
	case storage.TaskExecStatusFailed, storage.TaskExecStatusAborted, storage.TaskExecStatusRolledBack:
		return TaskStatusFailed
	case storage.TaskExecStatusPaused:
		return TaskStatusPaused
	default:
		return TaskStatusPending
	}
}

// DisplayTaskPlan shows the task plan for user approval
func DisplayTaskPlan(task *storage.TaskExecution) {
	fmt.Println()

	// Header
	header := fmt.Sprintf("📋 Task Plan: %s", task.Name)
	fmt.Println(TaskHeaderStyle.Render(header))
	fmt.Println()

	// Description
	if task.Description != "" {
		fmt.Printf("   %s\n\n", Subtle.Render(task.Description))
	}

	// Steps
	fmt.Println(Bold.Render("   Steps:"))
	for i, step := range task.Steps {
		stepNum := StepNumber.Render(fmt.Sprintf("%d.", i+1))
		icon := GetStepIcon(step.Status)
		style := GetStepStyle(step.Status)

		checkpoint := ""
		if step.Checkpoint {
			checkpoint = " " + Subtle.Render(IconCheckpoint)
		}

		actionInfo := formatActionInfo(&step.Action)

		fmt.Printf("   %s %s %s%s\n", stepNum, style.Render(icon), StepName.Render(step.Name), checkpoint)
		if actionInfo != "" {
			fmt.Printf("      %s\n", Subtle.Render(actionInfo))
		}
	}

	fmt.Println()
}

// DisplayTaskStatus shows the current status of a task
func DisplayTaskStatus(task *storage.TaskExecution) {
	fmt.Println()

	// Header with status
	statusStyle := GetTaskStatusStyle(task.Status)
	status := statusStyle.Render(string(task.Status))
	header := fmt.Sprintf("📋 %s [%s]", task.Name, status)
	fmt.Println(TaskHeaderStyle.Render(header))

	// Progress
	completed := 0
	for _, step := range task.Steps {
		if step.Status == storage.StepStatusCompleted {
			completed++
		}
	}
	progress := fmt.Sprintf("   Progress: %d/%d steps", completed, len(task.Steps))
	fmt.Println(Subtle.Render(progress))
	fmt.Println()

	// Steps with status
	for i, step := range task.Steps {
		stepNum := StepNumber.Render(fmt.Sprintf("%d.", i+1))
		icon := GetStepIcon(step.Status)
		style := GetStepStyle(step.Status)

		current := ""
		if i == task.CurrentStep && task.Status == storage.TaskExecStatusRunning {
			current = " ← current"
		}

		checkpoint := ""
		if step.Checkpoint {
			checkpoint = " " + Subtle.Render(IconCheckpoint)
		}

		fmt.Printf("   %s %s %s%s%s\n", stepNum, style.Render(icon), StepName.Render(step.Name), checkpoint, Subtle.Render(current))

		// Show error if failed
		if step.Status == storage.StepStatusFailed && step.Error != "" {
			errMsg := TruncateString(step.Error, 60)
			fmt.Printf("      %s\n", ErrorStyle.Render(errMsg))
		}

		// Show duration if completed
		if step.Status == storage.StepStatusCompleted && step.Duration > 0 {
			duration := fmt.Sprintf("(%.1fs)", float64(step.Duration)/1000)
			fmt.Printf("      %s\n", Subtle.Render(duration))
		}
	}

	// Show error if task failed
	if task.Error != "" && task.Status == storage.TaskExecStatusFailed {
		fmt.Println()
		fmt.Printf("   %s %s\n", ErrorStyle.Render("Error:"), task.Error)
	}

	fmt.Println()
}

// DisplayTaskList shows a list of tasks
func DisplayTaskList(tasks []storage.TaskMetadata) {
	fmt.Println()
	fmt.Println(Bold.Render("📋 Tasks"))
	fmt.Println()

	if len(tasks) == 0 {
		fmt.Println(Subtle.Render("   No tasks found"))
		fmt.Println()
		return
	}

	for _, meta := range tasks {
		statusStyle := GetTaskStatusStyle(meta.Status)
		status := statusStyle.Render(string(meta.Status))
		id := TruncateID(meta.ID, 8)

		progress := ""
		if meta.StepCount > 0 {
			completed := 0
			if meta.Status == storage.TaskExecStatusCompleted {
				completed = meta.StepCount
			} else if meta.CurrentStep >= 0 {
				completed = meta.CurrentStep
			}
			progress = fmt.Sprintf(" [%d/%d]", completed, meta.StepCount)
		}

		fmt.Printf("   %s  %s%s  %s\n",
			Subtle.Render(id),
			meta.Name,
			Subtle.Render(progress),
			status,
		)
	}

	fmt.Println()
}

// DisplayTaskApprovalPrompt shows the approval prompt for a task
func DisplayTaskApprovalPrompt() string {
	fmt.Println()
	fmt.Println(Bold.Render("   Options:"))
	fmt.Println("   [r] Run task")
	fmt.Println("   [e] Edit plan")
	fmt.Println("   [c] Cancel")
	fmt.Println()
	fmt.Print(PromptStyle.Render("   Choice: "))

	var choice string
	fmt.Scanln(&choice)
	return strings.ToLower(strings.TrimSpace(choice))
}

// DisplayStepProgress shows progress for the current step
func DisplayStepProgress(stepNum int, total int, stepName string) {
	progress := fmt.Sprintf("[%d/%d] %s", stepNum+1, total, stepName)
	fmt.Printf("\n   %s %s\n", SpinnerStyle.Render("◐"), progress)
}

// DisplayStepComplete shows step completion
func DisplayStepComplete(stepNum int, total int, stepName string, duration int64) {
	durationStr := ""
	if duration > 0 {
		durationStr = fmt.Sprintf(" (%.1fs)", float64(duration)/1000)
	}
	fmt.Printf("   %s [%d/%d] %s%s\n",
		SuccessStyle.Render("●"),
		stepNum+1, total,
		stepName,
		Subtle.Render(durationStr),
	)
}

// DisplayStepFailed shows step failure
func DisplayStepFailed(stepNum int, total int, stepName string, errMsg string) {
	fmt.Printf("   %s [%d/%d] %s\n", ErrorStyle.Render("✗"), stepNum+1, total, stepName)
	if errMsg != "" {
		fmt.Printf("      %s\n", ErrorStyle.Render(TruncateString(errMsg, 60)))
	}
}

// DisplayTaskComplete shows task completion summary
func DisplayTaskComplete(task *storage.TaskExecution) {
	fmt.Println()
	fmt.Printf("   %s Task completed: %s\n", SuccessStyle.Render("✓"), task.Name)

	// Calculate total duration
	if task.StartedAt != nil && task.CompletedAt != nil {
		duration := task.CompletedAt.Sub(*task.StartedAt)
		fmt.Printf("   %s\n", Subtle.Render(fmt.Sprintf("Total time: %.1fs", duration.Seconds())))
	}
	fmt.Println()
}

// DisplayTaskFailed shows task failure summary
func DisplayTaskFailed(task *storage.TaskExecution) {
	fmt.Println()
	fmt.Printf("   %s Task failed: %s\n", ErrorStyle.Render("✗"), task.Name)
	if task.Error != "" {
		fmt.Printf("   %s\n", ErrorStyle.Render(task.Error))
	}
	fmt.Println()
}

// formatActionInfo returns a human-readable description of an action
func formatActionInfo(action *storage.TaskAction) string {
	switch action.Type {
	case storage.ActionTypeTool:
		return fmt.Sprintf("Tool: %s", action.Tool)
	case storage.ActionTypeCommand:
		cmd := TruncateString(action.Command, 50)
		return fmt.Sprintf("Command: %s", cmd)
	case storage.ActionTypeAnalyze:
		prompt := TruncateString(action.Prompt, 50)
		return fmt.Sprintf("Analyze: %s", prompt)
	case storage.ActionTypeManual:
		return "Manual step"
	default:
		return ""
	}
}
