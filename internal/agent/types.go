package agent

import (
	"context"
	"time"

	"github.com/tara-vision/taracode/internal/tools"
)

// Type identifies the specialized agent
type Type string

const (
	TypePlanner     Type = "planner"
	TypeCoder       Type = "coder"
	TypeTester      Type = "tester"
	TypeReviewer    Type = "reviewer"
	TypeDevOps      Type = "devops"
	TypeSecurity    Type = "security"
	TypeDiagnostics Type = "diagnostics"
)

// String returns the string representation of the agent type
func (t Type) String() string {
	return string(t)
}

// DisplayName returns a human-readable name for the agent type
func (t Type) DisplayName() string {
	switch t {
	case TypePlanner:
		return "Planner"
	case TypeCoder:
		return "Coder"
	case TypeTester:
		return "Tester"
	case TypeReviewer:
		return "Reviewer"
	case TypeDevOps:
		return "DevOps"
	case TypeSecurity:
		return "Security"
	case TypeDiagnostics:
		return "Diagnostics"
	default:
		return string(t)
	}
}

// Description returns a brief description of what this agent does
func (t Type) Description() string {
	switch t {
	case TypePlanner:
		return "Task decomposition and dependency analysis"
	case TypeCoder:
		return "Code generation and editing"
	case TypeTester:
		return "Test execution and output analysis"
	case TypeReviewer:
		return "Code review and quality checks"
	case TypeDevOps:
		return "Infrastructure and deployment operations"
	case TypeSecurity:
		return "Security scanning and vulnerability analysis"
	case TypeDiagnostics:
		return "Failure analysis and root cause detection"
	default:
		return "Unknown agent type"
	}
}

// AllTypes returns all available agent types
func AllTypes() []Type {
	return []Type{
		TypePlanner,
		TypeCoder,
		TypeTester,
		TypeReviewer,
		TypeDevOps,
		TypeSecurity,
		TypeDiagnostics,
	}
}

// Config defines an agent's configuration
type Config struct {
	Type             Type     `yaml:"type" json:"type"`
	Model            string   `yaml:"model" json:"model"`
	Host             string   `yaml:"host,omitempty" json:"host,omitempty"` // Host name from hosts config
	Temperature      float32  `yaml:"temperature" json:"temperature"`
	TopP             float32  `yaml:"top_p" json:"top_p"`
	NumPredict       int      `yaml:"num_predict" json:"num_predict"`
	MaxContextTokens int      `yaml:"max_context_tokens" json:"max_context_tokens"`
	ToolCategories   []string `yaml:"tool_categories,omitempty" json:"tool_categories,omitempty"`
	MaxToolIter      int      `yaml:"max_tool_iterations" json:"max_tool_iterations"`
	Timeout          int      `yaml:"timeout" json:"timeout"` // seconds
	AutoInvoke       bool     `yaml:"auto_invoke,omitempty" json:"auto_invoke,omitempty"`
	ReviewStrictness string   `yaml:"review_strictness,omitempty" json:"review_strictness,omitempty"` // low, medium, high
}

// DefaultConfig returns the default configuration for an agent type
func DefaultConfig(agentType Type) Config {
	switch agentType {
	case TypePlanner:
		return Config{
			Type:             TypePlanner,
			Model:            "gemma3:12b",
			Temperature:      0.3,
			MaxContextTokens: 4096,
			ToolCategories:   []string{"file"},
			MaxToolIter:      3,
			Timeout:          60,
		}
	case TypeCoder:
		return Config{
			Type:             TypeCoder,
			Model:            "gemma3:27b",
			Temperature:      0.4,
			MaxContextTokens: 16384,
			ToolCategories:   []string{"file", "git", "command"},
			MaxToolIter:      10,
			Timeout:          300,
		}
	case TypeTester:
		return Config{
			Type:             TypeTester,
			Model:            "gemma3:27b",
			Temperature:      0.2,
			MaxContextTokens: 8192,
			ToolCategories:   []string{"file", "command"},
			MaxToolIter:      5,
			Timeout:          180,
		}
	case TypeReviewer:
		return Config{
			Type:             TypeReviewer,
			Model:            "gemma3:27b",
			Temperature:      0.5,
			MaxContextTokens: 12288,
			ToolCategories:   []string{"file", "search"},
			MaxToolIter:      5,
			Timeout:          180,
			ReviewStrictness: "medium",
		}
	case TypeDevOps:
		return Config{
			Type:             TypeDevOps,
			Model:            "gemma3:27b",
			Temperature:      0.3,
			MaxContextTokens: 12288,
			ToolCategories:   []string{"kubernetes", "terraform", "docker", "cloud"},
			MaxToolIter:      10,
			Timeout:          300,
		}
	case TypeSecurity:
		return Config{
			Type:             TypeSecurity,
			Model:            "gemma3:27b",
			Temperature:      0.2,
			MaxContextTokens: 12288,
			ToolCategories:   []string{"security", "file"},
			MaxToolIter:      5,
			Timeout:          300,
		}
	case TypeDiagnostics:
		return Config{
			Type:             TypeDiagnostics,
			Model:            "gemma3:12b",
			Temperature:      0.2,
			MaxContextTokens: 4096,
			ToolCategories:   []string{"file", "command"},
			MaxToolIter:      3,
			Timeout:          60,
			AutoInvoke:       true,
		}
	default:
		return Config{
			Type:             agentType,
			Model:            "gemma3:27b",
			Temperature:      0.4,
			MaxContextTokens: 8192,
			MaxToolIter:      5,
			Timeout:          120,
		}
	}
}

