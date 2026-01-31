package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/tools"
)

// PlannerAgent specializes in task decomposition and dependency analysis
type PlannerAgent struct {
	*BaseAgent
}

// NewPlannerAgent creates a new planner agent
func NewPlannerAgent(prov provider.Provider, toolReg *tools.Registry) *PlannerAgent {
	return &PlannerAgent{
		BaseAgent: NewBaseAgent(TypePlanner, prov, toolReg),
	}
}

// TaskPlan represents a planned task with steps
type TaskPlan struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Steps       []PlanStep `json:"steps"`
}

// PlanStep represents a single step in the plan
type PlanStep struct {
	Index        int      `json:"index"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AgentType    Type     `json:"agent_type"`
	ActionType   string   `json:"action_type"` // tool, command, analyze
	Tool         string   `json:"tool,omitempty"`
	Command      string   `json:"command,omitempty"`
	Prompt       string   `json:"prompt,omitempty"`
	DependsOn    []int    `json:"depends_on,omitempty"`
	Checkpoint   bool     `json:"checkpoint,omitempty"`
	Verification string   `json:"verification,omitempty"`
	OnFailure    string   `json:"on_failure,omitempty"` // retry, skip, abort, rollback
}

// CanHandle returns true for planning-related tasks
func (a *PlannerAgent) CanHandle(taskType string) bool {
	return taskType == "plan" || taskType == "analyze" || taskType == "decompose"
}

// Execute runs the planner agent
func (a *PlannerAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for planner agent")
	}

	// Build planning prompt
	prompt := a.buildPlanningPrompt(execCtx)

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: a.getPlannerSystemPrompt(),
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: prompt,
		},
	}

	// Create request without tools - planner outputs JSON plan directly
	req := openai.ChatCompletionRequest{
		Model:       a.config.Model,
		Messages:    messages,
		Temperature: a.config.Temperature,
	}

	// Execute with timeout
	execCtx2, cancel := context.WithTimeout(ctx, time.Duration(a.config.Timeout)*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(execCtx2, req)
	if err != nil {
		a.IncrementErrors()
		return &ExecutionResult{
			AgentType: TypePlanner,
			Success:   false,
			Error:     err.Error(),
			Duration:  time.Since(startTime),
		}, err
	}

	if len(resp.Choices) == 0 {
		return &ExecutionResult{
			AgentType: TypePlanner,
			Success:   false,
			Error:     "no response from model",
			Duration:  time.Since(startTime),
		}, fmt.Errorf("no response from model")
	}

	output := resp.Choices[0].Message.Content
	tokensUsed := resp.Usage.TotalTokens
	a.AddTokensUsed(tokensUsed)

	// Extract context items from the plan
	contextItems := a.extractContextFromPlan(output)

	return &ExecutionResult{
		AgentType:  TypePlanner,
		Output:     output,
		Context:    contextItems,
		TokensUsed: tokensUsed,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getPlannerSystemPrompt returns the specialized system prompt for planning
func (a *PlannerAgent) getPlannerSystemPrompt() string {
	return `You are a Task Planning Agent for taracode, a DevOps AI assistant.

Your role is to break down complex tasks into concrete, executable steps.

RULES:
1. Maximum 8 steps per plan
2. Each step must be atomic and verifiable
3. Assign each step to the most appropriate agent type
4. Include dependencies between steps when needed
5. Mark checkpoint: true for steps that modify files or run destructive commands
6. Order steps logically

AGENT TYPES:
- planner: Task decomposition (rarely needed as sub-step)
- coder: Code writing and editing
- tester: Test execution and verification
- reviewer: Code review and quality checks
- devops: Infrastructure operations (Kubernetes, Terraform, Docker)
- security: Security scanning and analysis
- diagnostics: Failure analysis (auto-invoked on errors)

ACTION TYPES:
- tool: Execute a taracode tool
- command: Execute a shell command
- analyze: Ask LLM to analyze something

ON_FAILURE OPTIONS:
- retry: Retry the step with diagnosis
- skip: Skip this step and continue
- abort: Stop execution (default)
- rollback: Rollback to last checkpoint

OUTPUT FORMAT:
Respond with a JSON object:
{
  "name": "Short task name (3-5 words)",
  "description": "One sentence describing what this task accomplishes",
  "steps": [
    {
      "index": 0,
      "name": "Step name",
      "description": "What this step does",
      "agent_type": "coder",
      "action_type": "tool",
      "tool": "tool_name",
      "depends_on": [],
      "checkpoint": false,
      "on_failure": "abort"
    }
  ]
}

OUTPUT ONLY THE JSON, NO MARKDOWN FENCES OR EXPLANATION.`
}

// buildPlanningPrompt builds the prompt for planning
func (a *PlannerAgent) buildPlanningPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("TASK: %s\n\n", execCtx.Prompt))

	// Add available tools
	if a.toolRegistry != nil {
		sb.WriteString("AVAILABLE TOOLS:\n")
		for _, tool := range a.toolRegistry.GetToolList() {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description))
		}
		sb.WriteString("\n")
	}

	// Add context from previous agents
	if len(execCtx.Context) > 0 {
		sb.WriteString("CONTEXT FROM PRIOR ANALYSIS:\n")
		for _, item := range execCtx.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Key, item.Value))
		}
		sb.WriteString("\n")
	}

	// Add prior results if any
	if len(execCtx.PriorResults) > 0 {
		sb.WriteString("PRIOR STEPS COMPLETED:\n")
		for _, result := range execCtx.PriorResults {
			status := "success"
			if !result.Success {
				status = "failed"
			}
			sb.WriteString(fmt.Sprintf("- Step %d (%s): %s\n", result.StepIndex+1, result.AgentType.DisplayName(), status))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// extractContextFromPlan extracts shareable context items from the plan
func (a *PlannerAgent) extractContextFromPlan(output string) []ContextItem {
	var items []ContextItem

	// Try to parse the plan
	plan, err := a.ParsePlan(output)
	if err != nil {
		return items
	}

	// Add plan summary as context
	items = append(items, ContextItem{
		Key:        "plan_name",
		Value:      plan.Name,
		Importance: 8,
		Source:     TypePlanner,
		CreatedAt:  time.Now(),
	})

	items = append(items, ContextItem{
		Key:        "step_count",
		Value:      fmt.Sprintf("%d", len(plan.Steps)),
		Importance: 5,
		Source:     TypePlanner,
		CreatedAt:  time.Now(),
	})

	// Add agent assignments
	agentCounts := make(map[Type]int)
	for _, step := range plan.Steps {
		agentCounts[step.AgentType]++
	}

	var agentSummary []string
	for agent, count := range agentCounts {
		agentSummary = append(agentSummary, fmt.Sprintf("%s: %d", agent.DisplayName(), count))
	}
	items = append(items, ContextItem{
		Key:        "agent_assignments",
		Value:      strings.Join(agentSummary, ", "),
		Importance: 6,
		Source:     TypePlanner,
		CreatedAt:  time.Now(),
	})

	return items
}

// ParsePlan parses a plan from LLM output
func (a *PlannerAgent) ParsePlan(output string) (*TaskPlan, error) {
	// Clean up response
	output = strings.TrimSpace(output)
	output = strings.TrimPrefix(output, "```json")
	output = strings.TrimPrefix(output, "```")
	output = strings.TrimSuffix(output, "```")
	output = strings.TrimSpace(output)

	// Find JSON object in response
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no valid JSON found in response")
	}
	output = output[start : end+1]

	var plan TaskPlan
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	// Validate plan
	if err := a.validatePlan(&plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

// validatePlan validates a task plan
func (a *PlannerAgent) validatePlan(plan *TaskPlan) error {
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

		// Validate agent type
		validAgent := false
		for _, t := range AllTypes() {
			if step.AgentType == t {
				validAgent = true
				break
			}
		}
		if !validAgent && step.AgentType != "" {
			// Try to map string to agent type
			plan.Steps[i].AgentType = a.mapAgentType(string(step.AgentType))
		}

		// Validate action type
		switch step.ActionType {
		case "tool":
			if step.Tool == "" {
				return fmt.Errorf("step %d: tool action requires tool name", i+1)
			}
		case "command":
			if step.Command == "" {
				return fmt.Errorf("step %d: command action requires command", i+1)
			}
		case "analyze":
			if step.Prompt == "" && step.Description == "" {
				return fmt.Errorf("step %d: analyze action requires prompt or description", i+1)
			}
		case "":
			// Auto-detect action type
			if step.Tool != "" {
				plan.Steps[i].ActionType = "tool"
			} else if step.Command != "" {
				plan.Steps[i].ActionType = "command"
			} else {
				plan.Steps[i].ActionType = "analyze"
			}
		}

		// Validate dependencies
		for _, dep := range step.DependsOn {
			if dep < 0 || dep >= i {
				return fmt.Errorf("step %d: invalid dependency on step %d", i+1, dep+1)
			}
		}
	}

	return nil
}

// mapAgentType maps a string to an agent type
func (a *PlannerAgent) mapAgentType(s string) Type {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "planner", "plan":
		return TypePlanner
	case "coder", "code", "developer":
		return TypeCoder
	case "tester", "test":
		return TypeTester
	case "reviewer", "review":
		return TypeReviewer
	case "devops", "ops", "infrastructure":
		return TypeDevOps
	case "security", "sec":
		return TypeSecurity
	case "diagnostics", "debug", "diagnose":
		return TypeDiagnostics
	default:
		return TypeCoder // Default to coder
	}
}
