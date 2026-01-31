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

// SecurityAgent specializes in security scanning and vulnerability analysis
type SecurityAgent struct {
	*BaseAgent
}

// NewSecurityAgent creates a new security agent
func NewSecurityAgent(prov provider.Provider, toolReg *tools.Registry) *SecurityAgent {
	return &SecurityAgent{
		BaseAgent: NewBaseAgent(TypeSecurity, prov, toolReg),
	}
}

// CanHandle returns true for security-related tasks
func (a *SecurityAgent) CanHandle(taskType string) bool {
	return taskType == "security" || taskType == "scan" ||
		taskType == "vulnerability" || taskType == "audit"
}

// Execute runs the security agent
func (a *SecurityAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for security agent")
	}

	systemPrompt := a.getSecuritySystemPrompt(execCtx)

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
				AgentType:  TypeSecurity,
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

	contextItems := a.buildSecurityContext(allToolResults, finalOutput)

	return &ExecutionResult{
		AgentType:  TypeSecurity,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getSecuritySystemPrompt returns the specialized system prompt for security
func (a *SecurityAgent) getSecuritySystemPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a Security Agent for taracode, a DevOps AI assistant.

Your role is to identify security vulnerabilities and provide remediation guidance.

SCANNING STRATEGY:
1. Start with broad scans (trivy_scan, dependency_audit)
2. Follow up with targeted scans based on findings
3. Check for secrets/credentials (gitleaks_scan, secrets_scan)
4. Analyze infrastructure configs (tfsec_scan, kubesec_scan)

SEVERITY LEVELS:
- CRITICAL: Immediate action required, active exploitation possible
- HIGH: Serious vulnerability, fix within 24-48 hours
- MEDIUM: Should be fixed, but not immediately exploitable
- LOW: Minor issues, fix when convenient

SCAN TOOLS:
- trivy_scan: Container/filesystem vulnerabilities
- gitleaks_scan: Git history secrets detection
- secrets_scan: Hardcoded credentials in code
- dependency_audit: npm/pip/go/cargo vulnerabilities
- sast_scan: Static application security testing
- tfsec_scan: Terraform security issues
- kubesec_scan: Kubernetes manifest security

OUTPUT FORMAT:
For each finding:
- Severity: CRITICAL/HIGH/MEDIUM/LOW
- Type: vulnerability/secret/misconfiguration
- Location: file:line or resource
- Description: What was found
- Remediation: How to fix

Summary:
- Critical: X
- High: X
- Medium: X
- Low: X
- Recommendation: Block/Warning/Pass

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
func (a *SecurityAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for security agent", tc.Function.Name),
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

// buildSecurityContext builds context items from security scan results
func (a *SecurityAgent) buildSecurityContext(toolResults []ToolCallResult, output string) []ContextItem {
	var items []ContextItem

	output = strings.ToLower(output)

	// Count severity levels from output
	criticalCount := strings.Count(output, "critical")
	highCount := strings.Count(output, "high")
	mediumCount := strings.Count(output, "medium")
	lowCount := strings.Count(output, "low")

	items = append(items, ContextItem{
		Key:        "critical_findings",
		Value:      fmt.Sprintf("%d", criticalCount),
		Importance: 10,
		Source:     TypeSecurity,
		CreatedAt:  time.Now(),
	})

	items = append(items, ContextItem{
		Key:        "high_findings",
		Value:      fmt.Sprintf("%d", highCount),
		Importance: 9,
		Source:     TypeSecurity,
		CreatedAt:  time.Now(),
	})

	items = append(items, ContextItem{
		Key:        "medium_findings",
		Value:      fmt.Sprintf("%d", mediumCount),
		Importance: 6,
		Source:     TypeSecurity,
		CreatedAt:  time.Now(),
	})

	items = append(items, ContextItem{
		Key:        "low_findings",
		Value:      fmt.Sprintf("%d", lowCount),
		Importance: 3,
		Source:     TypeSecurity,
		CreatedAt:  time.Now(),
	})

	// Determine overall security status
	status := "pass"
	if criticalCount > 0 {
		status = "block"
	} else if highCount > 0 {
		status = "warning"
	}

	items = append(items, ContextItem{
		Key:        "security_status",
		Value:      status,
		Importance: 10,
		Source:     TypeSecurity,
		CreatedAt:  time.Now(),
	})

	// Track which scans were performed
	var scans []string
	for _, tr := range toolResults {
		if tr.Success && strings.Contains(tr.ToolName, "scan") {
			scans = append(scans, tr.ToolName)
		}
	}
	if len(scans) > 0 {
		items = append(items, ContextItem{
			Key:        "scans_performed",
			Value:      strings.Join(scans, ", "),
			Importance: 5,
			Source:     TypeSecurity,
			CreatedAt:  time.Now(),
		})
	}

	return items
}
