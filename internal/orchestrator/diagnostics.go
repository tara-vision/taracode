package orchestrator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tara-vision/taracode/internal/agent"
)

// FailureDiagnostics provides detailed analysis of failures
type FailureDiagnostics struct {
	ToolName    string                 `json:"tool_name"`
	Params      map[string]interface{} `json:"params"`
	Error       string                 `json:"error"`
	ExitCode    int                    `json:"exit_code,omitempty"`
	Stderr      string                 `json:"stderr,omitempty"`
	Stdout      string                 `json:"stdout,omitempty"`
	RootCause   string                 `json:"root_cause"`
	Suggestion  string                 `json:"suggestion"`
	Context     string                 `json:"context,omitempty"`
	Severity    string                 `json:"severity"` // low, medium, high, critical
	Recoverable bool                   `json:"recoverable"`
}

// DiagnoseFailure analyzes a tool call failure and provides actionable information
func DiagnoseFailure(toolName string, params map[string]interface{}, err error, output string) *FailureDiagnostics {
	errStr := "unknown error"
	if err != nil {
		errStr = err.Error()
	}

	diag := &FailureDiagnostics{
		ToolName:    toolName,
		Params:      params,
		Error:       errStr,
		Severity:    "medium",
		Recoverable: true,
	}

	// Parse stderr/stdout from output
	diag.parseOutput(output)

	// Analyze based on tool type
	switch {
	case strings.HasPrefix(toolName, "kubectl"):
		diag.analyzeKubectlError()
	case strings.HasPrefix(toolName, "terraform"):
		diag.analyzeTerraformError()
	case strings.HasPrefix(toolName, "docker"):
		diag.analyzeDockerError()
	case toolName == "execute_command":
		diag.analyzeCommandError()
	case toolName == "write_file" || toolName == "edit_file":
		diag.analyzeFileError()
	case toolName == "git_commit" || strings.HasPrefix(toolName, "git_"):
		diag.analyzeGitError()
	default:
		diag.analyzeGenericError()
	}

	return diag
}

func (d *FailureDiagnostics) parseOutput(output string) {
	// Extract exit code if present
	exitCodePattern := regexp.MustCompile(`exit code:?\s*(\d+)`)
	if match := exitCodePattern.FindStringSubmatch(output); len(match) > 1 {
		fmt.Sscanf(match[1], "%d", &d.ExitCode)
	}

	// Separate stderr from stdout
	if strings.Contains(output, "stderr:") {
		parts := strings.SplitN(output, "stderr:", 2)
		if len(parts) > 0 {
			d.Stdout = strings.TrimSpace(parts[0])
		}
		if len(parts) > 1 {
			d.Stderr = strings.TrimSpace(parts[1])
		}
	} else {
		d.Stderr = output
	}
}

