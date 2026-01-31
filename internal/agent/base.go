package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/tools"
)

// BaseAgent provides common functionality for all agent types
type BaseAgent struct {
	agentType    Type
	config       Config
	state        State
	provider     provider.Provider
	client       *openai.Client
	toolRegistry *tools.Registry
	mu           sync.RWMutex
}

// NewBaseAgent creates a new base agent
func NewBaseAgent(agentType Type, prov provider.Provider, toolReg *tools.Registry) *BaseAgent {
	cfg := DefaultConfig(agentType)

	return &BaseAgent{
		agentType:    agentType,
		config:       cfg,
		provider:     prov,
		toolRegistry: toolReg,
		state: State{
			Type:         agentType,
			Active:       false,
			CurrentStep:  -1,
			TokensUsed:   0,
			LastActivity: time.Now(),
			ErrorCount:   0,
			Invocations:  0,
		},
	}
}

// Type returns the agent type
func (a *BaseAgent) Type() Type {
	return a.agentType
}

// Config returns the agent configuration
func (a *BaseAgent) Config() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// SetConfig updates the agent configuration
func (a *BaseAgent) SetConfig(cfg Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.config = cfg
}

// GetState returns the current agent state
func (a *BaseAgent) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// GetToolRegistry returns the tools available to this agent
func (a *BaseAgent) GetToolRegistry() *tools.Registry {
	return a.toolRegistry
}

// SetProvider sets the LLM provider for this agent
func (a *BaseAgent) SetProvider(prov provider.Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.provider = prov
	if prov != nil {
		a.client = prov.CreateClient()
	}
}

// GetProvider returns the current provider
func (a *BaseAgent) GetProvider() provider.Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.provider
}

// GetClient returns the OpenAI client
func (a *BaseAgent) GetClient() *openai.Client {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.client
}

// CanHandle returns true if this agent can handle the given task type
func (a *BaseAgent) CanHandle(taskType string) bool {
	// Base implementation - can be overridden by specialized agents
	switch a.agentType {
	case TypePlanner:
		return taskType == "plan" || taskType == "analyze"
	case TypeCoder:
		return taskType == "code" || taskType == "edit" || taskType == "implement"
	case TypeTester:
		return taskType == "test" || taskType == "verify"
	case TypeReviewer:
		return taskType == "review" || taskType == "audit"
	case TypeDevOps:
		return taskType == "deploy" || taskType == "infrastructure"
	case TypeSecurity:
		return taskType == "security" || taskType == "scan"
	case TypeDiagnostics:
		return taskType == "diagnose" || taskType == "debug"
	default:
		return false
	}
}

// UpdateState updates the agent state
func (a *BaseAgent) UpdateState(fn func(*State)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(&a.state)
}

// IncrementInvocations increments the invocation counter
func (a *BaseAgent) IncrementInvocations() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Invocations++
	a.state.LastActivity = time.Now()
}

// IncrementErrors increments the error counter
func (a *BaseAgent) IncrementErrors() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.ErrorCount++
}

// AddTokensUsed adds to the token usage counter
func (a *BaseAgent) AddTokensUsed(tokens int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.TokensUsed += tokens
}

// SetActive sets the active state
func (a *BaseAgent) SetActive(active bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Active = active
	if active {
		a.state.LastActivity = time.Now()
	}
}

// SetCurrentStep sets the current step index
func (a *BaseAgent) SetCurrentStep(step int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.CurrentStep = step
}