// State tracks runtime state of an agent
type State struct {
	Type         Type      `json:"type"`
	Active       bool      `json:"active"`
	CurrentStep  int       `json:"current_step"`
	TokensUsed   int       `json:"tokens_used"`
	LastActivity time.Time `json:"last_activity"`
	ErrorCount   int       `json:"error_count"`
	Invocations  int       `json:"invocations"`
}

// ContextItem represents a piece of shareable context between agents
type ContextItem struct {
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Importance int       `json:"importance"` // 1-10, higher = more important
	Source     Type      `json:"source"`     // Agent that produced it
	CreatedAt  time.Time `json:"created_at"`
}

// ExecutionContext provides context for agent execution
type ExecutionContext struct {
	TaskID       string                 `json:"task_id"`
	StepIndex    int                    `json:"step_index"`
	Prompt       string                 `json:"prompt"`
	SharedMemory map[string]interface{} `json:"shared_memory"`
	PriorResults []StepResult           `json:"prior_results"`
	Context      []ContextItem          `json:"context"`
	WorkingDir   string                 `json:"working_dir"`
}

// StepResult captures the result of a previous step
type StepResult struct {
	StepIndex  int           `json:"step_index"`
	AgentType  Type          `json:"agent_type"`
	Output     string        `json:"output"`
	Success    bool          `json:"success"`
	Error      string        `json:"error,omitempty"`
	Context    []ContextItem `json:"context,omitempty"`
	TokensUsed int           `json:"tokens_used"`
	Duration   time.Duration `json:"duration"`
}

// ToolCallResult captures the result of a tool execution
type ToolCallResult struct {
	ToolName string                 `json:"tool_name"`
	Params   map[string]interface{} `json:"params"`
	Result   string                 `json:"result"`
	Success  bool                   `json:"success"`
	Duration time.Duration          `json:"duration"`
}

// ExecutionResult captures agent execution output
type ExecutionResult struct {
	AgentType  Type             `json:"agent_type"`
	Output     string           `json:"output"`
	ToolCalls  []ToolCallResult `json:"tool_calls,omitempty"`
	Context    []ContextItem    `json:"context,omitempty"`
	TokensUsed int              `json:"tokens_used"`
	Duration   time.Duration    `json:"duration"`
	Success    bool             `json:"success"`
	Error      string           `json:"error,omitempty"`
}

// Handoff represents a handoff between agents
type Handoff struct {
	FromAgent    Type          `json:"from_agent"`
	ToAgent      Type          `json:"to_agent"`
	StepIndex    int           `json:"step_index"`
	Context      []ContextItem `json:"context"`
	Continuation string        `json:"continuation"` // What the next agent should do
	Timestamp    time.Time     `json:"timestamp"`
}

// Agent interface for specialized agents
type Agent interface {
	// Type returns the agent type
	Type() Type

	// Config returns the agent configuration
	Config() Config

	// Execute runs the agent with the given context
	Execute(ctx context.Context, execCtx *ExecutionContext) (*ExecutionResult, error)

	// GetState returns the current agent state
	GetState() State

	// SetConfig updates the agent configuration
	SetConfig(cfg Config)

	// GetToolRegistry returns the tools available to this agent
	GetToolRegistry() *tools.Registry

	// CanHandle returns true if this agent can handle the given task type
	CanHandle(taskType string) bool
}

// ToolCategory defines categories of tools for access control
type ToolCategory string

