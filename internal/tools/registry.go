package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/history"
)

type ToolExecutor func(params map[string]interface{}, workingDir string) (string, error)

type Registry struct {
	tools          map[string]ToolExecutor
	mcpTools       map[string]string // tool name -> server name
	historyManager *history.Manager
}

// fileMutationTools lists tools that modify files and need history tracking
var fileMutationTools = map[string]bool{
	"write_file":    true,
	"append_file":   true,
	"edit_file":     true,
	"insert_lines":  true,
	"replace_lines": true,
	"delete_lines":  true,
	"copy_file":     true,
	"move_file":     true,
	"delete_file":   true,
}

func NewRegistry() *Registry {
	r := &Registry{
		tools:    make(map[string]ToolExecutor),
		mcpTools: make(map[string]string),
	}

	// Register all tools
	// File operations
	r.RegisterTool("read_file", ReadFile)
	r.RegisterTool("write_file", WriteFile)
	r.RegisterTool("append_file", AppendFile)
	r.RegisterTool("edit_file", EditFile)
	r.RegisterTool("insert_lines", InsertLines)
	r.RegisterTool("replace_lines", ReplaceLines)
	r.RegisterTool("delete_lines", DeleteLines)
	r.RegisterTool("copy_file", CopyFile)
	r.RegisterTool("move_file", MoveFile)
	r.RegisterTool("delete_file", DeleteFile)
	r.RegisterTool("create_directory", CreateDirectory)
	r.RegisterTool("list_files", ListFiles)
	r.RegisterTool("find_files", FindFiles)

	// Command execution
	r.RegisterTool("execute_command", ExecuteCommand)
	r.RegisterTool("search_files", SearchFiles)

	// Git operations
	r.RegisterTool("git_status", GitStatus)
	r.RegisterTool("git_diff", GitDiff)
	r.RegisterTool("git_log", GitLog)
	r.RegisterTool("git_add", GitAdd)
	r.RegisterTool("git_commit", GitCommit)
	r.RegisterTool("git_branch", GitBranch)
	r.RegisterTool("git_stash", GitStash)

	// Web tools
	r.RegisterTool("web_search", WebSearch)
	r.RegisterTool("web_fetch", WebFetch)

	// Utility tools
	r.RegisterTool("get_datetime", GetDateTime)

	// Kubernetes tools
	r.RegisterTool("kubectl_get", KubectlGet)
	r.RegisterTool("kubectl_apply", KubectlApply)
	r.RegisterTool("kubectl_delete", KubectlDelete)
	r.RegisterTool("kubectl_describe", KubectlDescribe)
	r.RegisterTool("kubectl_logs", KubectlLogs)
	r.RegisterTool("kubectl_exec", KubectlExec)
	r.RegisterTool("helm_list", HelmList)
	r.RegisterTool("helm_install", HelmInstall)

	// Terraform tools
	r.RegisterTool("terraform_init", TerraformInit)
	r.RegisterTool("terraform_plan", TerraformPlan)
	r.RegisterTool("terraform_apply", TerraformApply)
	r.RegisterTool("terraform_destroy", TerraformDestroy)
	r.RegisterTool("terraform_output", TerraformOutput)
	r.RegisterTool("terraform_state", TerraformState)

	// Docker tools
	r.RegisterTool("docker_build", DockerBuild)
	r.RegisterTool("docker_ps", DockerPs)
	r.RegisterTool("docker_logs", DockerLogs)
	r.RegisterTool("docker_compose", DockerCompose)
	r.RegisterTool("docker_exec", DockerExec)

	// AWS tools
	r.RegisterTool("aws_cli", AwsCli)
	r.RegisterTool("aws_ecs", AwsEcs)
	r.RegisterTool("aws_eks", AwsEks)

	// Azure tools
	r.RegisterTool("az_cli", AzCli)
	r.RegisterTool("az_aks", AzAks)

	// GCP tools
	r.RegisterTool("gcloud", GcloudCli)
	r.RegisterTool("gke", GkeCli)

	// Security tools
	r.RegisterTool("trivy_scan", TrivyScan)
	r.RegisterTool("gitleaks_scan", GitleaksScan)
	r.RegisterTool("secrets_scan", SecretsScan)
	r.RegisterTool("dependency_audit", DependencyAudit)
	r.RegisterTool("sast_scan", SASTScan)
	r.RegisterTool("tfsec_scan", TfsecScan)
	r.RegisterTool("kubesec_scan", KubesecScan)

	return r
}

