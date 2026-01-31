package watch

import (
	"testing"

	"github.com/tara-vision/taracode/internal/assistant"
)

func TestParseFindings(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected []Finding
	}{
		{
			name:     "no issues",
			response: "No issues detected.",
			expected: []Finding{},
		},
		{
			name:     "single error",
			response: "[ERROR] Connection refused on port 5432",
			expected: []Finding{{Type: FindingError, Description: "Connection refused on port 5432"}},
		},
		{
			name:     "single warning",
			response: "[WARNING] Deprecated API usage detected",
			expected: []Finding{{Type: FindingWarning, Description: "Deprecated API usage detected"}},
		},
		{
			name:     "single improvement",
			response: "[IMPROVEMENT] Consider using connection pooling",
			expected: []Finding{{Type: FindingImprovement, Description: "Consider using connection pooling"}},
		},
		{
			name: "multiple findings",
			response: `Here's what I found:
- [ERROR] Database connection failed
- [WARNING] High memory usage detected
- [IMPROVEMENT] Use async/await pattern`,
			expected: []Finding{
				{Type: FindingError, Description: "Database connection failed"},
				{Type: FindingWarning, Description: "High memory usage detected"},
				{Type: FindingImprovement, Description: "Use async/await pattern"},
			},
		},
		{
			name: "case insensitive",
			response: `[error] lowercase error
[WARNING] Mixed case warning
[Improvement] Title case improvement`,
			expected: []Finding{
				{Type: FindingError, Description: "lowercase error"},
				{Type: FindingWarning, Description: "Mixed case warning"},
				{Type: FindingImprovement, Description: "Title case improvement"},
			},
		},
		{
			name: "with leading dash and spaces",
			response: `  - [ERROR] Spaced error
-[WARNING] No space warning`,
			expected: []Finding{
				{Type: FindingError, Description: "Spaced error"},
				{Type: FindingWarning, Description: "No space warning"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFindings(tt.response)
			if len(result) != len(tt.expected) {
				t.Errorf("parseFindings() returned %d findings, want %d", len(result), len(tt.expected))
				return
			}
			for i, f := range result {
				if f.Type != tt.expected[i].Type {
					t.Errorf("Finding %d: Type = %s, want %s", i, f.Type, tt.expected[i].Type)
				}
				if f.Description != tt.expected[i].Description {
					t.Errorf("Finding %d: Description = %q, want %q", i, f.Description, tt.expected[i].Description)
				}
			}
		})
	}
}

func TestNewWatchMonitor(t *testing.T) {
	config := DefaultConfig()
	analyzeFunc := func(prompt string, images []*assistant.ImageData) (string, error) {
		return "No issues detected.", nil
	}

	monitor := NewWatchMonitor(config, analyzeFunc, "/tmp/test")

	if monitor == nil {
		t.Fatal("NewWatchMonitor returned nil")
	}

	if monitor.IsRunning() {
		t.Error("New monitor should not be running")
	}

	status := monitor.Status()
	if status.State != StateIdle {
		t.Errorf("New monitor state = %v, want %v", status.State, StateIdle)
	}
}

func TestMonitorStartStop(t *testing.T) {
	config := DefaultConfig()
	config.CaptureInterval = 100 * 1000000 // Very long to prevent actual captures during test

	analyzeFunc := func(prompt string, images []*assistant.ImageData) (string, error) {
		return "No issues detected.", nil
	}

	monitor := NewWatchMonitor(config, analyzeFunc, "/tmp/test")

	// Start
	err := monitor.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	if !monitor.IsRunning() {
		t.Error("Monitor should be running after Start()")
	}

	// Try to start again (should fail)
	err = monitor.Start()
	if err == nil {
		t.Error("Start() should fail when already running")
	}

	// Stop
	monitor.Stop()

	if monitor.IsRunning() {
		t.Error("Monitor should not be running after Stop()")
	}

	// Stop again (should be safe)
	monitor.Stop()
}
