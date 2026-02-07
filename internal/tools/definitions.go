package tools

import (
	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// ToolDefinition contains metadata for a tool
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
	Category    string // file, git, web, kubernetes, terraform, docker, cloud, security
}

// GetToolDefinitions returns all tool definitions for OpenAI function calling
func GetToolDefinitions() []openai.Tool {
	definitions := []ToolDefinition{
		// =============================================================================
		// FILE OPERATIONS
		// =============================================================================
		{
			Name:        "read_file",
			Description: "Read the contents of a file at the specified path",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file, creating it if it doesn't exist or overwriting if it does. Use dry_run=true to preview changes without applying them.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to write",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview what would be written without actually writing",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
		{
			Name:        "append_file",
			Description: "Append content to the end of an existing file. Use dry_run=true to preview changes.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to append to",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to append",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview what would be appended without actually appending",
					},
				},
				"required": []string{"file_path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Description: "Edit a file by replacing a specific string with another string. Use dry_run=true to preview changes.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit",
					},
					"old_string": map[string]interface{}{
						"type":        "string",
						"description": "String to find and replace",
					},
					"new_string": map[string]interface{}{
						"type":        "string",
						"description": "Replacement string",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview the changes without applying them",
					},
				},
				"required": []string{"file_path", "old_string", "new_string"},
			},
		},
		{
			Name:        "insert_lines",
			Description: "Insert content at a specific line number in a file",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file",
					},
					"line_number": map[string]interface{}{
						"type":        "integer",
						"description": "Line number to insert at (1-indexed)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to insert",
					},
				},
				"required": []string{"file_path", "line_number", "content"},
			},
		},
		{
			Name:        "replace_lines",
			Description: "Replace a range of lines in a file with new content",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-indexed)",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Ending line number (inclusive)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "New content to replace the lines with",
					},
				},
				"required": []string{"file_path", "start_line", "end_line", "content"},
			},
		},
		{
			Name:        "delete_lines",
			Description: "Delete a range of lines from a file",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-indexed)",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Ending line number (inclusive)",
					},
				},
				"required": []string{"file_path", "start_line", "end_line"},
			},
		},
		{
			Name:        "copy_file",
			Description: "Copy a file from source path to destination path. Use dry_run=true to preview.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source_path": map[string]interface{}{
						"type":        "string",
						"description": "Source file path",
					},
					"dest_path": map[string]interface{}{
						"type":        "string",
						"description": "Destination file path",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview the copy without executing it",
					},
				},
				"required": []string{"source_path", "dest_path"},
			},
		},
		{
			Name:        "move_file",
			Description: "Move or rename a file. Use dry_run=true to preview.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source_path": map[string]interface{}{
						"type":        "string",
						"description": "Source file path",
					},
					"dest_path": map[string]interface{}{
						"type":        "string",
						"description": "Destination file path",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview the move without executing it",
					},
				},
				"required": []string{"source_path", "dest_path"},
			},
		},
		{
			Name:        "delete_file",
			Description: "Delete a file or directory. Use dry_run=true to preview.",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file or directory to delete",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, recursively delete directories",
					},
					"dry_run": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, preview what would be deleted without deleting",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "create_directory",
			Description: "Create a new directory, including parent directories if needed",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path of the directory to create",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files and directories in a directory",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "Directory path (default: current directory)",
					},
					"recursive": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, list files recursively",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "find_files",
			Description: "Find files matching a glob pattern",
			Category:    "file",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern (e.g., '*.go', '**/*.yaml')",
					},
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "Starting directory (default: current directory)",
					},
				},
				"required": []string{"pattern"},
			},
		},

		// =============================================================================
		// COMMAND EXECUTION
		// =============================================================================
		{
			Name:        "execute_command",
			Description: "Execute a shell command and return its output",
			Category:    "command",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Shell command to execute",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "search_files",
			Description: "Search for a pattern in files using grep",
			Category:    "command",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern (regex supported)",
					},
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "Directory to search in (default: current directory)",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "File pattern to limit search (e.g., '*.go')",
					},
				},
				"required": []string{"pattern"},
			},
		},

		// =============================================================================
		// GIT OPERATIONS
		// =============================================================================
		{
			Name:        "git_status",
			Description: "Show the working tree status (modified, staged, untracked files)",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			Name:        "git_diff",
			Description: "Show changes between commits, commit and working tree, etc.",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"staged": map[string]interface{}{
						"type":        "boolean",
						"description": "Show staged changes only",
					},
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Specific file to diff",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "git_log",
			Description: "Show commit history",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Number of commits to show (default: 10)",
					},
					"oneline": map[string]interface{}{
						"type":        "boolean",
						"description": "Show each commit on a single line",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "git_add",
			Description: "Stage files for commit",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"files": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Files to stage (use ['.'] for all)",
					},
				},
				"required": []string{"files"},
			},
		},
		{
			Name:        "git_commit",
			Description: "Create a commit with staged changes",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Commit message",
					},
				},
				"required": []string{"message"},
			},
		},
		{
			Name:        "git_branch",
			Description: "List, create, or delete branches",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Branch name to create (omit to list branches)",
					},
					"delete": map[string]interface{}{
						"type":        "boolean",
						"description": "Delete the specified branch",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "git_stash",
			Description: "Save, restore, or manage stashed changes",
			Category:    "git",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "Stash operation: save, pop, apply, drop, list, show, or clear",
						"enum":        []string{"save", "pop", "apply", "drop", "list", "show", "clear"},
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Message to describe the stash (for save operation)",
					},
					"stash_ref": map[string]interface{}{
						"type":        "string",
						"description": "Stash reference like stash@{0} (for pop, apply, drop, show)",
					},
					"include_untracked": map[string]interface{}{
						"type":        "boolean",
						"description": "Include untracked files in the stash (for save)",
					},
				},
				"required": []string{},
			},
		},

		// =============================================================================
		// WEB TOOLS
		// =============================================================================
		{
			Name:        "web_search",
			Description: "Search the web using DuckDuckGo and return results",
			Category:    "web",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"num_results": map[string]interface{}{
						"type":        "integer",
						"description": "Number of results to return (default: 5)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "web_fetch",
			Description: "Fetch and extract text content from a URL",
			Category:    "web",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to fetch",
					},
				},
				"required": []string{"url"},
			},
		},

		// =============================================================================
		// UTILITY TOOLS
		// =============================================================================
		{
			Name:        "get_datetime",
			Description: "MANDATORY: Call this tool for ANY question about date, time, day of week, or 'what day is today'. Returns accurate local time. You MUST call this tool - never guess the date from memory.",
			Category:    "utility",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g., 'America/New_York', 'Europe/London'). Default is local timezone.",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: 'full' (default), 'date', 'time', 'iso', or 'unix'",
					},
				},
				"required": []string{},
			},
		},

		// =============================================================================
		// KUBERNETES TOOLS
		// =============================================================================
		{
			Name:        "kubectl_get",
			Description: "Get Kubernetes resources (pods, services, deployments, etc.)",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"resource": map[string]interface{}{
						"type":        "string",
						"description": "Resource type (pods, services, deployments, etc.)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Specific resource name (optional)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace (default: default)",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Label selector (e.g., 'app=nginx')",
					},
					"output": map[string]interface{}{
						"type":        "string",
						"description": "Output format (yaml, json, wide)",
					},
				},
				"required": []string{"resource"},
			},
		},
		{
			Name:        "kubectl_apply",
			Description: "Apply a Kubernetes manifest file",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Path to the manifest file",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
				},
				"required": []string{"file"},
			},
		},
		{
			Name:        "kubectl_delete",
			Description: "Delete Kubernetes resources",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"resource": map[string]interface{}{
						"type":        "string",
						"description": "Resource type",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Manifest file to delete",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "kubectl_describe",
			Description: "Show detailed information about a Kubernetes resource",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"resource": map[string]interface{}{
						"type":        "string",
						"description": "Resource type",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Resource name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
				},
				"required": []string{"resource", "name"},
			},
		},
		{
			Name:        "kubectl_logs",
			Description: "Get logs from a pod",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pod": map[string]interface{}{
						"type":        "string",
						"description": "Pod name",
					},
					"container": map[string]interface{}{
						"type":        "string",
						"description": "Container name (if multiple containers)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to show from the end",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Follow log output",
					},
				},
				"required": []string{"pod"},
			},
		},
		{
			Name:        "kubectl_exec",
			Description: "Execute a command in a container",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pod": map[string]interface{}{
						"type":        "string",
						"description": "Pod name",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute",
					},
					"container": map[string]interface{}{
						"type":        "string",
						"description": "Container name (if multiple containers)",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
				},
				"required": []string{"pod", "command"},
			},
		},
		{
			Name:        "helm_list",
			Description: "List Helm releases",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace (default: all namespaces)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "helm_install",
			Description: "Install a Helm chart",
			Category:    "kubernetes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"release": map[string]interface{}{
						"type":        "string",
						"description": "Release name",
					},
					"chart": map[string]interface{}{
						"type":        "string",
						"description": "Chart name or path",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
					},
					"values": map[string]interface{}{
						"type":        "string",
						"description": "Path to values file",
					},
					"set": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Set values (key=value)",
					},
				},
				"required": []string{"release", "chart"},
			},
		},

		// =============================================================================
		// TERRAFORM TOOLS
		// =============================================================================
		{
			Name:        "terraform_init",
			Description: "Initialize a Terraform working directory",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"upgrade": map[string]interface{}{
						"type":        "boolean",
						"description": "Upgrade modules and plugins",
					},
					"backend_config": map[string]interface{}{
						"type":        "string",
						"description": "Backend configuration (key=value)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "terraform_plan",
			Description: "Generate and show an execution plan",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"out": map[string]interface{}{
						"type":        "string",
						"description": "Write plan to file",
					},
					"var_file": map[string]interface{}{
						"type":        "string",
						"description": "Variable file path",
					},
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Target specific resource",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "terraform_apply",
			Description: "Apply Terraform changes (DESTRUCTIVE - will modify infrastructure)",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_file": map[string]interface{}{
						"type":        "string",
						"description": "Plan file to apply",
					},
					"auto_approve": map[string]interface{}{
						"type":        "boolean",
						"description": "Skip interactive approval",
					},
					"var_file": map[string]interface{}{
						"type":        "string",
						"description": "Variable file path",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "terraform_destroy",
			Description: "Destroy Terraform-managed infrastructure (DESTRUCTIVE)",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Target specific resource to destroy",
					},
					"auto_approve": map[string]interface{}{
						"type":        "boolean",
						"description": "Skip interactive approval",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "terraform_output",
			Description: "Show Terraform output values",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Specific output name",
					},
					"json": map[string]interface{}{
						"type":        "boolean",
						"description": "Output in JSON format",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "terraform_state",
			Description: "Manage Terraform state",
			Category:    "terraform",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "State subcommand (list, show, mv, rm)",
					},
					"args": map[string]interface{}{
						"type":        "string",
						"description": "Additional arguments",
					},
				},
				"required": []string{"subcommand"},
			},
		},

		// =============================================================================
		// DOCKER TOOLS
		// =============================================================================
		{
			Name:        "docker_build",
			Description: "Build a Docker image from a Dockerfile",
			Category:    "docker",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Image tag (name:tag)",
					},
					"dockerfile": map[string]interface{}{
						"type":        "string",
						"description": "Dockerfile path (default: Dockerfile)",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Build context (default: current directory)",
					},
					"build_args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Build arguments (KEY=value)",
					},
				},
				"required": []string{"tag"},
			},
		},
		{
			Name:        "docker_ps",
			Description: "List Docker containers",
			Category:    "docker",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"all": map[string]interface{}{
						"type":        "boolean",
						"description": "Show all containers (including stopped)",
					},
					"filter": map[string]interface{}{
						"type":        "string",
						"description": "Filter by condition",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "docker_logs",
			Description: "Get logs from a Docker container",
			Category:    "docker",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{
						"type":        "string",
						"description": "Container name or ID",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to show from end",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Follow log output",
					},
				},
				"required": []string{"container"},
			},
		},
		{
			Name:        "docker_compose",
			Description: "Run Docker Compose commands",
			Category:    "docker",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "Compose subcommand (up, down, ps, logs, etc.)",
					},
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Compose file path",
					},
					"detach": map[string]interface{}{
						"type":        "boolean",
						"description": "Run containers in background",
					},
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific services to target",
					},
				},
				"required": []string{"subcommand"},
			},
		},
		{
			Name:        "docker_exec",
			Description: "Execute a command in a running container",
			Category:    "docker",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{
						"type":        "string",
						"description": "Container name or ID",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to execute",
					},
					"interactive": map[string]interface{}{
						"type":        "boolean",
						"description": "Keep STDIN open",
					},
				},
				"required": []string{"container", "command"},
			},
		},

		// =============================================================================
		// CLOUD TOOLS - AWS
		// =============================================================================
		{
			Name:        "aws_cli",
			Description: "Run AWS CLI commands",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"service": map[string]interface{}{
						"type":        "string",
						"description": "AWS service (s3, ec2, iam, etc.)",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Service command",
					},
					"args": map[string]interface{}{
						"type":        "string",
						"description": "Additional arguments",
					},
					"region": map[string]interface{}{
						"type":        "string",
						"description": "AWS region",
					},
				},
				"required": []string{"service", "command"},
			},
		},
		{
			Name:        "aws_ecs",
			Description: "Manage AWS ECS clusters and services",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "ECS subcommand (list-clusters, describe-services, etc.)",
					},
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Cluster name or ARN",
					},
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
				},
				"required": []string{"subcommand"},
			},
		},
		{
			Name:        "aws_eks",
			Description: "Manage AWS EKS clusters",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "EKS subcommand (list-clusters, describe-cluster, etc.)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cluster name",
					},
				},
				"required": []string{"subcommand"},
			},
		},

		// =============================================================================
		// CLOUD TOOLS - AZURE
		// =============================================================================
		{
			Name:        "az_cli",
			Description: "Run Azure CLI commands",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"group": map[string]interface{}{
						"type":        "string",
						"description": "Azure command group (vm, storage, etc.)",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to run",
					},
					"args": map[string]interface{}{
						"type":        "string",
						"description": "Additional arguments",
					},
				},
				"required": []string{"group", "command"},
			},
		},
		{
			Name:        "az_aks",
			Description: "Manage Azure Kubernetes Service",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "AKS subcommand (list, show, get-credentials, etc.)",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cluster name",
					},
					"resource_group": map[string]interface{}{
						"type":        "string",
						"description": "Resource group name",
					},
				},
				"required": []string{"subcommand"},
			},
		},

		// =============================================================================
		// CLOUD TOOLS - GCP
		// =============================================================================
		{
			Name:        "gcloud",
			Description: "Run Google Cloud CLI commands",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"component": map[string]interface{}{
						"type":        "string",
						"description": "GCloud component (compute, storage, etc.)",
					},
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Command to run",
					},
					"args": map[string]interface{}{
						"type":        "string",
						"description": "Additional arguments",
					},
					"project": map[string]interface{}{
						"type":        "string",
						"description": "GCP project ID",
					},
				},
				"required": []string{"component", "command"},
			},
		},
		{
			Name:        "gke",
			Description: "Manage Google Kubernetes Engine",
			Category:    "cloud",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subcommand": map[string]interface{}{
						"type":        "string",
						"description": "GKE subcommand (clusters list, clusters describe, etc.)",
					},
					"zone": map[string]interface{}{
						"type":        "string",
						"description": "GCP zone or region",
					},
					"cluster": map[string]interface{}{
						"type":        "string",
						"description": "Cluster name",
					},
				},
				"required": []string{"subcommand"},
			},
		},

		// =============================================================================
		// SECURITY TOOLS
		// =============================================================================
		{
			Name:        "trivy_scan",
			Description: "Scan container images, filesystems, or configs for vulnerabilities using Trivy",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Scan target: image name:tag, '.' for filesystem, or path",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Scan type: image (default), fs, config, repo",
						"enum":        []string{"image", "fs", "config", "repo"},
					},
					"severity": map[string]interface{}{
						"type":        "string",
						"description": "Severity levels to include (UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: table (default), json, sarif",
					},
					"ignore_unfixed": map[string]interface{}{
						"type":        "boolean",
						"description": "Ignore vulnerabilities without fixes",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gitleaks_scan",
			Description: "Scan git repository for hardcoded secrets, API keys, and credentials",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to scan (default: current directory)",
					},
					"verbose": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable verbose output",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Report format: json, csv, sarif",
					},
					"baseline": map[string]interface{}{
						"type":        "string",
						"description": "Baseline file to ignore known secrets",
					},
					"no_git": map[string]interface{}{
						"type":        "boolean",
						"description": "Scan files without git history",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "secrets_scan",
			Description: "Quick pattern-based search for hardcoded secrets in code",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to scan (default: current directory)",
					},
					"patterns": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Custom patterns to search for (default: password, api_key, token, secret)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "dependency_audit",
			Description: "Check package dependencies for known vulnerabilities",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type":        "string",
						"description": "Package manager type",
						"enum":        []string{"npm", "pip", "go", "cargo", "composer"},
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Project directory (default: current directory)",
					},
					"json": map[string]interface{}{
						"type":        "boolean",
						"description": "Output in JSON format (npm only)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (pip-audit only)",
					},
				},
				"required": []string{"type"},
			},
		},
		{
			Name:        "sast_scan",
			Description: "Run static application security testing (SAST) using Semgrep",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to scan (default: current directory)",
					},
					"config": map[string]interface{}{
						"type":        "string",
						"description": "Ruleset: auto (default), p/security-audit, p/owasp-top-ten",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: text, json, sarif",
					},
					"severity": map[string]interface{}{
						"type":        "string",
						"description": "Minimum severity: INFO, WARNING, ERROR",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "tfsec_scan",
			Description: "Scan Terraform code for security misconfigurations",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory with Terraform files (default: current directory)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: default, json, csv, sarif",
					},
					"minimum_severity": map[string]interface{}{
						"type":        "string",
						"description": "Minimum severity: CRITICAL, HIGH, MEDIUM, LOW",
					},
					"exclude": map[string]interface{}{
						"type":        "string",
						"description": "Check IDs to exclude (comma-separated)",
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "kubesec_scan",
			Description: "Analyze Kubernetes manifests for security risks",
			Category:    "security",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Path to Kubernetes manifest YAML",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format: json, template",
					},
				},
				"required": []string{"file"},
			},
		},
	}

	// Convert to OpenAI tools format using proper jsonschema.Definition
	tools := make([]openai.Tool, len(definitions))
	for i, def := range definitions {
		// Convert map to jsonschema.Definition for proper serialization
		schemaDef := convertToSchema(def.Parameters)
		tools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  schemaDef,
			},
		}
	}

	return tools
}

