package assistant

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/storage"
)

// TaskExecutor runs task steps and handles verification/retry
type TaskExecutor struct {
	assistant   *Assistant
	taskManager *storage.TaskManager
}

// NewTaskExecutor creates a new task executor
func NewTaskExecutor(asst *Assistant, tm *storage.TaskManager) *TaskExecutor {
	return &TaskExecutor{
		assistant:   asst,
		taskManager: tm,
	}
}

// ExecuteStep runs a single step and returns the result
func (te *TaskExecutor) ExecuteStep(task *storage.TaskExecution, stepIndex int) error {
	if stepIndex < 0 || stepIndex >= len(task.Steps) {
		return fmt.Errorf("invalid step index: %d", stepIndex)
	}

	step := &task.Steps[stepIndex]
	step.Status = storage.StepStatusRunning
	now := time.Now()
	step.StartedAt = &now

	// Create checkpoint if required
	if step.Checkpoint {
		checkpoint, err := te.taskManager.CreateCheckpoint(task, fmt.Sprintf("Before step %d: %s", stepIndex+1, step.Name))
		if err != nil {
			return fmt.Errorf("failed to create checkpoint: %w", err)
		}
		_ = checkpoint // checkpoint is created and saved
	}

	// Execute based on action type
	var output string
	var execErr error

	switch step.Action.Type {
	case storage.ActionTypeTool:
		output, execErr = te.executeTool(step)
	case storage.ActionTypeCommand:
		output, execErr = te.executeCommand(step)
	case storage.ActionTypeAnalyze:
		output, execErr = te.executeAnalyze(step)
	case storage.ActionTypeManual:
		// Manual steps are marked as completed by user
		output = "Manual step - awaiting user action"
		step.Status = storage.StepStatusPending
		return te.taskManager.SaveTask(task)
	default:
		execErr = fmt.Errorf("unknown action type: %s", step.Action.Type)
	}

	// Record execution result
	endTime := time.Now()
	step.CompletedAt = &endTime
	step.Duration = endTime.Sub(now).Milliseconds()
	step.Output = output

	if execErr != nil {
		step.Error = execErr.Error()
		step.Status = storage.StepStatusFailed

		// Handle failure based on verification settings
		if step.Verification != nil {
			return te.handleStepFailure(task, step, execErr)
		}
		return te.taskManager.SaveTask(task)
	}

	// Verify step if verification is configured
	if step.Verification != nil {
		step.Status = storage.StepStatusVerifying
		if err := te.taskManager.SaveTask(task); err != nil {
			return err
		}

		verified, verifyErr := te.verifyStep(step)
		if !verified || verifyErr != nil {
			step.Status = storage.StepStatusFailed
			if verifyErr != nil {
				step.Error = verifyErr.Error()
			} else {
				step.Error = "verification failed"
			}
			return te.handleStepFailure(task, step, verifyErr)
		}
	}

	step.Status = storage.StepStatusCompleted
	return te.taskManager.SaveTask(task)
}

// executeTool executes a tool action
func (te *TaskExecutor) executeTool(step *storage.TaskStep) (string, error) {
	registry := te.assistant.GetToolRegistry()
	if registry == nil {
		return "", fmt.Errorf("no tool registry available")
	}

	if !registry.HasTool(step.Action.Tool) {
		return "", fmt.Errorf("tool not found: %s", step.Action.Tool)
	}

	// Execute the tool using the registry
	result, err := registry.ExecuteTool(step.Action.Tool, step.Action.Params, te.assistant.workingDir)
	if err != nil {
		return result, err
	}

	return result, nil
}

