package agent

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// AgentsConfig represents the full agents configuration section
type AgentsConfig struct {
	Enabled           bool    `yaml:"enabled" mapstructure:"enabled"`
	DefaultRouting    string  `yaml:"default_routing" mapstructure:"default_routing"`
	FallbackModel     string  `yaml:"fallback_model" mapstructure:"fallback_model"`
	TimeoutMultiplier float64 `yaml:"timeout_multiplier" mapstructure:"timeout_multiplier"`

	// Per-agent configurations
	Planner     *AgentConfigYAML `yaml:"planner" mapstructure:"planner"`
	Coder       *AgentConfigYAML `yaml:"coder" mapstructure:"coder"`
	Tester      *AgentConfigYAML `yaml:"tester" mapstructure:"tester"`
	Reviewer    *AgentConfigYAML `yaml:"reviewer" mapstructure:"reviewer"`
	DevOps      *AgentConfigYAML `yaml:"devops" mapstructure:"devops"`
	Security    *AgentConfigYAML `yaml:"security" mapstructure:"security"`
	Diagnostics *AgentConfigYAML `yaml:"diagnostics" mapstructure:"diagnostics"`
}

// AgentConfigYAML represents an agent's configuration in YAML format
type AgentConfigYAML struct {
	Model            string   `yaml:"model" mapstructure:"model"`
	Host             string   `yaml:"host,omitempty" mapstructure:"host"` // Host name from hosts config
	Temperature      float32  `yaml:"temperature" mapstructure:"temperature"`
	TopP             float32  `yaml:"top_p" mapstructure:"top_p"`
	NumPredict       int      `yaml:"num_predict" mapstructure:"num_predict"`
	MaxContextTokens int      `yaml:"max_context_tokens" mapstructure:"max_context_tokens"`
	ToolCategories   []string `yaml:"tool_categories" mapstructure:"tool_categories"`
	Timeout          int      `yaml:"timeout" mapstructure:"timeout"`
	MaxToolIter      int      `yaml:"max_tool_iter" mapstructure:"max_tool_iter"`

	// Agent-specific settings
	ReviewStrictness string `yaml:"review_strictness,omitempty" mapstructure:"review_strictness"` // For reviewer
	AutoInvoke       bool   `yaml:"auto_invoke,omitempty" mapstructure:"auto_invoke"`             // For diagnostics
}

// DefaultAgentsConfig returns the default agents configuration
func DefaultAgentsConfig() AgentsConfig {
	return AgentsConfig{
		Enabled:           true,
		DefaultRouting:    "auto",
		FallbackModel:     "gemma3:27b",
		TimeoutMultiplier: 1.0,
		Planner:           defaultAgentConfigYAML(TypePlanner),
		Coder:             defaultAgentConfigYAML(TypeCoder),
		Tester:            defaultAgentConfigYAML(TypeTester),
		Reviewer:          defaultAgentConfigYAML(TypeReviewer),
		DevOps:            defaultAgentConfigYAML(TypeDevOps),
		Security:          defaultAgentConfigYAML(TypeSecurity),
		Diagnostics:       defaultAgentConfigYAML(TypeDiagnostics),
	}
}

// defaultAgentConfigYAML returns the default YAML config for an agent type
func defaultAgentConfigYAML(agentType Type) *AgentConfigYAML {
	cfg := DefaultConfig(agentType)
	yamlCfg := &AgentConfigYAML{
		Model:            cfg.Model,
		Temperature:      cfg.Temperature,
		MaxContextTokens: cfg.MaxContextTokens,
		ToolCategories:   cfg.ToolCategories,
		Timeout:          cfg.Timeout,
		MaxToolIter:      cfg.MaxToolIter,
	}

	// Set agent-specific defaults
	if agentType == TypeReviewer {
		yamlCfg.ReviewStrictness = "medium"
	}
	if agentType == TypeDiagnostics {
		yamlCfg.AutoInvoke = true
	}

	return yamlCfg
}

