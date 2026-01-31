package agent

import (
	"fmt"
	"sync"

	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/tools"
)

// Registry manages all agent instances
type Registry struct {
	agents      map[Type]Agent
	toolReg     *tools.Registry
	mu          sync.RWMutex
	initialized bool
}

// NewRegistry creates a new agent registry
func NewRegistry(toolReg *tools.Registry) *Registry {
	return &Registry{
		agents:  make(map[Type]Agent),
		toolReg: toolReg,
	}
}

// Initialize creates all agent instances with the given provider using default config
func (r *Registry) Initialize(prov provider.Provider) error {
	return r.InitializeWithConfig(prov, DefaultAgentsConfig())
}

// InitializeWithConfig creates all agent instances with the given provider and config
func (r *Registry) InitializeWithConfig(prov provider.Provider, cfg AgentsConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.initialized {
		return nil
	}

	// Create all agent types with their configurations
	r.agents[TypePlanner] = NewPlannerAgent(prov, r.toolReg)
	r.agents[TypePlanner].SetConfig(cfg.GetAgentConfig(TypePlanner))

	r.agents[TypeCoder] = NewCoderAgent(prov, r.toolReg)
	r.agents[TypeCoder].SetConfig(cfg.GetAgentConfig(TypeCoder))

	r.agents[TypeTester] = NewTesterAgent(prov, r.toolReg)
	r.agents[TypeTester].SetConfig(cfg.GetAgentConfig(TypeTester))

	r.agents[TypeReviewer] = NewReviewerAgent(prov, r.toolReg)
	r.agents[TypeReviewer].SetConfig(cfg.GetAgentConfig(TypeReviewer))

	r.agents[TypeDevOps] = NewDevOpsAgent(prov, r.toolReg)
	r.agents[TypeDevOps].SetConfig(cfg.GetAgentConfig(TypeDevOps))

	r.agents[TypeSecurity] = NewSecurityAgent(prov, r.toolReg)
	r.agents[TypeSecurity].SetConfig(cfg.GetAgentConfig(TypeSecurity))

	r.agents[TypeDiagnostics] = NewDiagnosticsAgent(prov, r.toolReg)
	r.agents[TypeDiagnostics].SetConfig(cfg.GetAgentConfig(TypeDiagnostics))

	r.initialized = true
	return nil
}

// Get returns an agent by type
func (r *Registry) Get(agentType Type) (Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agent, ok := r.agents[agentType]
	if !ok {
		return nil, fmt.Errorf("agent type %s not found", agentType)
	}
	return agent, nil
}

// GetAll returns all registered agents
func (r *Registry) GetAll() map[Type]Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[Type]Agent)
	for k, v := range r.agents {
		result[k] = v
	}
	return result
}

// GetByTaskType returns the best agent for a given task type
func (r *Registry) GetByTaskType(taskType string) (Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, agent := range r.agents {
		if agent.CanHandle(taskType) {
			return agent, nil
		}
	}

	// Default to coder agent for unknown task types
	if agent, ok := r.agents[TypeCoder]; ok {
		return agent, nil
	}

	return nil, fmt.Errorf("no agent found for task type: %s", taskType)
}

// UpdateConfig updates the configuration for a specific agent
func (r *Registry) UpdateConfig(agentType Type, cfg Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent, ok := r.agents[agentType]
	if !ok {
		return fmt.Errorf("agent type %s not found", agentType)
	}

	agent.SetConfig(cfg)
	return nil
}

// UpdateProvider updates the provider for all agents
func (r *Registry) UpdateProvider(prov provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, agent := range r.agents {
		if ba, ok := agent.(*BaseAgent); ok {
			ba.SetProvider(prov)
		} else if pa, ok := agent.(*PlannerAgent); ok {
			pa.SetProvider(prov)
		} else if ca, ok := agent.(*CoderAgent); ok {
			ca.SetProvider(prov)
		} else if ta, ok := agent.(*TesterAgent); ok {
			ta.SetProvider(prov)
		} else if ra, ok := agent.(*ReviewerAgent); ok {
			ra.SetProvider(prov)
		} else if da, ok := agent.(*DevOpsAgent); ok {
			da.SetProvider(prov)
		} else if sa, ok := agent.(*SecurityAgent); ok {
			sa.SetProvider(prov)
		} else if dia, ok := agent.(*DiagnosticsAgent); ok {
			dia.SetProvider(prov)
		}
	}
}

// UpdateProviderForAgent updates the provider for a specific agent
func (r *Registry) UpdateProviderForAgent(agentType Type, prov provider.Provider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent, ok := r.agents[agentType]
	if !ok {
		return fmt.Errorf("agent type %s not found", agentType)
	}

	switch a := agent.(type) {
	case *PlannerAgent:
		a.SetProvider(prov)
	case *CoderAgent:
		a.SetProvider(prov)
	case *TesterAgent:
		a.SetProvider(prov)
	case *ReviewerAgent:
		a.SetProvider(prov)
	case *DevOpsAgent:
		a.SetProvider(prov)
	case *SecurityAgent:
		a.SetProvider(prov)
	case *DiagnosticsAgent:
		a.SetProvider(prov)
	default:
		return fmt.Errorf("unknown agent type: %T", agent)
	}

	return nil
}

// GetStates returns the current state of all agents
func (r *Registry) GetStates() map[Type]State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	states := make(map[Type]State)
	for agentType, agent := range r.agents {
		states[agentType] = agent.GetState()
	}
	return states
}

// GetConfigs returns the configuration of all agents
func (r *Registry) GetConfigs() map[Type]Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	configs := make(map[Type]Config)
	for agentType, agent := range r.agents {
		configs[agentType] = agent.Config()
	}
	return configs
}

// IsInitialized returns true if the registry has been initialized
func (r *Registry) IsInitialized() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.initialized
}

// AgentInfo contains display information about an agent
type AgentInfo struct {
	Type        Type   `json:"type"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Model       string `json:"model"`
	State       State  `json:"state"`
}

// GetAgentInfos returns display information for all agents
func (r *Registry) GetAgentInfos() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var infos []AgentInfo
	for _, agentType := range AllTypes() {
		agent, ok := r.agents[agentType]
		if !ok {
			continue
		}

		cfg := agent.Config()
		state := agent.GetState()

		infos = append(infos, AgentInfo{
			Type:        agentType,
			DisplayName: agentType.DisplayName(),
			Description: agentType.Description(),
			Model:       cfg.Model,
			State:       state,
		})
	}
	return infos
}