const (
	ToolCategoryFile       ToolCategory = "file"
	ToolCategoryGit        ToolCategory = "git"
	ToolCategoryCommand    ToolCategory = "command"
	ToolCategorySearch     ToolCategory = "search"
	ToolCategoryWeb        ToolCategory = "web"
	ToolCategoryKubernetes ToolCategory = "kubernetes"
	ToolCategoryTerraform  ToolCategory = "terraform"
	ToolCategoryDocker     ToolCategory = "docker"
	ToolCategoryCloud      ToolCategory = "cloud"
	ToolCategorySecurity   ToolCategory = "security"
	ToolCategoryMCP        ToolCategory = "mcp"
)

// ToolCategoryMap maps tool names to their categories
var ToolCategoryMap = map[string]ToolCategory{
	// File operations
	"read_file":        ToolCategoryFile,
	"write_file":       ToolCategoryFile,
	"append_file":      ToolCategoryFile,
	"edit_file":        ToolCategoryFile,
	"insert_lines":     ToolCategoryFile,
	"replace_lines":    ToolCategoryFile,
	"delete_lines":     ToolCategoryFile,
	"copy_file":        ToolCategoryFile,
	"move_file":        ToolCategoryFile,
	"delete_file":      ToolCategoryFile,
	"create_directory": ToolCategoryFile,
	"list_files":       ToolCategoryFile,
	"find_files":       ToolCategoryFile,

	// Command/Search
	"execute_command": ToolCategoryCommand,
	"search_files":    ToolCategorySearch,

	// Git
	"git_status": ToolCategoryGit,
	"git_diff":   ToolCategoryGit,
	"git_log":    ToolCategoryGit,
	"git_add":    ToolCategoryGit,
	"git_commit": ToolCategoryGit,
	"git_branch": ToolCategoryGit,
	"git_stash":  ToolCategoryGit,

	// Web
	"web_search": ToolCategoryWeb,
	"web_fetch":  ToolCategoryWeb,

	// Kubernetes
	"kubectl_get":      ToolCategoryKubernetes,
	"kubectl_apply":    ToolCategoryKubernetes,
	"kubectl_delete":   ToolCategoryKubernetes,
	"kubectl_describe": ToolCategoryKubernetes,
	"kubectl_logs":     ToolCategoryKubernetes,
	"kubectl_exec":     ToolCategoryKubernetes,
	"helm_list":        ToolCategoryKubernetes,
	"helm_install":     ToolCategoryKubernetes,

	// Terraform
	"terraform_init":    ToolCategoryTerraform,
	"terraform_plan":    ToolCategoryTerraform,
	"terraform_apply":   ToolCategoryTerraform,
	"terraform_destroy": ToolCategoryTerraform,
	"terraform_output":  ToolCategoryTerraform,
	"terraform_state":   ToolCategoryTerraform,

	// Docker
	"docker_build":   ToolCategoryDocker,
	"docker_ps":      ToolCategoryDocker,
	"docker_logs":    ToolCategoryDocker,
	"docker_compose": ToolCategoryDocker,
	"docker_exec":    ToolCategoryDocker,

	// Cloud
	"aws_cli": ToolCategoryCloud,
	"aws_ecs": ToolCategoryCloud,
	"aws_eks": ToolCategoryCloud,
	"az_cli":  ToolCategoryCloud,
	"az_aks":  ToolCategoryCloud,
	"gcloud":  ToolCategoryCloud,
	"gke":     ToolCategoryCloud,

	// Security
	"trivy_scan":       ToolCategorySecurity,
	"gitleaks_scan":    ToolCategorySecurity,
	"secrets_scan":     ToolCategorySecurity,
	"dependency_audit": ToolCategorySecurity,
	"sast_scan":        ToolCategorySecurity,
	"tfsec_scan":       ToolCategorySecurity,
	"kubesec_scan":     ToolCategorySecurity,
}

// GetToolCategory returns the category for a tool name
func GetToolCategory(toolName string) ToolCategory {
	if cat, ok := ToolCategoryMap[toolName]; ok {
		return cat
	}
	// Check for MCP tools (prefixed with server name)
	if len(toolName) > 0 && toolName[0] != '_' {
		// Could be an MCP tool
		return ToolCategoryMCP
	}
	return ""
}

// IsToolAllowed checks if a tool is allowed for the given categories
func IsToolAllowed(toolName string, allowedCategories []string) bool {
	if len(allowedCategories) == 0 {
		return true // No restrictions
	}

	toolCat := GetToolCategory(toolName)
	if toolCat == "" {
		return true // Unknown tools are allowed by default
	}

	for _, cat := range allowedCategories {
		if ToolCategory(cat) == toolCat {
			return true
		}
	}
	return false
}