// convertToSchema converts a map[string]interface{} to jsonschema.Definition
func convertToSchema(params map[string]interface{}) *jsonschema.Definition {
	def := &jsonschema.Definition{
		Type: jsonschema.Object,
	}

	// Extract properties
	if props, ok := params["properties"].(map[string]interface{}); ok {
		def.Properties = make(map[string]jsonschema.Definition)
		for name, propVal := range props {
			if propMap, ok := propVal.(map[string]interface{}); ok {
				propDef := jsonschema.Definition{}

				// Set type
				if typeStr, ok := propMap["type"].(string); ok {
					switch typeStr {
					case "string":
						propDef.Type = jsonschema.String
					case "integer":
						propDef.Type = jsonschema.Integer
					case "number":
						propDef.Type = jsonschema.Number
					case "boolean":
						propDef.Type = jsonschema.Boolean
					case "array":
						propDef.Type = jsonschema.Array
						// Handle array items
						if items, ok := propMap["items"].(map[string]interface{}); ok {
							if itemType, ok := items["type"].(string); ok && itemType == "string" {
								propDef.Items = &jsonschema.Definition{Type: jsonschema.String}
							}
						}
					case "object":
						propDef.Type = jsonschema.Object
					}
				}

				// Set description
				if desc, ok := propMap["description"].(string); ok {
					propDef.Description = desc
				}

				// Set enum
				if enum, ok := propMap["enum"].([]string); ok {
					propDef.Enum = enum
				}

				def.Properties[name] = propDef
			}
		}
	}

	// Extract required fields
	if required, ok := params["required"].([]string); ok {
		def.Required = required
	}

	return def
}