// executeCommand executes a shell command
func (te *TaskExecutor) executeCommand(step *storage.TaskStep) (string, error) {
	cmd := exec.Command("bash", "-c", step.Action.Command)
	cmd.Dir = te.assistant.workingDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// executeAnalyze sends a prompt to LLM for analysis
func (te *TaskExecutor) executeAnalyze(step *storage.TaskStep) (string, error) {
	response, err := te.assistant.SendMessageForPlanning(step.Action.Prompt)
	if err != nil {
		return "", fmt.Errorf("analysis failed: %w", err)
	}
	return response, nil
}

// verifyStep runs verification for a step
func (te *TaskExecutor) verifyStep(step *storage.TaskStep) (bool, error) {
	if step.Verification == nil {
		return true, nil
	}

	switch step.Verification.Type {
	case storage.VerifyTypeExitCode:
		// Already verified by command execution success
		return step.Error == "", nil

	case storage.VerifyTypeContains:
		return strings.Contains(step.Output, step.Verification.Expected), nil

	case storage.VerifyTypeCommand:
		cmd := exec.Command("bash", "-c", step.Verification.Command)
		cmd.Dir = te.assistant.workingDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("verification command failed: %w\nOutput: %s", err, string(output))
		}
		return true, nil

	case storage.VerifyTypeTool:
		registry := te.assistant.GetToolRegistry()
		if registry == nil {
			return false, fmt.Errorf("no tool registry for verification")
		}
		if !registry.HasTool(step.Verification.Tool) {
			return false, fmt.Errorf("verification tool not found: %s", step.Verification.Tool)
		}
		_, err := registry.ExecuteTool(step.Verification.Tool, step.Verification.Params, te.assistant.workingDir)
		return err == nil, err
	}

	return true, nil
}

// handleStepFailure handles a failed step based on verification settings
func (te *TaskExecutor) handleStepFailure(task *storage.TaskExecution, step *storage.TaskStep, err error) error {
	if step.Verification == nil {
		return te.taskManager.SaveTask(task)
	}

	switch step.Verification.OnFailure {
	case "retry":
		if step.RetryCount < 1 {
			// Try to refine and retry the step
			planner := NewTaskPlanner(te.assistant)
			refinedStep, refineErr := planner.RefineStep(step, step.Error)
			if refineErr == nil {
				// Update step with refined version
				task.Steps[step.Index] = *refinedStep
				task.Steps[step.Index].Status = storage.StepStatusRetrying
				if saveErr := te.taskManager.SaveTask(task); saveErr != nil {
					return saveErr
				}
				// Re-execute the refined step
				return te.ExecuteStep(task, step.Index)
			}
		}
		// Retry exhausted, mark as failed
		step.Status = storage.StepStatusFailed
		task.Status = storage.TaskExecStatusFailed

	case "skip":
		step.Status = storage.StepStatusSkipped

	case "rollback":
		// Find and rollback to last checkpoint
		checkpoint := te.taskManager.GetLatestCheckpoint(task)
		if checkpoint != nil {
			if rbErr := te.taskManager.RollbackToCheckpoint(task, checkpoint.ID); rbErr != nil {
				return rbErr
			}
			task.Status = storage.TaskExecStatusRolledBack
		} else {
			task.Status = storage.TaskExecStatusFailed
		}

	default: // "abort"
		step.Status = storage.StepStatusFailed
		task.Status = storage.TaskExecStatusFailed
	}

	return te.taskManager.SaveTask(task)
}

