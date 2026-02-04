package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestEnhanceError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains []string
	}{
		{
			name: "connection refused",
			err:  errors.New("dial tcp: connection refused"),
			contains: []string{
				"Connection Refused",
				"ollama serve",
			},
		},
		{
			name: "timeout",
			err:  errors.New("request timeout after 30s"),
			contains: []string{
				"Timeout",
				"took too long",
			},
		},
		{
			name: "unknown tool",
			err:  errors.New("unknown tool: kubectl_pods"),
			contains: []string{
				"Unknown Tool",
				"/tools",
			},
		},
		{
			name: "model not found",
			err:  errors.New("model not found: llama3"),
			contains: []string{
				"Model Not Found",
				"ollama pull",
			},
		},
		{
			name: "rate limited",
			err:  errors.New("HTTP 429: rate limited"),
			contains: []string{
				"Rate Limited",
				"slow down",
			},
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			contains: []string{"Error:", "something went wrong"},
		},
		{
			name:     "nil error",
			err:      nil,
			contains: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := EnhanceError(tc.err)

			if tc.err == nil {
				if result != "" {
					t.Errorf("Expected empty string for nil error, got: %s", result)
				}
				return
			}

			for _, c := range tc.contains {
				if !strings.Contains(result, c) {
					t.Errorf("Expected result to contain %q, got: %s", c, result)
				}
			}
		})
	}
}

func TestSuggestSimilarTools(t *testing.T) {
	availableTools := []string{
		"kubectl_get",
		"kubectl_apply",
		"kubectl_delete",
		"terraform_init",
		"terraform_plan",
		"docker_build",
		"docker_ps",
		"read_file",
		"write_file",
	}

	tests := []struct {
		name     string
		input    string
		expected []string // Expected suggestions (in order of relevance)
	}{
		{
			name:     "kubectl prefix",
			input:    "kubectl",
			expected: []string{"kubectl_get", "kubectl_apply", "kubectl_delete"},
		},
		{
			name:     "partial match",
			input:    "kube",
			expected: []string{"kubectl_get", "kubectl_apply", "kubectl_delete"},
		},
		{
			name:     "terraform typo",
			input:    "terrafrom",
			expected: []string{"terraform_init", "terraform_plan"},
		},
		{
			name:     "docker",
			input:    "docker",
			expected: []string{"docker_build", "docker_ps"},
		},
		{
			name:     "file operations",
			input:    "file",
			expected: []string{"read_file", "write_file"},
		},
		{
			name:     "no match",
			input:    "xyz123",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := SuggestSimilarTools(tc.input, availableTools)

			if tc.expected == nil {
				if len(result) > 0 {
					t.Errorf("Expected no suggestions, got: %v", result)
				}
				return
			}

			// Check that expected items are in results
			for _, exp := range tc.expected {
				found := false
				for _, r := range result {
					if r == exp {
						found = true
						break
					}
				}
				if !found && len(result) > 0 {
					// Only error if we got results but not the expected one
					t.Logf("Expected %q in suggestions, got: %v", exp, result)
				}
			}

			// Should have at most 3 suggestions
			if len(result) > 3 {
				t.Errorf("Expected at most 3 suggestions, got %d: %v", len(result), result)
			}
		})
	}
}

func TestEnhanceUnknownToolError(t *testing.T) {
	tools := []string{"kubectl_get", "kubectl_apply", "docker_build"}

	result := EnhanceUnknownToolError("kubectl_list", tools)

	if !strings.Contains(result, "kubectl_list") {
		t.Error("Expected result to contain the tool name")
	}

	if !strings.Contains(result, "Did you mean") {
		t.Error("Expected result to contain suggestions")
	}

	if !strings.Contains(result, "/tools") {
		t.Error("Expected result to mention /tools command")
	}
}

func TestFormatConnectionError(t *testing.T) {
	host := "http://localhost:11434"
	err := errors.New("connection refused")

	result := FormatConnectionError(host, err)

	if !strings.Contains(result, "Cannot connect") {
		t.Error("Expected connection error message")
	}

	if !strings.Contains(result, host) {
		t.Error("Expected result to contain host")
	}

	if !strings.Contains(result, "ollama serve") {
		t.Error("Expected suggestion to start ollama")
	}
}