func (r *Registry) RegisterTool(name string, executor ToolExecutor) {
	r.tools[name] = executor
}

// RegisterMCPTool registers an MCP tool with the registry
func (r *Registry) RegisterMCPTool(name, serverName string, executor ToolExecutor) {
	r.tools[name] = executor
	r.mcpTools[name] = serverName
}

// IsMCPTool checks if a tool is from an MCP server
func (r *Registry) IsMCPTool(name string) bool {
	_, ok := r.mcpTools[name]
	return ok
}

// GetMCPToolServer returns the server name for an MCP tool
func (r *Registry) GetMCPToolServer(name string) string {
	return r.mcpTools[name]
}

// UnregisterMCPTools removes all MCP tools from a specific server
func (r *Registry) UnregisterMCPTools(serverName string) {
	for name, server := range r.mcpTools {
		if server == serverName {
			delete(r.tools, name)
			delete(r.mcpTools, name)
		}
	}
}

// GetMCPTools returns all registered MCP tool names grouped by server
func (r *Registry) GetMCPTools() map[string][]string {
	result := make(map[string][]string)
	for name, server := range r.mcpTools {
		result[server] = append(result[server], name)
	}
	return result
}

// SetHistoryManager sets the history manager for operation tracking
func (r *Registry) SetHistoryManager(hm *history.Manager) {
	r.historyManager = hm
}

// GetHistoryManager returns the history manager
func (r *Registry) GetHistoryManager() *history.Manager {
	return r.historyManager
}

func (r *Registry) ExecuteTool(name string, params map[string]interface{}, workingDir string) (string, error) {
	executor, exists := r.tools[name]
	if !exists {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Check if this is a file mutation tool that needs history tracking
	needsHistory := fileMutationTools[name] && r.historyManager != nil

	var backupPath string
	var deletedContent string
	var originalPath string
	var target string

	if needsHistory {
		// Get target file path from params
		target = r.getTargetPath(name, params, workingDir)

		// Create backup before mutation (for write/edit tools)
		if target != "" && r.needsBackup(name) {
			if bp, err := r.historyManager.CreateBackup(target); err == nil {
				backupPath = bp
			}
		}

		// Capture deleted content for delete_file
		if name == "delete_file" && target != "" {
			if content, err := r.historyManager.CaptureDeletedContent(target); err == nil {
				deletedContent = content
			}
		}

		// Capture original path for move_file
		if name == "move_file" {
			originalPath = target
			// For move, the target after operation is dest_path
			if dest, ok := params["dest_path"].(string); ok {
				if !filepath.IsAbs(dest) {
					target = filepath.Join(workingDir, dest)
				} else {
					target = dest
				}
			}
		}

		// For copy_file, track the created file
		if name == "copy_file" {
			if dest, ok := params["dest_path"].(string); ok {
				if !filepath.IsAbs(dest) {
					target = filepath.Join(workingDir, dest)
				} else {
					target = dest
				}
			}
		}
	}

	// Execute the tool
	result, err := executor(params, workingDir)

	// Record operation in history
	if needsHistory {
		op := history.Operation{
			Timestamp:  time.Now(),
			Tool:       name,
			Type:       history.ToolToOperationType(name),
			Params:     params,
			Target:     target,
			BackupPath: backupPath,
			Success:    err == nil,
		}

		if err != nil {
			op.Result = err.Error()
		} else {
			op.Result = "success"
		}

		// Add additional info based on tool type
		if name == "delete_file" {
			op.DeletedContent = deletedContent
		}
		if name == "move_file" {
			op.OriginalPath = originalPath
		}
		if name == "copy_file" {
			op.CreatedPath = target
		}

		// Record even if operation failed (for audit purposes)
		_ = r.historyManager.Record(op)
	}

	return result, err
}

// getTargetPath extracts the primary file path from tool parameters
func (r *Registry) getTargetPath(toolName string, params map[string]interface{}, workingDir string) string {
	var filePath string

	switch toolName {
	case "copy_file", "move_file":
		if p, ok := params["source_path"].(string); ok {
			filePath = p
		}
	default:
		if p, ok := params["file_path"].(string); ok {
			filePath = p
		}
	}

	if filePath == "" {
		return ""
	}

	// Resolve to absolute path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workingDir, filePath)
	}

	return filePath
}

