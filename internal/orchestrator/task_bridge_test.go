package orchestrator

import (
	"testing"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/tools"
)

func TestNewTaskBridge(t *testing.T) {
	toolReg := tools.NewRegistry()

	bridge := NewTaskBridge(nil, toolReg, nil, "/tmp")

	if bridge == nil {
		t.Fatal("expected non-nil TaskBridge")
	}

	if bridge.IsInitialized() {
		t.Error("expected bridge to not be initialized initially")
	}
}

func TestTaskBridgeGetAgentInfosBeforeInit(t *testing.T) {
	toolReg := tools.NewRegistry()
	bridge := NewTaskBridge(nil, toolReg, nil, "/tmp")

	infos := bridge.GetAgentInfos()

	// Should return default agent info even before initialization
	if len(infos) == 0 {
		t.Error("expected at least one agent info")
	}

	// Check that all agent types are represented
	expectedTypes := agent.AllTypes()
	if len(infos) != len(expectedTypes) {
		t.Errorf("expected %d agent infos, got %d", len(expectedTypes), len(infos))
	}

	for _, info := range infos {
		if info.DisplayName == "" {
			t.Error("expected non-empty DisplayName")
		}
		if info.Description == "" {
			t.Error("expected non-empty Description")
		}
		if info.Model == "" {
			t.Error("expected non-empty Model")
		}
	}
}

func TestTaskBridgeRoutePromptBeforeInit(t *testing.T) {
	toolReg := tools.NewRegistry()
	bridge := NewTaskBridge(nil, toolReg, nil, "/tmp")

	tests := []struct {
		prompt   string
		expected agent.Type
	}{
		{"plan the implementation", agent.TypePlanner},
		{"write some code", agent.TypeCoder},
		{"run the tests", agent.TypeTester},
		{"review this PR", agent.TypeReviewer},
		{"deploy to kubernetes", agent.TypeDevOps},
		{"scan for security vulnerabilities", agent.TypeSecurity},
		{"debug this error", agent.TypeDiagnostics},
	}

	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			agentType, info := bridge.RoutePrompt(tt.prompt)

			if agentType != tt.expected {
				t.Errorf("expected %s for prompt %q, got %s", tt.expected, tt.prompt, agentType)
			}

			if info.MatchedAgent != tt.expected {
				t.Errorf("expected RoutingInfo.MatchedAgent %s, got %s", tt.expected, info.MatchedAgent)
			}
		})
	}
}

func TestTaskBridgeGetStatusBeforeInit(t *testing.T) {
	toolReg := tools.NewRegistry()
	bridge := NewTaskBridge(nil, toolReg, nil, "/tmp")

	status := bridge.GetStatus()

	if len(status.Agents) == 0 {
		t.Error("expected at least one agent in status")
	}
}

func TestTaskBridgeSetWorkingDir(t *testing.T) {
	toolReg := tools.NewRegistry()
	bridge := NewTaskBridge(nil, toolReg, nil, "/original")

	bridge.SetWorkingDir("/new/path")

	// The working dir change should be reflected internally
	// We can't directly access it, but we can verify the bridge still works
	if bridge == nil {
		t.Error("bridge should not be nil after SetWorkingDir")
	}
}

func TestTaskBridgeGetTaskStateBeforeInit(t *testing.T) {
	toolReg := tools.NewRegistry()
	bridge := NewTaskBridge(nil, toolReg, nil, "/tmp")

	state := bridge.GetTaskState()

	// Should be nil when orchestrator isn't initialized
	if state != nil {
		t.Error("expected nil TaskState before initialization")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("expected non-empty ID")
	}

	// IDs should be unique (at least different)
	// Note: This test may occasionally fail if IDs are generated in the same nanosecond
	// but that's extremely rare
	if id1 == id2 {
		t.Error("expected different IDs for consecutive calls")
	}
}

func TestAgentStatusFields(t *testing.T) {
	status := AgentStatus{
		Agents:      []agent.AgentInfo{},
		ActiveTask:  "test-task",
		ActiveAgent: agent.TypeCoder,
		TaskState:   nil,
	}

	if status.ActiveTask != "test-task" {
		t.Errorf("unexpected ActiveTask: %s", status.ActiveTask)
	}

	if status.ActiveAgent != agent.TypeCoder {
		t.Errorf("unexpected ActiveAgent: %s", status.ActiveAgent)
	}
}
