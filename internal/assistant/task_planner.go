package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tara-vision/taracode/internal/storage"
)

// TaskPlanner generates execution plans from natural language tasks
type TaskPlanner struct {
	assistant *Assistant
}

// NewTaskPlanner creates a new task planner
func NewTaskPlanner(asst *Assistant) *TaskPlanner {
	return &TaskPlanner{
		assistant: asst,
	}
}

// PlanTask generates an execution plan from a natural language task description
func (tp *TaskPlanner) PlanTask(taskDescription string) (*storage.TaskExecution, error) {
	// Create initial task execution
	task := &storage.TaskExecution{
		ID:           uuid.New().String(),
		OriginalTask: taskDescription,
		Status:       storage.TaskExecStatusPlanning,
		CurrentStep:  -1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Checkpoints:  []storage.TaskCheckpoint{},
	}

	// Generate the plan using LLM
	plan, err := tp.generatePlan(taskDescription)
	if err != nil {
		task.Status = storage.TaskExecStatusFailed
		task.Error = err.Error()
		return task, err
	}

	// Apply plan to task
	task.Name = plan.Name
	task.Description = plan.Description
	task.Steps = plan.Steps
	task.Status = storage.TaskExecStatusPending

	return task, nil
}

// TaskPlan is the intermediate plan structure from LLM
type TaskPlan struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Steps       []storage.TaskStep  `json:"steps"`
}

// generatePlan uses the LLM to create an execution plan
func (tp *TaskPlanner) generatePlan(taskDescription string) (*TaskPlan, error) {
	prompt := tp.buildPlanningPrompt(taskDescription)

	// Send to LLM for planning (without tool execution)
	response, err := tp.assistant.SendMessageForPlanning(prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate plan: %w", err)
	}

	// Parse the plan from LLM response
	plan, err := tp.parsePlanResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	// Validate and normalize the plan
	if err := tp.validatePlan(plan); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	return plan, nil
}

func (tp *TaskPlanner) buildPlanningPrompt(taskDescription string) string {
	toolList := tp.getAvailableToolsList()

	return fmt.Sprintf(`You are a task planning assistant. Break down the following task into concrete, executable steps.

TASK: %s

AVAILABLE TOOLS:
%s

RULES:
1. Maximum 8 steps (keep it focused)
2. Each step must use ONE tool or ONE shell command
3. Steps should be atomic and verifiable
4. Include checkpoint: true for steps that modify files or run destructive commands
5. Order steps logically with dependencies in mind

Respond with a JSON object in this EXACT format:
{
  "name": "Short task name (3-5 words)",
  "description": "One sentence describing what this task accomplishes",
  "steps": [
    {
      "name": "Step name",
      "description": "What this step does",
      "action": {
        "type": "tool",
        "tool": "tool_name",
        "params": {"param1": "value1"}
      },
      "checkpoint": false,
      "verification": {
        "type": "exitcode",
        "on_failure": "abort"
      }
    }
  ]
}

ACTION TYPES:
- "tool": Execute a taracode tool (tool + params required)
- "command": Execute a shell command (command required)
- "analyze": Ask LLM to analyze something (prompt required)

VERIFICATION TYPES (optional):
- "exitcode": Check command exit code is 0
- "contains": Check output contains expected string
- "command": Run a verification command

ON_FAILURE OPTIONS:
- "retry": Retry the step once with a fix attempt
- "skip": Skip this step and continue
- "abort": Stop execution (default)
- "rollback": Rollback to last checkpoint

OUTPUT ONLY THE JSON, NO MARKDOWN FENCES OR EXPLANATION.`, taskDescription, toolList)
}

func (tp *TaskPlanner) getAvailableToolsList() string {
	registry := tp.assistant.GetToolRegistry()
	if registry == nil {
		return "No tools available"
	}

	var sb strings.Builder
	for _, tool := range registry.GetToolList() {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description))
	}

	return sb.String()
}