// LoadAgentsConfig loads agents configuration from viper and project overrides
func LoadAgentsConfig(projectDir string) AgentsConfig {
	cfg := DefaultAgentsConfig()

	// Load from global config (viper)
	if viper.IsSet("agents") {
		if err := viper.UnmarshalKey("agents", &cfg); err != nil {
			// Silently use defaults on error
		}
	}

	// Load project-specific overrides if present
	if projectDir != "" {
		projectCfg := loadProjectAgentsConfig(projectDir)
		if projectCfg != nil {
			cfg = mergeAgentsConfig(cfg, *projectCfg)
		}
	}

	return cfg
}

// loadProjectAgentsConfig loads agents config from .taracode/agents.yaml
func loadProjectAgentsConfig(projectDir string) *AgentsConfig {
	agentsFile := filepath.Join(projectDir, ".taracode", "agents.yaml")

	data, err := os.ReadFile(agentsFile)
	if err != nil {
		return nil // File doesn't exist or can't be read
	}

	var cfg AgentsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil // Invalid YAML
	}

	return &cfg
}

// mergeAgentsConfig merges project config over global config
func mergeAgentsConfig(global, project AgentsConfig) AgentsConfig {
	result := global

	// Override global settings if project specifies them
	if project.DefaultRouting != "" {
		result.DefaultRouting = project.DefaultRouting
	}
	if project.FallbackModel != "" {
		result.FallbackModel = project.FallbackModel
	}
	if project.TimeoutMultiplier != 0 {
		result.TimeoutMultiplier = project.TimeoutMultiplier
	}

	// Merge individual agent configs
	result.Planner = mergeAgentConfigYAML(result.Planner, project.Planner)
	result.Coder = mergeAgentConfigYAML(result.Coder, project.Coder)
	result.Tester = mergeAgentConfigYAML(result.Tester, project.Tester)
	result.Reviewer = mergeAgentConfigYAML(result.Reviewer, project.Reviewer)
	result.DevOps = mergeAgentConfigYAML(result.DevOps, project.DevOps)
	result.Security = mergeAgentConfigYAML(result.Security, project.Security)
	result.Diagnostics = mergeAgentConfigYAML(result.Diagnostics, project.Diagnostics)

	return result
}

// mergeAgentConfigYAML merges two agent configs, preferring non-zero values from override
func mergeAgentConfigYAML(base, override *AgentConfigYAML) *AgentConfigYAML {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}

	result := *base

	if override.Model != "" {
		result.Model = override.Model
	}
	if override.Host != "" {
		result.Host = override.Host
	}
	if override.Temperature != 0 {
		result.Temperature = override.Temperature
	}
	if override.TopP != 0 {
		result.TopP = override.TopP
	}
	if override.NumPredict != 0 {
		result.NumPredict = override.NumPredict
	}
	if override.MaxContextTokens != 0 {
		result.MaxContextTokens = override.MaxContextTokens
	}
	if len(override.ToolCategories) > 0 {
		result.ToolCategories = override.ToolCategories
	}
	if override.Timeout != 0 {
		result.Timeout = override.Timeout
	}
	if override.MaxToolIter != 0 {
		result.MaxToolIter = override.MaxToolIter
	}
	if override.ReviewStrictness != "" {
		result.ReviewStrictness = override.ReviewStrictness
	}
	// AutoInvoke is a bool, always override if project config is set
	result.AutoInvoke = override.AutoInvoke

	return &result
}

// ApplyGlobalModelOptions sets global top_p and num_predict on agents that don't
// have per-agent overrides. Temperature is already per-agent, so it is not touched.
func (ac *AgentsConfig) ApplyGlobalModelOptions(topP float32, numPredict int) {
	for _, cfg := range []*AgentConfigYAML{
		ac.Planner, ac.Coder, ac.Tester, ac.Reviewer,
		ac.DevOps, ac.Security, ac.Diagnostics,
	} {
		if cfg == nil {
			continue
		}
		if cfg.TopP == 0 {
			cfg.TopP = topP
		}
		if cfg.NumPredict == 0 {
			cfg.NumPredict = numPredict
		}
	}
}

