package orchestrator

import (
	"errors"
	"testing"
)

func TestDiagnoseFailure(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		params        map[string]interface{}
		err           error
		output        string
		expectedCause string
		expectRecover bool
	}{
		{
			name:          "kubectl connection refused",
			toolName:      "kubectl_get",
			params:        map[string]interface{}{"resource": "pods"},
			err:           errors.New("connection refused"),
			output:        "connection refused to server localhost:8080",
			expectedCause: "Cannot connect to Kubernetes cluster",
			expectRecover: true,
		},
		{
			name:          "kubectl not found",
			toolName:      "kubectl_describe",
			params:        map[string]interface{}{"resource": "pod", "name": "test-pod"},
			err:           errors.New("not found"),
			output:        "Error from server (NotFound): pods \"test-pod\" not found",
			expectedCause: "Resource does not exist in the cluster",
			expectRecover: true,
		},
		{
			name:          "terraform state lock",
			toolName:      "terraform_apply",
			params:        map[string]interface{}{},
			err:           errors.New("state locked"),
			output:        "Error acquiring the state lock",
			expectedCause: "Terraform state is locked by another process",
			expectRecover: true,
		},
		{
			name:          "docker daemon not running",
			toolName:      "docker_ps",
			params:        map[string]interface{}{},
			err:           errors.New("cannot connect"),
			output:        "Cannot connect to the Docker daemon",
			expectedCause: "Docker daemon is not running",
			expectRecover: true,
		},
		{
			name:          "docker image not found",
			toolName:      "docker_run",
			params:        map[string]interface{}{"image": "nonexistent:latest"},
			err:           errors.New("image not found"),
			output:        "no such image 'nonexistent:latest'",
			expectedCause: "Docker image not found",
			expectRecover: true,
		},
		{
			name:          "git not a repository",
			toolName:      "git_status",
			params:        map[string]interface{}{},
			err:           errors.New("not a git repository"),
			output:        "fatal: not a git repository",
			expectedCause: "Not in a git repository",
			expectRecover: true,
		},
		{
			name:          "git merge conflict",
			toolName:      "git_merge",
			params:        map[string]interface{}{"branch": "feature"},
			err:           errors.New("merge conflict"),
			output:        "CONFLICT (content): Merge conflict in file.txt",
			expectedCause: "Merge conflict detected",
			expectRecover: true, // Git conflicts are considered recoverable (manual resolution needed)
		},
		{
			name:          "file permission denied",
			toolName:      "write_file",
			params:        map[string]interface{}{"path": "/etc/passwd"},
			err:           errors.New("permission denied"),
			output:        "permission denied",
			expectedCause: "Permission denied accessing file",
			expectRecover: true,
		},
		{
			name:          "unknown tool generic error",
			toolName:      "unknown_tool",
			params:        map[string]interface{}{},
			err:           errors.New("some error"),
			output:        "generic error message",
			expectedCause: "Operation failed",
			expectRecover: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := DiagnoseFailure(tt.toolName, tt.params, tt.err, tt.output)

			if diag == nil {
				t.Fatal("expected non-nil diagnostics")
			}

			if diag.ToolName != tt.toolName {
				t.Errorf("expected ToolName %s, got %s", tt.toolName, diag.ToolName)
			}

			if diag.RootCause != tt.expectedCause {
				t.Errorf("expected RootCause %q, got %q", tt.expectedCause, diag.RootCause)
			}

			if diag.Recoverable != tt.expectRecover {
				t.Errorf("expected Recoverable %v, got %v", tt.expectRecover, diag.Recoverable)
			}

			if diag.Suggestion == "" {
				t.Error("expected non-empty Suggestion")
			}

			if diag.Severity == "" {
				t.Error("expected non-empty Severity")
			}
		})
	}
}

func TestDiagnoseFailureNilError(t *testing.T) {
	diag := DiagnoseFailure("test_tool", nil, nil, "")

	if diag == nil {
		t.Fatal("expected non-nil diagnostics even with nil error")
	}

	if diag.Error != "unknown error" {
		t.Errorf("expected Error to be 'unknown error', got %q", diag.Error)
	}

	if diag.RootCause == "" {
		t.Error("expected non-empty RootCause")
	}
}