func (tp *TaskPlanner) parsePlanResponse(response string) (*TaskPlan, error) {
	// Clean up response - remove markdown fences if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Find JSON object in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no valid JSON found in response")
	}
	response = response[start : end+1]

	// Parse into intermediate structure
	var rawPlan struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Action       struct {
				Type    string                 `json:"type"`
				Tool    string                 `json:"tool,omitempty"`
				Params  map[string]interface{} `json:"params,omitempty"`
				Command string                 `json:"command,omitempty"`
				Prompt  string                 `json:"prompt,omitempty"`
			} `json:"action"`
			Checkpoint   bool `json:"checkpoint,omitempty"`
			Verification *struct {
				Type      string `json:"type,omitempty"`
				Command   string `json:"command,omitempty"`
				Expected  string `json:"expected,omitempty"`
				OnFailure string `json:"on_failure,omitempty"`
				Timeout   int    `json:"timeout,omitempty"`
			} `json:"verification,omitempty"`
		} `json:"steps"`
	}

	if err := json.Unmarshal([]byte(response), &rawPlan); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Convert to TaskPlan
	plan := &TaskPlan{
		Name:        rawPlan.Name,
		Description: rawPlan.Description,
		Steps:       make([]storage.TaskStep, len(rawPlan.Steps)),
	}

	for i, rawStep := range rawPlan.Steps {
		step := storage.TaskStep{
			ID:          uuid.New().String(),
			Index:       i,
			Name:        rawStep.Name,
			Description: rawStep.Description,
			Status:      storage.StepStatusPending,
			Checkpoint:  rawStep.Checkpoint,
			Action: storage.TaskAction{
				Type:    storage.TaskActionType(rawStep.Action.Type),
				Tool:    rawStep.Action.Tool,
				Params:  rawStep.Action.Params,
				Command: rawStep.Action.Command,
				Prompt:  rawStep.Action.Prompt,
			},
		}

		// Convert verification if present
		if rawStep.Verification != nil {
			step.Verification = &storage.TaskVerify{
				Type:      storage.TaskVerifyType(rawStep.Verification.Type),
				Command:   rawStep.Verification.Command,
				Expected:  rawStep.Verification.Expected,
				OnFailure: rawStep.Verification.OnFailure,
				Timeout:   rawStep.Verification.Timeout,
			}
		}

		plan.Steps[i] = step
	}

	return plan, nil
}

func (tp *TaskPlanner) validatePlan(plan *TaskPlan) error {
	if plan.Name == "" {
		return fmt.Errorf("plan must have a name")
	}

	if len(plan.Steps) == 0 {
		return fmt.Errorf("plan must have at least one step")
	}

	if len(plan.Steps) > 8 {
		return fmt.Errorf("plan cannot have more than 8 steps (got %d)", len(plan.Steps))
	}

	for i, step := range plan.Steps {
		if step.Name == "" {
			return fmt.Errorf("step %d must have a name", i+1)
		}

		switch step.Action.Type {
		case storage.ActionTypeTool:
			if step.Action.Tool == "" {
				return fmt.Errorf("step %d: tool action requires tool name", i+1)
			}
		case storage.ActionTypeCommand:
			if step.Action.Command == "" {
				return fmt.Errorf("step %d: command action requires command", i+1)
			}
		case storage.ActionTypeAnalyze:
			if step.Action.Prompt == "" {
				return fmt.Errorf("step %d: analyze action requires prompt", i+1)
			}
		case storage.ActionTypeManual:
			// Manual steps don't require additional params
		default:
			return fmt.Errorf("step %d: unknown action type: %s", i+1, step.Action.Type)
		}
	}

	return nil
}

// RefineStep asks the LLM to refine a single step that failed
func (tp *TaskPlanner) RefineStep(step *storage.TaskStep, errorMsg string) (*storage.TaskStep, error) {
	prompt := fmt.Sprintf(`A task step failed and needs to be fixed.

STEP: %s
DESCRIPTION: %s
ACTION TYPE: %s
TOOL: %s
PARAMS: %v
ERROR: %s

Provide a corrected step that will succeed. Respond with JSON in this format:
{
  "name": "Corrected step name",
  "description": "What this step does",
  "action": {
    "type": "tool",
    "tool": "tool_name",
    "params": {"param1": "value1"}
  }
}

OUTPUT ONLY THE JSON.`, step.Name, step.Description, step.Action.Type, step.Action.Tool, step.Action.Params, errorMsg)

	response, err := tp.assistant.SendMessageForPlanning(prompt)
	if err != nil {
		return nil, err
	}

	// Parse refined step
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no valid JSON found in refinement response")
	}
	response = response[start : end+1]

	var rawStep struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Action      struct {
			Type    string                 `json:"type"`
			Tool    string                 `json:"tool,omitempty"`
			Params  map[string]interface{} `json:"params,omitempty"`
			Command string                 `json:"command,omitempty"`
		} `json:"action"`
	}

	if err := json.Unmarshal([]byte(response), &rawStep); err != nil {
		return nil, err
	}

	refinedStep := &storage.TaskStep{
		ID:          step.ID,
		Index:       step.Index,
		Name:        rawStep.Name,
		Description: rawStep.Description,
		Status:      storage.StepStatusPending,
		Checkpoint:  step.Checkpoint,
		RetryCount:  step.RetryCount + 1,
		Action: storage.TaskAction{
			Type:    storage.TaskActionType(rawStep.Action.Type),
			Tool:    rawStep.Action.Tool,
			Params:  rawStep.Action.Params,
			Command: rawStep.Action.Command,
		},
		Verification: step.Verification,
	}

	return refinedStep, nil
}