// needsBackup returns whether a tool needs a backup before execution
func (r *Registry) needsBackup(toolName string) bool {
	switch toolName {
	case "write_file", "append_file", "edit_file", "insert_lines", "replace_lines", "delete_lines":
		return true
	default:
		return false
	}
}

// IsFileMutationTool checks if a tool modifies files
func IsFileMutationTool(name string) bool {
	return fileMutationTools[name]
}

// GetToolNames returns a list of all registered tool names
func (r *Registry) GetToolNames() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// HasTool checks if a tool exists
func (r *Registry) HasTool(name string) bool {
	_, exists := r.tools[name]
	return exists
}

// GetExecutor returns the executor function for a tool
func (r *Registry) GetExecutor(name string) ToolExecutor {
	return r.tools[name]
}

// GetToolList returns a list of all registered tools with descriptions
func (r *Registry) GetToolList() []ToolInfo {
	// Map of tool names to descriptions
	descriptions := map[string]string{
		"read_file":         "Read file contents",
		"write_file":        "Create or overwrite a file",
		"append_file":       "Append content to a file",
		"edit_file":         "Find and replace text in a file",
		"insert_lines":      "Insert lines at a specific position",
		"replace_lines":     "Replace a range of lines",
		"delete_lines":      "Delete a range of lines",
		"copy_file":         "Copy a file",
		"move_file":         "Move or rename a file",
		"delete_file":       "Delete a file",
		"create_directory":  "Create a directory",
		"list_files":        "List files in a directory",
		"find_files":        "Find files matching a glob pattern",
		"search_files":      "Search for text patterns in files",
		"execute_command":   "Execute a shell command",
		"web_search":        "Search the web",
		"web_fetch":         "Fetch content from a URL",
		"get_datetime":      "Get current date and time",
		"git_status":        "Show git repository status",
		"git_diff":          "Show git diff",
		"git_log":           "Show git commit history",
		"git_add":           "Stage files for commit",
		"git_commit":        "Create a git commit",
		"git_branch":        "List git branches",
		"git_stash":         "Stash changes",
		"kubectl_get":       "Get Kubernetes resources",
		"kubectl_apply":     "Apply Kubernetes manifests",
		"kubectl_delete":    "Delete Kubernetes resources",
		"kubectl_describe":  "Describe Kubernetes resources",
		"kubectl_logs":      "Get Kubernetes pod logs",
		"kubectl_exec":      "Execute command in Kubernetes pod",
		"helm_list":         "List Helm releases",
		"helm_install":      "Install Helm chart",
		"terraform_init":    "Initialize Terraform",
		"terraform_plan":    "Create Terraform plan",
		"terraform_apply":   "Apply Terraform changes",
		"terraform_destroy": "Destroy Terraform resources",
		"terraform_output":  "Show Terraform outputs",
		"terraform_state":   "Manage Terraform state",
		"docker_build":      "Build Docker image",
		"docker_ps":         "List Docker containers",
		"docker_logs":       "Show Docker container logs",
		"docker_compose":    "Run docker-compose commands",
		"docker_exec":       "Execute command in Docker container",
		"aws_cli":           "Execute AWS CLI commands",
		"aws_ecs":           "Manage AWS ECS",
		"aws_eks":           "Manage AWS EKS",
		"az_cli":            "Execute Azure CLI commands",
		"aks":               "Manage Azure AKS",
		"gcloud":            "Execute Google Cloud CLI commands",
		"gke":               "Manage Google GKE",
		"trivy_scan":        "Scan for vulnerabilities with Trivy",
		"gitleaks_scan":     "Scan for secrets with Gitleaks",
		"secrets_scan":      "Scan for hardcoded secrets",
		"dependency_audit":  "Audit dependencies for vulnerabilities",
		"sast_scan":         "Static Application Security Testing",
		"tfsec_scan":        "Terraform security scanner",
		"kubesec_scan":      "Kubernetes security scanner",
	}

	tools := make([]ToolInfo, 0, len(r.tools))
	for name := range r.tools {
		desc := descriptions[name]
		if desc == "" {
			desc = "Tool: " + name
		}
		tools = append(tools, ToolInfo{Name: name, Description: desc})
	}
	return tools
}