// RunTask executes all steps of a task
func (te *TaskExecutor) RunTask(task *storage.TaskExecution) error {
	if task.Status != storage.TaskExecStatusPending && task.Status != storage.TaskExecStatusPaused {
		return fmt.Errorf("task cannot be run: status is %s", task.Status)
	}

	task.Status = storage.TaskExecStatusRunning
	now := time.Now()
	task.StartedAt = &now

	if err := te.taskManager.SaveTask(task); err != nil {
		return err
	}

	// Start from current step or beginning
	startStep := task.CurrentStep + 1
	if startStep < 0 {
		startStep = 0
	}

	for i := startStep; i < len(task.Steps); i++ {
		task.CurrentStep = i
		if err := te.taskManager.SaveTask(task); err != nil {
			return err
		}

		if err := te.ExecuteStep(task, i); err != nil {
			return err
		}

		// Check if task was failed, aborted, or rolled back
		if task.Status == storage.TaskExecStatusFailed ||
			task.Status == storage.TaskExecStatusAborted ||
			task.Status == storage.TaskExecStatusRolledBack {
			return nil
		}

		// Check if step is manual (requires user action)
		if task.Steps[i].Status == storage.StepStatusPending {
			task.Status = storage.TaskExecStatusPaused
			return te.taskManager.SaveTask(task)
		}
	}

	// All steps completed
	task.Status = storage.TaskExecStatusCompleted
	completedAt := time.Now()
	task.CompletedAt = &completedAt

	return te.taskManager.SaveTask(task)
}

// PauseTask pauses task execution
func (te *TaskExecutor) PauseTask(task *storage.TaskExecution) error {
	if task.Status != storage.TaskExecStatusRunning {
		return fmt.Errorf("can only pause running tasks")
	}
	task.Status = storage.TaskExecStatusPaused
	return te.taskManager.SaveTask(task)
}

// AbortTask aborts task execution
func (te *TaskExecutor) AbortTask(task *storage.TaskExecution) error {
	task.Status = storage.TaskExecStatusAborted
	return te.taskManager.SaveTask(task)
}

// ParseTaskTemplate parses a YAML task template into a TaskTemplate
func ParseTaskTemplate(yamlContent string) (*storage.TaskTemplate, error) {
	// For now, use JSON since we have those definitions ready
	// YAML parsing can be added with gopkg.in/yaml.v3
	var template storage.TaskTemplate
	if err := json.Unmarshal([]byte(yamlContent), &template); err != nil {
		return nil, fmt.Errorf("failed to parse task template: %w", err)
	}
	return &template, nil
}

// CreateTaskFromTemplate creates a TaskExecution from a template
func (te *TaskExecutor) CreateTaskFromTemplate(template *storage.TaskTemplate, variables map[string]string) (*storage.TaskExecution, error) {
	task, err := te.taskManager.CreateTask(template.Name, template.Description, "template:"+template.Name)
	if err != nil {
		return nil, err
	}

	// Convert template steps to task steps
	for i, ts := range template.Steps {
		// Substitute variables in params
		params := make(map[string]interface{})
		for k, v := range ts.Params {
			if strVal, ok := v.(string); ok {
				for varName, varVal := range variables {
					strVal = strings.ReplaceAll(strVal, "{{"+varName+"}}", varVal)
				}
				params[k] = strVal
			} else {
				params[k] = v
			}
		}

		actionType := storage.ActionTypeTool
		if ts.Action == "command" {
			actionType = storage.ActionTypeCommand
		} else if ts.Action == "analyze" {
			actionType = storage.ActionTypeAnalyze
		}

		step := storage.TaskStep{
			Index:       i,
			Name:        ts.Name,
			Description: ts.Name,
			Status:      storage.StepStatusPending,
			Checkpoint:  ts.Checkpoint,
			Action: storage.TaskAction{
				Type:   actionType,
				Tool:   ts.Action,
				Params: params,
			},
		}

		// Add verification if defined
		if ts.Verify != nil {
			step.Verification = &storage.TaskVerify{
				Command:   ts.Verify.Command,
				Expected:  ts.Verify.Contains,
				Timeout:   ts.Verify.Timeout,
				OnFailure: ts.OnFailure,
			}
			if ts.Verify.Command != "" {
				step.Verification.Type = storage.VerifyTypeCommand
			} else if ts.Verify.Contains != "" {
				step.Verification.Type = storage.VerifyTypeContains
			}
		}

		task.Steps = append(task.Steps, step)
	}

	task.Status = storage.TaskExecStatusPending
	return task, te.taskManager.SaveTask(task)
}
