package agent

import (
	"testing"
)

func TestAllTypes(t *testing.T) {
	types := AllTypes()

	expectedTypes := []Type{
		TypePlanner,
		TypeCoder,
		TypeTester,
		TypeReviewer,
		TypeDevOps,
		TypeSecurity,
		TypeDiagnostics,
	}

	if len(types) != len(expectedTypes) {
		t.Errorf("expected %d agent types, got %d", len(expectedTypes), len(types))
	}

	for _, expected := range expectedTypes {
		found := false
		for _, actual := range types {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find agent type %s", expected)
		}
	}
}

func TestTypeDisplayName(t *testing.T) {
	tests := []struct {
		agentType   Type
		displayName string
	}{
		{TypePlanner, "Planner"},
		{TypeCoder, "Coder"},
		{TypeTester, "Tester"},
		{TypeReviewer, "Reviewer"},
		{TypeDevOps, "DevOps"},
		{TypeSecurity, "Security"},
		{TypeDiagnostics, "Diagnostics"},
	}

	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			if tt.agentType.DisplayName() != tt.displayName {
				t.Errorf("expected display name %q for %s, got %q", tt.displayName, tt.agentType, tt.agentType.DisplayName())
			}
		})
	}
}

func TestTypeDescription(t *testing.T) {
	for _, agentType := range AllTypes() {
		t.Run(string(agentType), func(t *testing.T) {
			desc := agentType.Description()
			if desc == "" {
				t.Errorf("expected non-empty description for %s", agentType)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	for _, agentType := range AllTypes() {
		t.Run(string(agentType), func(t *testing.T) {
			cfg := DefaultConfig(agentType)

			if cfg.Model == "" {
				t.Errorf("expected non-empty model for %s", agentType)
			}

			if cfg.MaxContextTokens == 0 {
				t.Errorf("expected non-zero MaxContextTokens for %s", agentType)
			}

			if cfg.MaxToolIter == 0 {
				t.Errorf("expected non-zero MaxToolIter for %s", agentType)
			}

			if cfg.Timeout == 0 {
				t.Errorf("expected non-zero Timeout for %s", agentType)
			}
		})
	}
}

func TestStateZeroValues(t *testing.T) {
	state := State{}

	if state.Active {
		t.Error("expected Active to be false by default")
	}

	if state.Invocations != 0 {
		t.Errorf("expected Invocations to be 0, got %d", state.Invocations)
	}

	if state.TokensUsed != 0 {
		t.Errorf("expected TokensUsed to be 0, got %d", state.TokensUsed)
	}

	if state.ErrorCount != 0 {
		t.Errorf("expected ErrorCount to be 0, got %d", state.ErrorCount)
	}
}