// securityToolsWithSeverity maps security tools to their severity parameter name
// Only tools that support severity filtering are included
var securityToolsWithSeverity = map[string]string{
	"trivy_scan": "severity",         // --severity HIGH,CRITICAL
	"sast_scan":  "severity",         // --severity ERROR,WARNING
	"tfsec_scan": "minimum_severity", // --minimum-severity HIGH
}

// InjectSecurityDefaults injects default security configuration into tool parameters
// Returns the modified params map (may be the same map or a copy)
func InjectSecurityDefaults(toolName string, params map[string]interface{}, defaultSeverity string) map[string]interface{} {
	// Check if this is a security tool that supports severity
	severityParam, isSecurityTool := securityToolsWithSeverity[toolName]
	if !isSecurityTool || defaultSeverity == "" {
		return params
	}

	// Check if severity is already explicitly set
	if existingSeverity, ok := params[severityParam].(string); ok && existingSeverity != "" {
		return params
	}

	// Create a copy of params to avoid modifying the original
	newParams := make(map[string]interface{})
	for k, v := range params {
		newParams[k] = v
	}

	// Normalize severity for the specific tool
	normalizedSeverity := NormalizeSeverity(toolName, defaultSeverity)
	newParams[severityParam] = normalizedSeverity

	return newParams
}

// NormalizeSeverity converts severity levels to tool-specific format
func NormalizeSeverity(toolName string, severity string) string {
	// Normalize to uppercase and remove spaces
	severity = strings.ToUpper(strings.ReplaceAll(severity, " ", ""))

	switch toolName {
	case "sast_scan":
		// semgrep uses ERROR, WARNING, INFO instead of CRITICAL, HIGH, MEDIUM, LOW
		// Map common severity levels to semgrep equivalents
		replacements := map[string]string{
			"CRITICAL": "ERROR",
			"HIGH":     "ERROR",
			"MEDIUM":   "WARNING",
			"LOW":      "INFO",
			"UNKNOWN":  "INFO",
		}
		parts := strings.Split(severity, ",")
		mapped := make([]string, 0, len(parts))
		seen := make(map[string]bool)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if replacement, ok := replacements[part]; ok {
				if !seen[replacement] {
					mapped = append(mapped, replacement)
					seen[replacement] = true
				}
			} else if !seen[part] {
				mapped = append(mapped, part)
				seen[part] = true
			}
		}
		return strings.Join(mapped, ",")
	default:
		// trivy, tfsec use standard severity levels
		return severity
	}
}

// IsSecurityTool checks if a tool is a security scanning tool
func IsSecurityTool(name string) bool {
	securityTools := map[string]bool{
		"trivy_scan":       true,
		"gitleaks_scan":    true,
		"secrets_scan":     true,
		"dependency_audit": true,
		"sast_scan":        true,
		"tfsec_scan":       true,
		"kubesec_scan":     true,
	}
	return securityTools[name]
}
