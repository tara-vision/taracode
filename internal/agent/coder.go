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

// CoderAgent specializes in code generation and editing
type CoderAgent struct {
	*BaseAgent
}

// NewCoderAgent creates a new coder agent
func NewCoderAgent(prov provider.Provider, toolReg *tools.Registry) *CoderAgent {
	return &CoderAgent{
		BaseAgent: NewBaseAgent(TypeCoder, prov, toolReg),
	}
}

// CanHandle returns true for coding-related tasks
func (a *CoderAgent) CanHandle(taskType string) bool {
	return taskType == "code" || taskType == "edit" || taskType == "implement" ||
		taskType == "write" || taskType == "create" || taskType == "fix"
}

// Execute runs the coder agent with iterative tool calling
func (a *CoderAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for coder agent")
	}

	// Build system prompt
	systemPrompt := a.getCoderSystemPrompt(execCtx)

	// Initialize conversation
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

	var allToolResults []ToolCallResult
	var finalOutput string
	totalTokens := 0

	// Iterative tool calling loop
	for iteration := 0; iteration < a.config.MaxToolIter; iteration++ {
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
		resp, err := client.CreateChatCompletion(execCtx2, req)
		cancel()

		if err != nil {
			a.IncrementErrors()
			return &ExecutionResult{
				AgentType:  TypeCoder,
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

		// Append assistant message
		messages = append(messages, choice.Message)

		// If there's content, capture it
		if choice.Message.Content != "" {
			finalOutput = choice.Message.Content
		}

		// Check for tool calls
		if len(choice.Message.ToolCalls) == 0 {
			// No more tool calls, we're done
			break
		}

		// Execute tool calls
		for _, tc := range choice.Message.ToolCalls {
			toolResult := a.executeToolCall(execCtx.WorkingDir, tc)
			allToolResults = append(allToolResults, toolResult)

			// Add tool response to conversation
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    toolResult.Result,
				ToolCallID: tc.ID,
			})
		}
	}

	a.AddTokensUsed(totalTokens)

	// Extract context items
	contextItems := a.extractContextFromOutput(finalOutput, allToolResults)

	return &ExecutionResult{
		AgentType:  TypeCoder,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getCoderSystemPrompt returns the specialized system prompt for coding
func (a *CoderAgent) getCoderSystemPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a Coding Agent for taracode, a DevOps AI assistant.

Your role is to write high-quality, maintainable code and make precise edits.

RULES:
1. ALWAYS read files before editing them
2. Make minimal, targeted changes - don't rewrite entire files
3. Follow existing code patterns and conventions
4. Handle errors appropriately
5. Use descriptive variable and function names

WORKFLOW:
1. Read the relevant file(s) to understand the context
2. Plan your changes
3. Make the edit using edit_file or write_file
4. Verify the change was successful

IMPORTANT:
- Prefer edit_file over write_file for existing files
- Use dry_run=true to preview changes before applying
- Create backups of important files before major changes
- If a file doesn't exist, use write_file to create it

`)

	// Add context from execution context
	if len(execCtx.Context) > 0 {
		sb.WriteString("\nCONTEXT FROM PRIOR STEPS:\n")
		for _, item := range execCtx.Context {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Key, item.Value))
		}
	}

	// Add prior results
	if len(execCtx.PriorResults) > 0 {
		sb.WriteString("\nPRIOR STEPS:\n")
		for _, result := range execCtx.PriorResults {
			status := "completed"
			if !result.Success {
				status = "failed: " + result.Error
			}
			sb.WriteString(fmt.Sprintf("- Step %d (%s): %s\n", result.StepIndex+1, result.AgentType.DisplayName(), status))
		}
	}

	return sb.String()
}

// executeToolCall executes a single tool call
func (a *CoderAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	// Check if tool is allowed
	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for coder agent", tc.Function.Name),
			Success:  false,
			Duration: time.Since(startTime),
		}
	}

	// Parse parameters
	var params map[string]interface{}
	if err := parseJSON(tc.Function.Arguments, &params); err != nil {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("failed to parse parameters: %v", err),
			Success:  false,
			Duration: time.Since(startTime),
		}
	}

	// Execute tool
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

// extractContextFromOutput extracts shareable context from the output
func (a *CoderAgent) extractContextFromOutput(output string, toolResults []ToolCallResult) []ContextItem {
	var items []ContextItem

	// Track files that were modified
	var modifiedFiles []string
	var readFiles []string

	for _, tr := range toolResults {
		if !tr.Success {
			continue
		}

		switch tr.ToolName {
		case "write_file", "edit_file", "append_file":
			if path, ok := tr.Params["file_path"].(string); ok {
				modifiedFiles = append(modifiedFiles, path)
			}
		case "read_file":
			if path, ok := tr.Params["file_path"].(string); ok {
				readFiles = append(readFiles, path)
			}
		}
	}

	if len(modifiedFiles) > 0 {
		items = append(items, ContextItem{
			Key:        "files_modified",
			Value:      strings.Join(modifiedFiles, ", "),
			Importance: 9,
			Source:     TypeCoder,
			CreatedAt:  time.Now(),
		})
	}

	if len(readFiles) > 0 {
		items = append(items, ContextItem{
			Key:        "files_read",
			Value:      strings.Join(readFiles, ", "),
			Importance: 5,
			Source:     TypeCoder,
			CreatedAt:  time.Now(),
		})
	}

	// Track tool call count
	items = append(items, ContextItem{
		Key:        "tool_calls",
		Value:      fmt.Sprintf("%d", len(toolResults)),
		Importance: 3,
		Source:     TypeCoder,
		CreatedAt:  time.Now(),
	})

	return items
}
