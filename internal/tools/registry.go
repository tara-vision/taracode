package tools

import (
	"fmt"
)

type ToolExecutor func(params map[string]interface{}, workingDir string) (string, error)

type Registry struct {
	tools map[string]ToolExecutor
}

func NewRegistry() *Registry {
	r := &Registry{
		tools: make(map[string]ToolExecutor),
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

	return r
}

func (r *Registry) RegisterTool(name string, executor ToolExecutor) {
	r.tools[name] = executor
}

func (r *Registry) ExecuteTool(name string, params map[string]interface{}, workingDir string) (string, error) {
	executor, exists := r.tools[name]
	if !exists {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	return executor(params, workingDir)
}