// GetToolDefinitionsByCategory returns tools filtered by category
func GetToolDefinitionsByCategory(category string) []openai.Tool {
	allTools := GetToolDefinitions()
	definitions := getDefinitionsMap()

	var filtered []openai.Tool
	for _, tool := range allTools {
		if def, ok := definitions[tool.Function.Name]; ok && def.Category == category {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// getDefinitionsMap returns a map of tool name to definition for category lookups
func getDefinitionsMap() map[string]ToolDefinition {
	return map[string]ToolDefinition{
		"read_file":         {Category: "file"},
		"write_file":        {Category: "file"},
		"append_file":       {Category: "file"},
		"edit_file":         {Category: "file"},
		"insert_lines":      {Category: "file"},
		"replace_lines":     {Category: "file"},
		"delete_lines":      {Category: "file"},
		"copy_file":         {Category: "file"},
		"move_file":         {Category: "file"},
		"delete_file":       {Category: "file"},
		"create_directory":  {Category: "file"},
		"list_files":        {Category: "file"},
		"find_files":        {Category: "file"},
		"execute_command":   {Category: "command"},
		"search_files":      {Category: "command"},
		"git_status":        {Category: "git"},
		"git_diff":          {Category: "git"},
		"git_log":           {Category: "git"},
		"git_add":           {Category: "git"},
		"git_commit":        {Category: "git"},
		"git_branch":        {Category: "git"},
		"git_stash":         {Category: "git"},
		"web_search":        {Category: "web"},
		"web_fetch":         {Category: "web"},
		"get_datetime":      {Category: "utility"},
		"datetime":          {Category: "utility"},
		"kubectl_get":       {Category: "kubernetes"},
		"kubectl_apply":     {Category: "kubernetes"},
		"kubectl_delete":    {Category: "kubernetes"},
		"kubectl_describe":  {Category: "kubernetes"},
		"kubectl_logs":      {Category: "kubernetes"},
		"kubectl_exec":      {Category: "kubernetes"},
		"helm_list":         {Category: "kubernetes"},
		"helm_install":      {Category: "kubernetes"},
		"terraform_init":    {Category: "terraform"},
		"terraform_plan":    {Category: "terraform"},
		"terraform_apply":   {Category: "terraform"},
		"terraform_destroy": {Category: "terraform"},
		"terraform_output":  {Category: "terraform"},
		"terraform_state":   {Category: "terraform"},
		"docker_build":      {Category: "docker"},
		"docker_ps":         {Category: "docker"},
		"docker_logs":       {Category: "docker"},
		"docker_compose":    {Category: "docker"},
		"docker_exec":       {Category: "docker"},
		"aws_cli":           {Category: "cloud"},
		"aws_ecs":           {Category: "cloud"},
		"aws_eks":           {Category: "cloud"},
		"az_cli":            {Category: "cloud"},
		"az_aks":            {Category: "cloud"},
		"gcloud":            {Category: "cloud"},
		"gke":               {Category: "cloud"},
		"trivy_scan":        {Category: "security"},
		"gitleaks_scan":     {Category: "security"},
		"secrets_scan":      {Category: "security"},
		"dependency_audit":  {Category: "security"},
		"sast_scan":         {Category: "security"},
		"tfsec_scan":        {Category: "security"},
		"kubesec_scan":      {Category: "security"},
	}
}

// ListToolsByCategory returns tool names grouped by category
func ListToolsByCategory() map[string][]string {
	defMap := getDefinitionsMap()
	result := make(map[string][]string)

	for name, def := range defMap {
		result[def.Category] = append(result[def.Category], name)
	}

	return result
}

// ToolInfo contains display information for a tool
type ToolInfo struct {
	Name        string
	Description string
	Category    string
}

// GetToolInfoList returns a list of all tools with their descriptions for display
func GetToolInfoList() []ToolInfo {
	// Build map of tool definitions
	definitions := []ToolDefinition{
		// File operations
		{Name: "read_file", Description: "Read the contents of a file", Category: "file"},
		{Name: "write_file", Description: "Write content to a file", Category: "file"},
		{Name: "append_file", Description: "Append content to a file", Category: "file"},
		{Name: "edit_file", Description: "Edit a file by replacing text", Category: "file"},
		{Name: "insert_lines", Description: "Insert content at a line number", Category: "file"},
		{Name: "replace_lines", Description: "Replace a range of lines", Category: "file"},
		{Name: "delete_lines", Description: "Delete a range of lines", Category: "file"},
		{Name: "copy_file", Description: "Copy a file", Category: "file"},
		{Name: "move_file", Description: "Move or rename a file", Category: "file"},
		{Name: "delete_file", Description: "Delete a file or directory", Category: "file"},
		{Name: "create_directory", Description: "Create a directory", Category: "file"},
		{Name: "list_files", Description: "List files in a directory", Category: "file"},
		{Name: "find_files", Description: "Find files by glob pattern", Category: "file"},
		// Command
		{Name: "execute_command", Description: "Execute a shell command", Category: "command"},
		{Name: "search_files", Description: "Search for pattern in files (grep)", Category: "command"},
		// Git
		{Name: "git_status", Description: "Show working tree status", Category: "git"},
		{Name: "git_diff", Description: "Show changes between commits", Category: "git"},
		{Name: "git_log", Description: "Show commit history", Category: "git"},
		{Name: "git_add", Description: "Stage files for commit", Category: "git"},
		{Name: "git_commit", Description: "Create a commit", Category: "git"},
		{Name: "git_branch", Description: "List/create/delete branches", Category: "git"},
		{Name: "git_stash", Description: "Save/restore stashed changes", Category: "git"},
		// Web
		{Name: "web_search", Description: "Search the web (DuckDuckGo)", Category: "web"},
		{Name: "web_fetch", Description: "Fetch content from a URL", Category: "web"},
		// Utility
		{Name: "get_datetime", Description: "Get current date and time", Category: "utility"},
		// Kubernetes
		{Name: "kubectl_get", Description: "Get Kubernetes resources", Category: "kubernetes"},
		{Name: "kubectl_apply", Description: "Apply a manifest file", Category: "kubernetes"},
		{Name: "kubectl_delete", Description: "Delete resources", Category: "kubernetes"},
		{Name: "kubectl_describe", Description: "Describe a resource", Category: "kubernetes"},
		{Name: "kubectl_logs", Description: "Get pod logs", Category: "kubernetes"},
		{Name: "kubectl_exec", Description: "Execute command in container", Category: "kubernetes"},
		{Name: "helm_list", Description: "List Helm releases", Category: "kubernetes"},
		{Name: "helm_install", Description: "Install a Helm chart", Category: "kubernetes"},
		// Terraform
		{Name: "terraform_init", Description: "Initialize Terraform", Category: "terraform"},
		{Name: "terraform_plan", Description: "Generate execution plan", Category: "terraform"},
		{Name: "terraform_apply", Description: "Apply changes", Category: "terraform"},
		{Name: "terraform_destroy", Description: "Destroy infrastructure", Category: "terraform"},
		{Name: "terraform_output", Description: "Show output values", Category: "terraform"},
		{Name: "terraform_state", Description: "Manage state", Category: "terraform"},
		// Docker
		{Name: "docker_build", Description: "Build Docker image", Category: "docker"},
		{Name: "docker_ps", Description: "List containers", Category: "docker"},
		{Name: "docker_logs", Description: "Get container logs", Category: "docker"},
		{Name: "docker_compose", Description: "Run Compose commands", Category: "docker"},
		{Name: "docker_exec", Description: "Execute in container", Category: "docker"},
		// Cloud
		{Name: "aws_cli", Description: "Run AWS CLI commands", Category: "cloud"},
		{Name: "aws_ecs", Description: "Manage AWS ECS", Category: "cloud"},
		{Name: "aws_eks", Description: "Manage AWS EKS", Category: "cloud"},
		{Name: "az_cli", Description: "Run Azure CLI commands", Category: "cloud"},
		{Name: "az_aks", Description: "Manage Azure AKS", Category: "cloud"},
		{Name: "gcloud", Description: "Run Google Cloud CLI", Category: "cloud"},
		{Name: "gke", Description: "Manage Google GKE", Category: "cloud"},
		// Security
		{Name: "trivy_scan", Description: "Scan for vulnerabilities", Category: "security"},
		{Name: "gitleaks_scan", Description: "Scan for secrets in git", Category: "security"},
		{Name: "secrets_scan", Description: "Pattern-based secrets search", Category: "security"},
		{Name: "dependency_audit", Description: "Check dependencies for vulns", Category: "security"},
		{Name: "sast_scan", Description: "Static security analysis", Category: "security"},
		{Name: "tfsec_scan", Description: "Scan Terraform for issues", Category: "security"},
		{Name: "kubesec_scan", Description: "Analyze K8s manifests", Category: "security"},
	}

	result := make([]ToolInfo, len(definitions))
	for i, def := range definitions {
		result[i] = ToolInfo{
			Name:        def.Name,
			Description: def.Description,
			Category:    def.Category,
		}
	}
	return result
}

// GetToolCount returns the total number of available tools
func GetToolCount() int {
	return len(GetToolInfoList())
}