// GetAgentConfig returns the Config for a specific agent type from AgentsConfig
func (ac *AgentsConfig) GetAgentConfig(agentType Type) Config {
	var yamlCfg *AgentConfigYAML

	switch agentType {
	case TypePlanner:
		yamlCfg = ac.Planner
	case TypeCoder:
		yamlCfg = ac.Coder
	case TypeTester:
		yamlCfg = ac.Tester
	case TypeReviewer:
		yamlCfg = ac.Reviewer
	case TypeDevOps:
		yamlCfg = ac.DevOps
	case TypeSecurity:
		yamlCfg = ac.Security
	case TypeDiagnostics:
		yamlCfg = ac.Diagnostics
	default:
		return DefaultConfig(agentType)
	}

	if yamlCfg == nil {
		return DefaultConfig(agentType)
	}

	// Apply timeout multiplier
	timeout := yamlCfg.Timeout
	if ac.TimeoutMultiplier != 0 && ac.TimeoutMultiplier != 1.0 {
		timeout = int(float64(timeout) * ac.TimeoutMultiplier)
	}

	return Config{
		Model:            yamlCfg.Model,
		Host:             yamlCfg.Host,
		Temperature:      yamlCfg.Temperature,
		TopP:             yamlCfg.TopP,
		NumPredict:       yamlCfg.NumPredict,
		MaxContextTokens: yamlCfg.MaxContextTokens,
		ToolCategories:   yamlCfg.ToolCategories,
		Timeout:          timeout,
		MaxToolIter:      yamlCfg.MaxToolIter,
	}
}

// SaveProjectAgentsConfig saves agents config to .taracode/agents.yaml
func SaveProjectAgentsConfig(projectDir string, cfg AgentsConfig) error {
	agentsDir := filepath.Join(projectDir, ".taracode")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return err
	}

	agentsFile := filepath.Join(agentsDir, "agents.yaml")

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Add header comment
	header := []byte("# Project-specific agent configuration\n# Overrides ~/.taracode/config.yaml agents section\n\n")
	data = append(header, data...)

	return os.WriteFile(agentsFile, data, 0644)
}

// GenerateExampleConfig generates an example agents.yaml content
func GenerateExampleConfig() string {
	return `# Agent Configuration Example
# Place in .taracode/agents.yaml to override global settings

# Global agent settings
enabled: true
default_routing: auto  # auto, manual, or task-based
fallback_model: gemma3:27b
timeout_multiplier: 1.0

# Individual agent configurations
# Each agent can specify:
#   - model: LLM model to use
#   - host: Named host from hosts config (optional, uses default if not set)
#   - temperature, max_context_tokens, timeout, etc.
planner:
  model: gemma3:12b
  # host: primary        # Uncomment to use specific host
  temperature: 0.3
  max_context_tokens: 4096
  timeout: 60

coder:
  model: gemma3:27b
  # host: primary        # Use powerful GPU host for coding
  temperature: 0.4
  max_context_tokens: 16384
  tool_categories:
    - file
    - git
    - command
  timeout: 300

tester:
  model: gemma3:27b
  temperature: 0.2
  max_context_tokens: 8192
  tool_categories:
    - file
    - command
  timeout: 180

reviewer:
  model: llama3.2:3b
  # host: local          # Use lightweight local model for reviews
  temperature: 0.5
  max_context_tokens: 12288
  tool_categories:
    - file
    - search
  review_strictness: medium  # low, medium, high
  timeout: 180

devops:
  model: gemma3:27b
  temperature: 0.3
  max_context_tokens: 12288
  tool_categories:
    - kubernetes
    - terraform
    - docker
    - cloud
  timeout: 300

security:
  model: gemma3:27b
  temperature: 0.2
  max_context_tokens: 12288
  tool_categories:
    - security
    - file
  timeout: 300

diagnostics:
  model: gemma3:12b
  # host: local          # Quick diagnostics on local
  temperature: 0.2
  max_context_tokens: 4096
  tool_categories:
    - file
    - command
  auto_invoke: true
  timeout: 60
`
}
