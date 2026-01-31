package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/tools"
)

// DiagnosticsAgent specializes in failure analysis and root cause detection
type DiagnosticsAgent struct {
	*BaseAgent
}

// NewDiagnosticsAgent creates a new diagnostics agent
func NewDiagnosticsAgent(prov provider.Provider, toolReg *tools.Registry) *DiagnosticsAgent {
	return &DiagnosticsAgent{
		BaseAgent: NewBaseAgent(TypeDiagnostics, prov, toolReg),
	}
}

// CanHandle returns true for diagnostic-related tasks
func (a *DiagnosticsAgent) CanHandle(taskType string) bool {
	return taskType == "diagnose" || taskType == "debug" || taskType == "analyze_failure"
}

// DiagnosticReport contains the structured analysis of a failure
type DiagnosticReport struct {
	RootCause   string   `json:"root_cause"`
	Impact      string   `json:"impact"`
	Suggestions []string `json:"suggestions"`
	CanRetry    bool     `json:"can_retry"`
	NeedsReplan bool     `json:"needs_replan"`
}

// Execute runs the diagnostics agent
func (a *DiagnosticsAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for diagnostics agent")
	}

	systemPrompt := a.getDiagnosticsSystemPrompt()

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: a.buildDiagnosticPrompt(execCtx),
		},
	}

	toolDefs := a.GetToolDefinitions()

	var allToolResults []ToolCallResult
	var finalOutput string
	totalTokens := 0

	for iteration := 0; iteration < a.config.MaxToolIter; iteration++ {
		req := openai.ChatCompletionRequest{
			Model:       a.config.Model,
			Messages:    messages,
			Temperature: a.config.Temperature,
		}

		if len(toolDefs) > 0 {
			req.Tools = toolDefs
		}

		execCtx2, cancel := context.WithTimeout(ctx, time.Duration(a.config.Timeout)*time.Second)
		resp, err := client.CreateChatCompletion(execCtx2, req)
		cancel()

		if err != nil {
			a.IncrementErrors()
			return &ExecutionResult{
				AgentType:  TypeDiagnostics,
				Output:     finalOutput,
				ToolCalls:  allToolResults,
				TokensUsed: totalTokens,
				Success:    false,
				Error:      err.Error(),
				Duration:   time.Since(startTime),
			}, err
		}

		if len(resp.Choices) == 0 {
			break
		}

		choice := resp.Choices[0]
		totalTokens += resp.Usage.TotalTokens

		messages = append(messages, choice.Message)

		if choice.Message.Content != "" {
			finalOutput = choice.Message.Content
		}

		if len(choice.Message.ToolCalls) == 0 {
			break
		}

		for _, tc := range choice.Message.ToolCalls {
			toolResult := a.executeToolCall(execCtx.WorkingDir, tc)
			allToolResults = append(allToolResults, toolResult)

			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    toolResult.Result,
				ToolCallID: tc.ID,
			})
		}
	}

	a.AddTokensUsed(totalTokens)

	contextItems := a.buildDiagnosticContext(finalOutput)

	return &ExecutionResult{
		AgentType:  TypeDiagnostics,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getDiagnosticsSystemPrompt returns the specialized system prompt for diagnostics
func (a *DiagnosticsAgent) getDiagnosticsSystemPrompt() string {
	return `You are a Diagnostics Agent for taracode, a DevOps AI assistant.

Your role is to analyze failures, identify root causes, and suggest fixes.

WORKFLOW:
1. Analyze the error message and context
2. Read relevant files if needed for more context
3. Identify the root cause
4. Determine if the issue is recoverable
5. Provide actionable suggestions

ANALYSIS APPROACH:
1. Start with the error message - what does it literally say?
2. Check for common patterns:
   - Missing dependencies
   - Permission issues
   - Syntax errors
   - Configuration problems
   - Resource not found
   - Network issues
3. Investigate the context - what was the step trying to do?
4. Determine if retry can help or if replanning is needed

OUTPUT FORMAT:
Provide your analysis as:

ROOT CAUSE:
[One sentence describing the fundamental issue]

IMPACT:
[What this failure means for the overall task]

SUGGESTIONS:
1. [First suggestion - most likely to work]
2. [Second suggestion - alternative approach]
3. [Third suggestion if applicable]

RECOMMENDATION:
- Can Retry: Yes/No (with fix) / No
- Needs Replan: Yes/No

If recommending retry, specify exactly what should change.
If recommending replan, explain what new steps are needed.`
}

// buildDiagnosticPrompt builds the prompt for failure analysis
func (a *DiagnosticsAgent) buildDiagnosticPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString("FAILURE ANALYSIS REQUEST\n\n")
	sb.WriteString(fmt.Sprintf("TASK CONTEXT:\n%s\n\n", execCtx.Prompt))

	// Add prior results with details
	if len(execCtx.PriorResults) > 0 {
		sb.WriteString("PRIOR STEPS:\n")
		for _, result := range execCtx.PriorResults {
			status := "SUCCESS"
			if !result.Success {
				status = fmt.Sprintf("FAILED: %s", result.Error)
			}
			sb.WriteString(fmt.Sprintf("Step %d (%s): %s\n", result.StepIndex+1, result.AgentType.DisplayName(), status))
			if result.Output != "" && len(result.Output) < 500 {
				sb.WriteString(fmt.Sprintf("  Output: %s\n", result.Output))
			}
		}
		sb.WriteString("\n")
	}

	// Add context items
	if len(execCtx.Context) > 0 {
		sb.WriteString("CONTEXT:\n")
		for _, item := range execCtx.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Key, item.Value))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Analyze this failure and provide your diagnosis.")

	return sb.String()
}

