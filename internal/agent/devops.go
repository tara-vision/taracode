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

// DevOpsAgent specializes in infrastructure and deployment operations
type DevOpsAgent struct {
	*BaseAgent
}

// NewDevOpsAgent creates a new devops agent
func NewDevOpsAgent(prov provider.Provider, toolReg *tools.Registry) *DevOpsAgent {
	return &DevOpsAgent{
		BaseAgent: NewBaseAgent(TypeDevOps, prov, toolReg),
	}
}

// CanHandle returns true for devops-related tasks
func (a *DevOpsAgent) CanHandle(taskType string) bool {
	return taskType == "deploy" || taskType == "infrastructure" ||
		taskType == "kubernetes" || taskType == "terraform" ||
		taskType == "docker" || taskType == "cloud"
}

// Execute runs the devops agent
func (a *DevOpsAgent) Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error) {
	startTime := time.Now()

	a.SetActive(true)
	a.IncrementInvocations()
	defer a.SetActive(false)

	client := a.GetClient()
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured for devops agent")
	}

	systemPrompt := a.getDevOpsSystemPrompt(execCtx)

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
				AgentType:  TypeDevOps,
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

	contextItems := a.buildDevOpsContext(allToolResults)

	return &ExecutionResult{
		AgentType:  TypeDevOps,
		Output:     finalOutput,
		ToolCalls:  allToolResults,
		Context:    contextItems,
		TokensUsed: totalTokens,
		Duration:   time.Since(startTime),
		Success:    true,
	}, nil
}

// getDevOpsSystemPrompt returns the specialized system prompt for devops
func (a *DevOpsAgent) getDevOpsSystemPrompt(execCtx *ExecutionContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a DevOps Agent for taracode, a DevOps AI assistant.

Your role is to manage infrastructure, deployments, and cloud operations.

SAFETY RULES:
1. ALWAYS use dry-run/plan before apply/destroy
2. Verify current state before making changes
3. Create backups/checkpoints before destructive operations
4. Prefer rolling deployments over recreate
5. Check resource dependencies before deletion

KUBERNETES WORKFLOW:
1. kubectl get to check current state
2. kubectl apply --dry-run=client to validate
3. kubectl apply to make changes
4. kubectl get/describe to verify

TERRAFORM WORKFLOW:
1. terraform init (if needed)
2. terraform plan to preview
3. terraform apply (with approval)
4. terraform output to verify

DOCKER WORKFLOW:
1. docker ps to check running containers
2. docker build to create images
3. docker-compose up --dry-run (if supported)
4. docker-compose up to deploy

CLOUD BEST PRACTICES:
- Use infrastructure as code when possible
- Tag resources appropriately
- Monitor costs and resource usage
- Implement least-privilege access

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
func (a *DevOpsAgent) executeToolCall(workingDir string, tc openai.ToolCall) ToolCallResult {
	startTime := time.Now()

	if !IsToolAllowed(tc.Function.Name, a.config.ToolCategories) {
		return ToolCallResult{
			ToolName: tc.Function.Name,
			Result:   fmt.Sprintf("tool %s not allowed for devops agent", tc.Function.Name),
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

// buildDevOpsContext builds context items from devops operations
func (a *DevOpsAgent) buildDevOpsContext(toolResults []ToolCallResult) []ContextItem {
	var items []ContextItem

	// Track infrastructure operations
	var k8sOps, tfOps, dockerOps []string

	for _, tr := range toolResults {
		if !tr.Success {
			continue
		}

		switch {
		case strings.HasPrefix(tr.ToolName, "kubectl_") || strings.HasPrefix(tr.ToolName, "helm_"):
			k8sOps = append(k8sOps, tr.ToolName)
		case strings.HasPrefix(tr.ToolName, "terraform_"):
			tfOps = append(tfOps, tr.ToolName)
		case strings.HasPrefix(tr.ToolName, "docker_"):
			dockerOps = append(dockerOps, tr.ToolName)
		}
	}

	if len(k8sOps) > 0 {
		items = append(items, ContextItem{
			Key:        "kubernetes_ops",
			Value:      strings.Join(k8sOps, ", "),
			Importance: 7,
			Source:     TypeDevOps,
			CreatedAt:  time.Now(),
		})
	}

	if len(tfOps) > 0 {
		items = append(items, ContextItem{
			Key:        "terraform_ops",
			Value:      strings.Join(tfOps, ", "),
			Importance: 7,
			Source:     TypeDevOps,
			CreatedAt:  time.Now(),
		})
	}

	if len(dockerOps) > 0 {
		items = append(items, ContextItem{
			Key:        "docker_ops",
			Value:      strings.Join(dockerOps, ", "),
			Importance: 7,
			Source:     TypeDevOps,
			CreatedAt:  time.Now(),
		})
	}

	return items
}