func (d *FailureDiagnostics) analyzeKubectlError() {
	stderr := strings.ToLower(d.Stderr)

	switch {
	case strings.Contains(stderr, "connection refused"):
		d.RootCause = "Cannot connect to Kubernetes cluster"
		d.Suggestion = "Check if the cluster is running and KUBECONFIG is set correctly"
		d.Severity = "high"
	case strings.Contains(stderr, "not found"):
		d.RootCause = "Resource does not exist in the cluster"
		d.Suggestion = "Verify the resource name and namespace are correct"
	case strings.Contains(stderr, "forbidden"):
		d.RootCause = "Insufficient permissions for this operation"
		d.Suggestion = "Check RBAC permissions for your service account"
		d.Severity = "high"
	case strings.Contains(stderr, "validationerror"):
		d.RootCause = "YAML manifest validation failed"
		d.Suggestion = "Check the manifest syntax and required fields"
		d.extractValidationContext()
	case strings.Contains(stderr, "already exists"):
		d.RootCause = "Resource already exists"
		d.Suggestion = "Use 'apply' instead of 'create', or delete the existing resource first"
		d.Recoverable = true
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeTerraformError() {
	stderr := strings.ToLower(d.Stderr)

	switch {
	case strings.Contains(stderr, "no configuration files"):
		d.RootCause = "No Terraform configuration found"
		d.Suggestion = "Ensure .tf files exist in the working directory"
		d.Severity = "high"
	case strings.Contains(stderr, "state lock"):
		d.RootCause = "Terraform state is locked by another process"
		d.Suggestion = "Wait for the other process to complete, or use 'terraform force-unlock'"
		d.Severity = "medium"
	case strings.Contains(stderr, "provider"):
		d.RootCause = "Provider configuration issue"
		d.Suggestion = "Run 'terraform init' to initialize providers"
	case strings.Contains(stderr, "invalid"):
		d.RootCause = "Invalid configuration syntax"
		d.Suggestion = "Check the HCL syntax in your .tf files"
		d.extractValidationContext()
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeDockerError() {
	stderr := strings.ToLower(d.Stderr)

	switch {
	case strings.Contains(stderr, "cannot connect to the docker daemon"):
		d.RootCause = "Docker daemon is not running"
		d.Suggestion = "Start Docker Desktop or the docker service"
		d.Severity = "high"
	case strings.Contains(stderr, "no such image"):
		d.RootCause = "Docker image not found"
		d.Suggestion = "Pull the image first with 'docker pull'"
	case strings.Contains(stderr, "port is already allocated"):
		d.RootCause = "Port is already in use"
		d.Suggestion = "Stop the process using this port or choose a different port"
	case strings.Contains(stderr, "permission denied"):
		d.RootCause = "Permission denied accessing Docker"
		d.Suggestion = "Add your user to the docker group or use sudo"
		d.Severity = "high"
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeCommandError() {
	stderr := strings.ToLower(d.Stderr)

	switch {
	case strings.Contains(stderr, "command not found"):
		d.RootCause = "Command not found in PATH"
		d.Suggestion = "Install the required tool or check PATH configuration"
		d.Severity = "high"
	case strings.Contains(stderr, "permission denied"):
		d.RootCause = "Permission denied"
		d.Suggestion = "Check file permissions or run with elevated privileges"
	case strings.Contains(stderr, "no such file or directory"):
		d.RootCause = "File or directory does not exist"
		d.Suggestion = "Verify the path is correct and the file exists"
	case d.ExitCode == 1:
		d.RootCause = "Command failed with generic error"
		d.Suggestion = "Check the command output for specific error details"
	case d.ExitCode == 127:
		d.RootCause = "Command not found"
		d.Suggestion = "Ensure the command is installed and in PATH"
	case d.ExitCode == 126:
		d.RootCause = "Command not executable"
		d.Suggestion = "Check file permissions (chmod +x)"
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeFileError() {
	error := strings.ToLower(d.Error)

	switch {
	case strings.Contains(error, "permission denied"):
		d.RootCause = "Permission denied accessing file"
		d.Suggestion = "Check file permissions or ownership"
		d.Severity = "high"
	case strings.Contains(error, "no such file"):
		d.RootCause = "File does not exist"
		d.Suggestion = "Verify the file path is correct"
	case strings.Contains(error, "is a directory"):
		d.RootCause = "Expected file but got directory"
		d.Suggestion = "Provide a file path, not a directory"
	case strings.Contains(error, "disk full") || strings.Contains(error, "no space"):
		d.RootCause = "Disk space exhausted"
		d.Suggestion = "Free up disk space"
		d.Severity = "critical"
		d.Recoverable = false
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeGitError() {
	stderr := strings.ToLower(d.Stderr)

	switch {
	case strings.Contains(stderr, "not a git repository"):
		d.RootCause = "Not in a git repository"
		d.Suggestion = "Initialize with 'git init' or navigate to a git repository"
	case strings.Contains(stderr, "nothing to commit"):
		d.RootCause = "No changes to commit"
		d.Suggestion = "Make changes before committing"
		d.Severity = "low"
	case strings.Contains(stderr, "merge conflict"):
		d.RootCause = "Merge conflict detected"
		d.Suggestion = "Resolve conflicts manually before proceeding"
		d.Severity = "high"
	case strings.Contains(stderr, "authentication failed"):
		d.RootCause = "Git authentication failed"
		d.Suggestion = "Check your credentials or SSH key configuration"
		d.Severity = "high"
	default:
		d.analyzeGenericError()
	}
}

func (d *FailureDiagnostics) analyzeGenericError() {
	if d.RootCause == "" {
		d.RootCause = "Operation failed"
	}
	if d.Suggestion == "" {
		d.Suggestion = "Review the error message and tool parameters"
	}
}

func (d *FailureDiagnostics) extractValidationContext() {
	// Try to extract line numbers or field names from validation errors
	linePattern := regexp.MustCompile(`line\s+(\d+)`)
	if match := linePattern.FindStringSubmatch(d.Stderr); len(match) > 1 {
		d.Context = fmt.Sprintf("Error near line %s", match[1])
	}

	fieldPattern := regexp.MustCompile(`field\s+"([^"]+)"`)
	if match := fieldPattern.FindStringSubmatch(d.Stderr); len(match) > 1 {
		if d.Context != "" {
			d.Context += fmt.Sprintf(", field: %s", match[1])
		} else {
			d.Context = fmt.Sprintf("Field: %s", match[1])
		}
	}
}

// Format returns a formatted string representation of the diagnostics
func (d *FailureDiagnostics) Format(verbose bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Tool: %s\n", d.ToolName))

	if verbose && len(d.Params) > 0 {
		sb.WriteString("Params:\n")
		for k, v := range d.Params {
			sb.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
	}

	sb.WriteString(fmt.Sprintf("Error: %s\n", d.Error))

	if d.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf("Exit Code: %d\n", d.ExitCode))
	}

	if verbose && d.Stderr != "" {
		sb.WriteString("\nstderr:\n")
		// Limit stderr to 500 chars
		stderr := d.Stderr
		if len(stderr) > 500 {
			stderr = stderr[:500] + "..."
		}
		sb.WriteString(stderr)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\nRoot Cause: %s\n", d.RootCause))
	sb.WriteString(fmt.Sprintf("Suggestion: %s\n", d.Suggestion))

	if d.Context != "" {
		sb.WriteString(fmt.Sprintf("Context: %s\n", d.Context))
	}

	sb.WriteString(fmt.Sprintf("Severity: %s\n", d.Severity))

	if d.Recoverable {
		sb.WriteString("Status: Recoverable\n")
	} else {
		sb.WriteString("Status: Not recoverable\n")
	}

	return sb.String()
}

// AgentContextUsage tracks context usage per agent
type AgentContextUsage struct {
	AgentType    agent.Type `json:"agent_type"`
	TokensUsed   int        `json:"tokens_used"`
	TokensBudget int        `json:"tokens_budget"`
	ItemCount    int        `json:"item_count"`
}

// GetAgentContextUsage returns context usage for all agents
func (o *Orchestrator) GetAgentContextUsage() []AgentContextUsage {
	var usage []AgentContextUsage

	for _, agentType := range agent.AllTypes() {
		ag, err := o.registry.Get(agentType)
		if err != nil {
			continue
		}

		cfg := ag.Config()
		state := ag.GetState()

		usage = append(usage, AgentContextUsage{
			AgentType:    agentType,
			TokensUsed:   state.TokensUsed,
			TokensBudget: cfg.MaxContextTokens,
			ItemCount:    0, // Would need context tracking per agent
		})
	}

	return usage
}
