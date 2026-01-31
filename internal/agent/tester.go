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

// TesterAgent specializes in test execution and verification
type TesterAgent struct {
	*BaseAgent
}

// NewTesterAgent creates a new tester agent
func NewTesterAgent(prov provider.Provider, toolReg *tools.Registry) *TesterAgent {
	return &TesterAgent{
		BaseAgent: NewBaseAgent(TypeTester, prov, toolReg),
	}
}

// CanHandle returns true for testing-related tasks
func (a *TesterAgent) CanHandle(taskType string) bool {
	return taskType == "test" || taskType == "verify" || taskType == "validate"
}

// Execute runs the tester agent
func (a *TesterAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for tester agent")
	}

	// Build system prompt
	systemPrompt := a.getTesterSystemPrompt(execCtx)

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

		execCtx2, cancel := context.WithTimeout(ctx, time.Duration(a.config.Timeout)*time.Second)
		resp, err := client.CreateChatCompletion(execCtx2, req)
		cancel()

		if err != nil {
			a.IncrementErrors()
			return &ExecutionResult{
				AgentType:  TypeTester,
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

	// Analyze test results
	testsPassed, testsFailed := a.analyzeTestResults(allToolResults)
	contextItems := a.buildTestContext(testsPassed, testsFailed, allToolResults)

	// Determine success based on test results
	success := testsFailed == 0

	return &ExecutionResult{
		AgentType:  TypeTester,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    success,
	}, nil
}

// getTesterSystemPrompt returns the specialized system prompt for testing
func (a *TesterAgent) getTesterSystemPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a Testing Agent for taracode, a DevOps AI assistant.

Your role is to execute tests and verify implementation correctness.

RULES:
1. Run existing test suites first
2. Analyze test output carefully for failures
3. Report specific failure details with line numbers
4. Suggest fixes for failing tests

WORKFLOW:
1. Identify the test command for the project (go test, npm test, pytest, etc.)
2. Run the tests using execute_command
3. Parse the output for failures
4. Report results clearly

TEST COMMANDS BY PROJECT TYPE:
- Go: go test ./... -v
- Node.js: npm test
- Python: pytest -v
- Rust: cargo test

OUTPUT FORMAT:
Summarize test results as:
- Total tests: X
- Passed: Y
- Failed: Z
- Failures: [list specific failures with file:line]

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
func (a *TesterAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for tester agent", tc.Function.Name),
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

// analyzeTestResults analyzes tool results to count test passes/failures
func (a *TesterAgent) analyzeTestResults(toolResults []ToolCallResult) (passed, failed int) {
	for _, tr := range toolResults {
		if tr.ToolName == "execute_command" {
			output := strings.ToLower(tr.Result)
			// Look for common test output patterns
			if strings.Contains(output, "fail") || strings.Contains(output, "error") {
				failed++
			}
			if strings.Contains(output, "pass") || strings.Contains(output, "ok") {
				passed++
			}
		}
	}
	return
}

// buildTestContext builds context items from test results
func (a *TesterAgent) buildTestContext(passed, failed int, toolResults []ToolCallResult) []ContextItem {
	var items []ContextItem

	items = append(items, ContextItem{
		Key:        "tests_passed",
		Value:      fmt.Sprintf("%d", passed),
		Importance: 7,
		Source:     TypeTester,
		CreatedAt:  time.Now(),
	})

	items = append(items, ContextItem{
		Key:        "tests_failed",
		Value:      fmt.Sprintf("%d", failed),
		Importance: 9,
		Source:     TypeTester,
		CreatedAt:  time.Now(),
	})

	status := "passing"
	if failed > 0 {
		status = "failing"
	}
	items = append(items, ContextItem{
		Key:        "test_status",
		Value:      status,
		Importance: 10,
		Source:     TypeTester,
		CreatedAt:  time.Now(),
	})

	return items
}