// GetFilteredTools returns tools filtered by the agent's allowed categories
func (a *BaseAgent) GetFilteredTools() []tools.ToolInfo {
	if a.toolRegistry == nil {
		return nil
	}

	allTools := a.toolRegistry.GetToolList()
	if len(a.config.ToolCategories) == 0 {
		return allTools
	}

	var filtered []tools.ToolInfo
	for _, tool := range allTools {
		if IsToolAllowed(tool.Name, a.config.ToolCategories) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// GetToolDefinitions returns OpenAI tool definitions for this agent's allowed tools
func (a *BaseAgent) GetToolDefinitions() []openai.Tool {
	filteredTools := a.GetFilteredTools()
	if len(filteredTools) == 0 {
		return nil
	}

	// Get full definitions from the tools package
	allDefs := tools.GetToolDefinitions()
	var defs []openai.Tool

	// Create a set of allowed tool names
	allowed := make(map[string]bool)
	for _, t := range filteredTools {
		allowed[t.Name] = true
	}

	// Filter definitions
	for _, def := range allDefs {
		if def.Function != nil && allowed[def.Function.Name] {
			defs = append(defs, def)
		}
	}

	return defs
}

// BuildSystemPrompt builds the system prompt for this agent
func (a *BaseAgent) BuildSystemPrompt(execCtx *ExecutionContext) string {
	basePrompt := a.getBaseSystemPrompt()

	// Add context items
	if len(execCtx.Context) > 0 {
		basePrompt += "\n\n## Prior Context\n"
		for _, item := range execCtx.Context {
			basePrompt += fmt.Sprintf("- **%s** (from %s): %s\n", item.Key, item.Source.DisplayName(), item.Value)
		}
	}

	// Add prior results summary
	if len(execCtx.PriorResults) > 0 {
		basePrompt += "\n\n## Previous Steps\n"
		for _, result := range execCtx.PriorResults {
			status := "completed"
			if !result.Success {
				status = "failed"
			}
			basePrompt += fmt.Sprintf("- Step %d (%s): %s\n", result.StepIndex+1, result.AgentType.DisplayName(), status)
		}
	}

	return basePrompt
}

// getBaseSystemPrompt returns the base system prompt for the agent type
func (a *BaseAgent) getBaseSystemPrompt() string {
	switch a.agentType {
	case TypePlanner:
		return `You are a Task Planning Agent. Your role is to:
1. Analyze tasks and break them into concrete, executable steps
2. Identify dependencies between steps
3. Determine which agent type should handle each step
4. Create efficient execution plans

Focus on:
- Clear, atomic steps that can be verified
- Proper dependency ordering
- Identifying potential failure points
- Suggesting checkpoints for destructive operations

Output plans in a structured format that can be parsed and executed.`

	case TypeCoder:
		return `You are a Coding Agent. Your role is to:
1. Write high-quality, maintainable code
2. Edit existing code with minimal changes
3. Follow project conventions and patterns
4. Handle file operations carefully

Focus on:
- Clean, readable code
- Proper error handling
- Following existing patterns in the codebase
- Making minimal, targeted changes

Always read files before editing them. Prefer small, focused edits over large rewrites.`

	case TypeTester:
		return `You are a Testing Agent. Your role is to:
1. Execute tests and analyze results
2. Write test cases when needed
3. Verify implementation correctness
4. Report test failures clearly

Focus on:
- Running existing test suites
- Analyzing test output for failures
- Suggesting fixes for failing tests
- Ensuring adequate test coverage

Report results clearly with specific failure details and suggestions.`

	case TypeReviewer:
		return `You are a Code Review Agent. Your role is to:
1. Review code for quality and correctness
2. Identify potential bugs and issues
3. Check for security vulnerabilities
4. Suggest improvements

Focus on:
- Logic errors and edge cases
- Security issues
- Performance concerns
- Code style and maintainability

Provide constructive, actionable feedback with specific suggestions.`

	case TypeDevOps:
		return `You are a DevOps Agent. Your role is to:
1. Manage infrastructure operations
2. Handle deployments and configuration
3. Work with Kubernetes, Terraform, Docker
4. Manage cloud resources

Focus on:
- Safe, reversible operations
- Proper resource management
- Following infrastructure best practices
- Clear status reporting

Always verify state before making changes. Prefer dry-run when available.`

	case TypeSecurity:
		return `You are a Security Agent. Your role is to:
1. Scan for vulnerabilities
2. Detect secrets and credentials
3. Audit dependencies
4. Analyze security posture

Focus on:
- Finding security issues before they become problems
- Clear severity reporting
- Actionable remediation steps
- Comprehensive scanning

Report findings with clear severity levels and fix recommendations.`

	case TypeDiagnostics:
		return `You are a Diagnostics Agent. Your role is to:
1. Analyze failures and errors
2. Identify root causes
3. Suggest fixes and workarounds
4. Help recover from failures

Focus on:
- Understanding what went wrong
- Finding the root cause, not just symptoms
- Providing actionable fix suggestions
- Helping the user understand the issue

Be thorough in analysis but concise in reporting.`

	default:
		return "You are an AI assistant helping with software development tasks."
	}
}

// Execute runs the agent - this is a base implementation that should be overridden
func (a *BaseAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	if a.client == nil {
		return nil, fmt.Errorf("no LLM client configured for agent %s", a.agentType)
	}

	// Build system prompt
	systemPrompt := a.BuildSystemPrompt(execCtx)

	// Build messages
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: execCtx.Prompt,
		},
	}

	// Get tool definitions
	toolDefs := a.GetToolDefinitions()

	// Create request
	req := openai.ChatCompletionRequest{
		Model:       a.config.Model,
		Messages:    messages,
		Temperature: a.config.Temperature,
	}

	if len(toolDefs) > 0 {
		req.Tools = toolDefs
	}

	// Execute with timeout
	execCtx2, cancel := context.WithTimeout(ctx, time.Duration(a.config.Timeout)*time.Second)
	defer cancel()

	resp, err := a.client.CreateChatCompletion(execCtx2, req)
	if err != nil {
		a.IncrementErrors()
		return &ExecutionResult{
			AgentType: a.agentType,
			Success:   false,
			Error:     err.Error(),
			Duration:  time.Since(startTime),
		}, err
	}

	// Process response
	if len(resp.Choices) == 0 {
		return &ExecutionResult{
			AgentType: a.agentType,
			Success:   false,
			Error:     "no response from model",
			Duration:  time.Since(startTime),
		}, fmt.Errorf("no response from model")
	}

	choice := resp.Choices[0]
	tokensUsed := resp.Usage.TotalTokens
	a.AddTokensUsed(tokensUsed)

	// Handle tool calls if present
	var toolResults []ToolCallResult
	if len(choice.Message.ToolCalls) > 0 {
		toolResults = a.executeToolCalls(execCtx.WorkingDir, choice.Message.ToolCalls)
	}

	return &ExecutionResult{
		AgentType:  a.agentType,
		Output:     choice.Message.Content,
		ToolCalls:  toolResults,
		TokensUsed: tokensUsed,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// executeToolCalls executes tool calls and returns results
func (a *BaseAgent) executeToolCalls(workingDir string, toolCalls []openai.ToolCall) []ToolCallResult {
	var results []ToolCallResult

	for _, tc := range toolCalls {
		startTime := time.Now()

		// Check if tool is allowed
		if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
			results = append(results, ToolCallResult{
				ToolName: tc.Function.Name,
				Result:   fmt.Sprintf("tool %s not allowed for this agent", tc.Function.Name),
				Success:  false,
				Duration: time.Since(startTime),
			})
			continue
		}

		// Parse parameters
		var params map[string]interface{}
		if err := parseJSON(tc.Function.Arguments, &params); err != nil {
			results = append(results, ToolCallResult{
				ToolName: tc.Function.Name,
				Result:   fmt.Sprintf("failed to parse parameters: %v", err),
				Success:  false,
				Duration: time.Since(startTime),
			})
			continue
		}

		// Execute tool
		result, err := a.toolRegistry.ExecuteTool(tc.Function.Name, params, workingDir)
		success := err == nil
		if err != nil {
			result = fmt.Sprintf("error: %v", err)
		}

		results = append(results, ToolCallResult{
			ToolName: tc.Function.Name,
			Params:   params,
			Result:   result,
			Success:  success,
			Duration: time.Since(startTime),
		})
	}

	return results
}

// parseJSON is a helper to parse JSON strings
func parseJSON(data string, v interface{}) error {
	if data == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}
