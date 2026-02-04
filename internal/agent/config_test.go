package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultAgentsConfig(t *testing.T) {
	cfg := DefaultAgentsConfig()

	if !cfg.Enabled {
		t.Error("expected Enabled to be true by default")
	}

	if cfg.DefaultRouting != "auto" {
		t.Errorf("expected DefaultRouting to be 'auto', got %s", cfg.DefaultRouting)
	}

	if cfg.FallbackModel != "gemma3:27b" {
		t.Errorf("expected FallbackModel to be 'gemma3:27b', got %s", cfg.FallbackModel)
	}

	if cfg.TimeoutMultiplier != 1.0 {
		t.Errorf("expected TimeoutMultiplier to be 1.0, got %f", cfg.TimeoutMultiplier)
	}
}

func TestGetAgentConfig(t *testing.T) {
	cfg := DefaultAgentsConfig()

	// These expected temperatures match DefaultConfig() in types.go
	tests := []struct {
		agentType    Type
		expectedTemp float32
	}{
		{TypePlanner, 0.3},
		{TypeCoder, 0.4}, // Coder uses higher temperature for creative code generation
		{TypeTester, 0.2},
		{TypeReviewer, 0.5}, // Reviewer uses higher temperature for varied feedback
		{TypeDevOps, 0.3},   // DevOps temperature for infrastructure tasks
		{TypeSecurity, 0.2}, // Security uses low temperature for precise analysis
		{TypeDiagnostics, 0.2},
	}

	for _, tt := range tests {
		t.Run(string(tt.agentType), func(t *testing.T) {
			agentCfg := cfg.GetAgentConfig(tt.agentType)

			if agentCfg.Temperature != tt.expectedTemp {
				t.Errorf("expected temperature %f for %s, got %f", tt.expectedTemp, tt.agentType, agentCfg.Temperature)
			}

			if agentCfg.Model == "" {
				t.Errorf("expected non-empty model for %s", tt.agentType)
			}

			if agentCfg.MaxContextTokens == 0 {
				t.Errorf("expected non-zero MaxContextTokens for %s", tt.agentType)
			}
		})
	}
}

func TestLoadAgentsConfigNoProject(t *testing.T) {
	// Load config with empty project dir (should use defaults)
	cfg := LoadAgentsConfig("")

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
}

func TestLoadAgentsConfigWithProjectOverride(t *testing.T) {
	// Create a temporary directory with agents.yaml
	tmpDir, err := os.MkdirTemp("", "taracode-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .taracode directory
	taracodeDir := filepath.Join(tmpDir, ".taracode")
	if err := os.MkdirAll(taracodeDir, 0755); err != nil {
		t.Fatalf("failed to create .taracode dir: %v", err)
	}

	// Write a test agents.yaml
	agentsYAML := `
enabled: true
default_routing: manual
timeout_multiplier: 2.0

planner:
  model: "custom-model"
  temperature: 0.5
`
	if err := os.WriteFile(filepath.Join(taracodeDir, "agents.yaml"), []byte(agentsYAML), 0644); err != nil {
		t.Fatalf("failed to write agents.yaml: %v", err)
	}

	cfg := LoadAgentsConfig(tmpDir)

	if cfg.DefaultRouting != "manual" {
		t.Errorf("expected DefaultRouting to be 'manual', got %s", cfg.DefaultRouting)
	}

	if cfg.TimeoutMultiplier != 2.0 {
		t.Errorf("expected TimeoutMultiplier to be 2.0, got %f", cfg.TimeoutMultiplier)
	}

	// Check that planner config was overridden
	plannerCfg := cfg.GetAgentConfig(TypePlanner)
	if plannerCfg.Model != "custom-model" {
		t.Errorf("expected planner model to be 'custom-model', got %s", plannerCfg.Model)
	}

	if plannerCfg.Temperature != 0.5 {
		t.Errorf("expected planner temperature to be 0.5, got %f", plannerCfg.Temperature)
	}
}

func TestGenerateExampleConfig(t *testing.T) {
	config := GenerateExampleConfig()

	if config == "" {
		t.Error("expected non-empty example config")
	}

	// Check that it contains expected sections
	expectedSections := []string{
		"enabled:",
		"default_routing:",
		"planner:",
		"coder:",
		"tester:",
		"reviewer:",
		"devops:",
		"security:",
		"diagnostics:",
	}

	for _, section := range expectedSections {
		if !contains(config, section) {
			t.Errorf("expected example config to contain %q", section)
		}
	}
}

func TestMergeAgentsConfig(t *testing.T) {
	base := DefaultAgentsConfig()
	override := AgentsConfig{
		DefaultRouting:    "manual",
		TimeoutMultiplier: 3.0,
		Planner: &AgentConfigYAML{
			Model:       "override-model",
			Temperature: 0.9,
		},
	}

	merged := mergeAgentsConfig(base, override)

	// Override values should take precedence
	if merged.DefaultRouting != "manual" {
		t.Errorf("expected merged DefaultRouting to be 'manual', got %s", merged.DefaultRouting)
	}

	if merged.TimeoutMultiplier != 3.0 {
		t.Errorf("expected merged TimeoutMultiplier to be 3.0, got %f", merged.TimeoutMultiplier)
	}

	// Planner should be overridden
	if merged.Planner == nil {
		t.Fatal("expected merged Planner to not be nil")
	}

	if merged.Planner.Model != "override-model" {
		t.Errorf("expected merged Planner.Model to be 'override-model', got %s", merged.Planner.Model)
	}

	// Other agents should retain base values
	if merged.Coder != nil && merged.Coder.Model != "" && merged.Coder.Model != base.Coder.Model {
		t.Errorf("expected merged Coder.Model to retain base value")
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