// executeToolCall executes a single tool call
func (a *DiagnosticsAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for diagnostics agent", tc.Function.Name),
			Success:  false,
			Duration: time.Since(startTime),
		}
	}

	var params map[string]interface{}
	if err := parseJSON(tc.Function.Arguments, &params); err != nil {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("failed to parse parameters: %v", err),
			Success:  false,
			Duration: time.Since(startTime),
		}
	}

	result, err := a.toolRegistry.ExecuteTool(tc.Function.Name, params, workingDir)
	success := err == nil
	if err != nil {
		result = fmt.Sprintf("error: %v", err)
	}

	return ToolCallResult{
		ToolName: tc.Function.Name,
		Params:   params,
		Result:   result,
		Success:  success,
		Duration: time.Since(startTime),
	}
}

// buildDiagnosticContext builds context items from diagnostic analysis
func (a *DiagnosticsAgent) buildDiagnosticContext(output string) []ContextItem {
	var items []ContextItem

	output = strings.ToLower(output)

	// Determine if retry is recommended
	canRetry := strings.Contains(output, "can retry: yes")
	items = append(items, ContextItem{
		Key:        "can_retry",
		Value:      fmt.Sprintf("%v", canRetry),
		Importance: 10,
		Source:     TypeDiagnostics,
		CreatedAt:  time.Now(),
	})

	// Determine if replan is needed
	needsReplan := strings.Contains(output, "needs replan: yes")
	items = append(items, ContextItem{
		Key:        "needs_replan",
		Value:      fmt.Sprintf("%v", needsReplan),
		Importance: 10,
		Source:     TypeDiagnostics,
		CreatedAt:  time.Now(),
	})

	// Extract root cause if present
	if idx := strings.Index(output, "root cause:"); idx != -1 {
		end := strings.Index(output[idx:], "\n")
		if end == -1 {
			end = len(output) - idx
		}
		rootCause := strings.TrimSpace(output[idx+11 : idx+end])
		if len(rootCause) > 200 {
			rootCause = rootCause[:200] + "..."
		}
		items = append(items, ContextItem{
			Key:        "root_cause",
			Value:      rootCause,
			Importance: 9,
			Source:     TypeDiagnostics,
			CreatedAt:  time.Now(),
		})
	}

	return items
}

// AnalyzeFailure is a convenience method for analyzing a specific failure
func (a *DiagnosticsAgent) AnalyzeFailure(ctx context.Context, workingDir string, stepResult StepResult, priorContext []ContextItem) (*ExecutionResult, error) {
	execCtx := &ExecutionContext{
		Prompt: fmt.Sprintf("Step %d (%s) failed with error: %s\n\nOutput: %s",
			stepResult.StepIndex+1,
			stepResult.AgentType.DisplayName(),
			stepResult.Error,
			stepResult.Output),
		PriorResults: []StepResult{stepResult},
		Context:      priorContext,
		WorkingDir:   workingDir,
	}

	return a.Execute(ctx, execCtx)
}
