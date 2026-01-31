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

// ReviewerAgent specializes in code review and quality checks
type ReviewerAgent struct {
	*BaseAgent
}

// NewReviewerAgent creates a new reviewer agent
func NewReviewerAgent(prov provider.Provider, toolReg *tools.Registry) *ReviewerAgent {
	return &ReviewerAgent{
		BaseAgent: NewBaseAgent(TypeReviewer, prov, toolReg),
	}
}

// CanHandle returns true for review-related tasks
func (a *ReviewerAgent) CanHandle(taskType string) bool {
	return taskType == "review" || taskType == "audit" || taskType == "check"
}

// Execute runs the reviewer agent
func (a *ReviewerAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for reviewer agent")
	}

	systemPrompt := a.getReviewerSystemPrompt(execCtx)

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
				AgentType:  TypeReviewer,
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

	contextItems := a.buildReviewContext(finalOutput)

	return &ExecutionResult{
		AgentType:  TypeReviewer,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getReviewerSystemPrompt returns the specialized system prompt for reviewing
func (a *ReviewerAgent) getReviewerSystemPrompt(execCtx *ExecutionContext) string {
	strictness := a.config.ReviewStrictness
	if strictness == "" {
		strictness = "medium"
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`You are a Code Review Agent for taracode, a DevOps AI assistant.

Your role is to review code for quality, correctness, and security.

REVIEW STRICTNESS: %s

`, strictness))

	switch strictness {
	case "low":
		sb.WriteString(`Focus on:
- Critical bugs and errors
- Security vulnerabilities
- Major code smells

Skip:
- Style suggestions
- Minor optimizations
- Documentation gaps
`)
	case "high":
		sb.WriteString(`Focus on:
- All bugs and potential issues
- Security vulnerabilities (thorough)
- Performance concerns
- Code style and conventions
- Documentation completeness
- Test coverage
- Error handling
- Edge cases
`)
	default: // medium
		sb.WriteString(`Focus on:
- Bugs and logic errors
- Security vulnerabilities
- Code readability
- Error handling
- Major style issues
`)
	}

	sb.WriteString(`

WORKFLOW:
1. Read the files to review
2. Analyze for issues
3. Provide constructive feedback

OUTPUT FORMAT:
For each issue found:
- File: path/to/file.go
- Line: 42
- Severity: Critical/High/Medium/Low
- Issue: Description
- Suggestion: How to fix

End with a summary:
- Total issues: X
- Critical: X, High: X, Medium: X, Low: X
- Recommendation: Approve / Request Changes / Block

`)

	if len(execCtx.Context) > 0 {
		sb.WriteString("\nCONTEXT FROM PRIOR STEPS:\n")
		for _, item := range execCtx.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Key, item.Value))
		}
	}

	return sb.String()
}

// executeToolCall executes a single tool call
func (a *ReviewerAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for reviewer agent", tc.Function.Name),
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

// buildReviewContext builds context items from review results
func (a *ReviewerAgent) buildReviewContext(output string) []ContextItem {
	var items []ContextItem

	output = strings.ToLower(output)

	// Determine review recommendation
	recommendation := "unknown"
	if strings.Contains(output, "approve") {
		recommendation = "approve"
	} else if strings.Contains(output, "block") {
		recommendation = "block"
	} else if strings.Contains(output, "request changes") {
		recommendation = "request_changes"
	}

	items = append(items, ContextItem{
		Key:        "review_recommendation",
		Value:      recommendation,
		Importance: 10,
		Source:     TypeReviewer,
		CreatedAt:  time.Now(),
	})

	// Check for critical issues
	hasCritical := strings.Contains(output, "critical")
	items = append(items, ContextItem{
		Key:        "has_critical_issues",
		Value:      fmt.Sprintf("%v", hasCritical),
		Importance: 9,
		Source:     TypeReviewer,
		CreatedAt:  time.Now(),
	})

	return items
}