func TestFailureDiagnosticsFields(t *testing.T) {
	diag := &FailureDiagnostics{
		ToolName:    "test_tool",
		RootCause:   "Test error",
		Suggestion:  "Try again",
		Severity:    "warning",
		Recoverable: true,
	}

	if diag.ToolName != "test_tool" {
		t.Errorf("unexpected ToolName: %s", diag.ToolName)
	}

	if diag.RootCause != "Test error" {
		t.Errorf("unexpected RootCause: %s", diag.RootCause)
	}

	if diag.Suggestion != "Try again" {
		t.Errorf("unexpected Suggestion: %s", diag.Suggestion)
	}

	if diag.Severity != "warning" {
		t.Errorf("unexpected Severity: %s", diag.Severity)
	}

	if !diag.Recoverable {
		t.Error("expected Recoverable to be true")
	}
}

func TestFailureDiagnosticsFormat(t *testing.T) {
	diag := &FailureDiagnostics{
		ToolName:    "kubectl_get",
		Params:      map[string]interface{}{"resource": "pods"},
		Error:       "connection refused",
		RootCause:   "Cannot connect to Kubernetes cluster",
		Suggestion:  "Check if the cluster is running",
		Severity:    "high",
		Recoverable: true,
	}

	// Test non-verbose format
	output := diag.Format(false)
	if output == "" {
		t.Error("expected non-empty formatted output")
	}

	if !contains(output, "Tool: kubectl_get") {
		t.Error("expected output to contain tool name")
	}

	if !contains(output, "Root Cause:") {
		t.Error("expected output to contain root cause")
	}

	// Test verbose format
	verboseOutput := diag.Format(true)
	if !contains(verboseOutput, "Params:") {
		t.Error("expected verbose output to contain params")
	}
}

func TestDiagnoseFailureKubectlErrors(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{"forbidden", "Error from server (Forbidden): pods is forbidden", "Insufficient permissions for this operation"},
		{"already exists", "Error from server (AlreadyExists): pods already exists", "Resource already exists"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := DiagnoseFailure("kubectl_apply", nil, errors.New("error"), tt.output)
			if diag.RootCause != tt.expected {
				t.Errorf("expected RootCause %q, got %q", tt.expected, diag.RootCause)
			}
		})
	}
}

func TestDiagnoseFailureDockerErrors(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{"port allocated", "Bind for 0.0.0.0:8080 failed: port is already allocated", "Port is already in use"},
		{"permission denied", "Got permission denied while trying to connect", "Permission denied accessing Docker"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := DiagnoseFailure("docker_run", nil, errors.New("error"), tt.output)
			if diag.RootCause != tt.expected {
				t.Errorf("expected RootCause %q, got %q", tt.expected, diag.RootCause)
			}
		})
	}
}

func TestDiagnoseFailureCommandErrors(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{"command not found", "bash: foo: command not found", "Command not found in PATH"},
		{"permission denied", "bash: ./script.sh: Permission denied", "Permission denied"},
		{"no such file", "bash: ./missing.sh: No such file or directory", "File or directory does not exist"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := DiagnoseFailure("execute_command", nil, errors.New("error"), tt.output)
			if diag.RootCause != tt.expected {
				t.Errorf("expected RootCause %q, got %q", tt.expected, diag.RootCause)
			}
		})
	}
}

func TestDiagnoseFailureGitErrors(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
	}{
		{"nothing to commit", "nothing to commit, working tree clean", "No changes to commit"},
		{"authentication failed", "fatal: Authentication failed for 'https://github.com/...'", "Git authentication failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag := DiagnoseFailure("git_commit", nil, errors.New("error"), tt.output)
			if diag.RootCause != tt.expected {
				t.Errorf("expected RootCause %q, got %q", tt.expected, diag.RootCause)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
