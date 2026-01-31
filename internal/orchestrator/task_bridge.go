package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
)

// TaskBridge connects the orchestrator to the existing task system
type TaskBridge struct {
	orchestrator *Orchestrator
	providerPool *provider.Pool
	toolRegistry *tools.Registry
	taskManager  *storage.TaskManager
	workingDir   string
	initialized  bool
}

// NewTaskBridge creates a new task bridge
func NewTaskBridge(
	provPool *provider.Pool,
	toolReg *tools.Registry,
	taskMgr *storage.TaskManager,
	workingDir string,
) *TaskBridge {
	return &TaskBridge{
		providerPool: provPool,
		toolRegistry: toolReg,
		taskManager:  taskMgr,
		workingDir:   workingDir,
	}
}

// Initialize sets up the orchestrator and agent system
func (tb *TaskBridge) Initialize() error {
	return tb.InitializeWithConfig(agent.DefaultAgentsConfig())
}

// InitializeWithConfig sets up the orchestrator with specific configuration
func (tb *TaskBridge) InitializeWithConfig(cfg agent.AgentsConfig) error {
	if tb.initialized {
		return nil
	}

	// Create agent registry
	agentRegistry := agent.NewRegistry(tb.toolRegistry)

	// Initialize with provider and config
	if err := agentRegistry.InitializeWithConfig(tb.providerPool.GetDefault(), cfg); err != nil {
		return fmt.Errorf("failed to initialize agent registry: %w", err)
	}

	// Create orchestrator
	tb.orchestrator = New(agentRegistry, tb.taskManager)

	// Set up event handler for logging
	tb.orchestrator.AddEventHandler(tb.defaultEventHandler)

	tb.initialized = true
	return nil
}

// ReloadConfig reloads the agent configuration from files
func (tb *TaskBridge) ReloadConfig(projectDir string) error {
	cfg := agent.LoadAgentsConfig(projectDir)

	if tb.orchestrator != nil && tb.orchestrator.GetRegistry() != nil {
		registry := tb.orchestrator.GetRegistry()
		for _, agentType := range agent.AllTypes() {
			if err := registry.UpdateConfig(agentType, cfg.GetAgentConfig(agentType)); err != nil {
				return fmt.Errorf("failed to update config for %s: %w", agentType, err)
			}
		}
	}

	return nil
}

// IsInitialized returns whether the bridge is initialized
func (tb *TaskBridge) IsInitialized() bool {
	return tb.initialized
}

// GetOrchestrator returns the orchestrator
func (tb *TaskBridge) GetOrchestrator() *Orchestrator {
	return tb.orchestrator
}

// PlanTask uses the planner agent to create a task plan
func (tb *TaskBridge) PlanTask(ctx context.Context, description string) (*storage.TaskExecution, error) {
	if !tb.initialized {
		if err := tb.Initialize(); err != nil {
			return nil, err
		}
	}

	// Get planner agent
	plannerAgent, err := tb.orchestrator.GetRegistry().Get(agent.TypePlanner)
	if err != nil {
		return nil, fmt.Errorf("failed to get planner agent: %w", err)
	}

	// Build execution context for planning
	execCtx := &agent.ExecutionContext{
		Prompt:     description,
		WorkingDir: tb.workingDir,
	}

	// Execute planner
	result, err := plannerAgent.Execute(ctx, execCtx)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}

	// Parse the plan from the planner output
	planner, ok := plannerAgent.(*agent.PlannerAgent)
	if !ok {
		return nil, fmt.Errorf("unexpected planner agent type")
	}

	plan, err := planner.ParsePlan(result.Output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	// Convert to TaskExecution
	task := tb.planToTaskExecution(plan, description)

	return task, nil
}

// planToTaskExecution converts a TaskPlan to a TaskExecution
func (tb *TaskBridge) planToTaskExecution(plan *agent.TaskPlan, originalTask string) *storage.TaskExecution {
	task := &storage.TaskExecution{
		ID:           generateID(),
		Name:         plan.Name,
		Description:  plan.Description,
		OriginalTask: originalTask,
		Status:       storage.TaskExecStatusPending,
		CurrentStep:  -1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Checkpoints:  []storage.TaskCheckpoint{},
	}

	// Convert plan steps to task steps
	for _, ps := range plan.Steps {
		step := storage.TaskStep{
			ID:          generateID(),
			Index:       ps.Index,
			Name:        ps.Name,
			Description: ps.Description,
			Status:      storage.StepStatusPending,
			Checkpoint:  ps.Checkpoint,
			Action: storage.TaskAction{
				Type:    storage.TaskActionType(ps.ActionType),
				Tool:    ps.Tool,
				Command: ps.Command,
				Prompt:  ps.Prompt,
			},
		}

		// Set up verification if specified
		if ps.OnFailure != "" {
			step.Verification = &storage.TaskVerify{
				OnFailure: ps.OnFailure,
			}
		}

		task.Steps = append(task.Steps, step)
	}

	return task
}

// ExecuteTask runs a task using the multi-agent orchestrator
func (tb *TaskBridge) ExecuteTask(ctx context.Context, task *storage.TaskExecution) error {
	if !tb.initialized {
		if err := tb.Initialize(); err != nil {
			return err
		}
	}

	return tb.orchestrator.ExecuteTask(ctx, task, tb.workingDir)
}

// ExecuteTaskWithCallback runs a task with a custom event callback
func (tb *TaskBridge) ExecuteTaskWithCallback(ctx context.Context, task *storage.TaskExecution, callback EventHandler) error {
	if !tb.initialized {
		if err := tb.Initialize(); err != nil {
			return err
		}
	}

	// Add temporary event handler
	tb.orchestrator.AddEventHandler(callback)

	return tb.orchestrator.ExecuteTask(ctx, task, tb.workingDir)
}

// UpdateAgentProvider updates the provider for a specific agent
func (tb *TaskBridge) UpdateAgentProvider(agentType agent.Type, model string) error {
	if !tb.initialized {
		return fmt.Errorf("task bridge not initialized")
	}

	// Get or create provider for the model
	prov, err := tb.providerPool.GetOrCreate(model)
	if err != nil {
		return fmt.Errorf("failed to get provider for model %s: %w", model, err)
	}

	// Update the agent's provider
	return tb.orchestrator.GetRegistry().UpdateProviderForAgent(agentType, prov)
}

// GetAgentInfos returns information about all agents
func (tb *TaskBridge) GetAgentInfos() []agent.AgentInfo {
	if !tb.initialized || tb.orchestrator == nil {
		// Return default agent info if not initialized
		var infos []agent.AgentInfo
		for _, agentType := range agent.AllTypes() {
			cfg := agent.DefaultConfig(agentType)
			infos = append(infos, agent.AgentInfo{
				Type:        agentType,
				DisplayName: agentType.DisplayName(),
				Description: agentType.Description(),
				Model:       cfg.Model,
			})
		}
		return infos
	}

	return tb.orchestrator.GetRegistry().GetAgentInfos()
}

// defaultEventHandler is the default event handler for logging
func (tb *TaskBridge) defaultEventHandler(event ExecutionEvent) {
	// This is a no-op handler - actual UI updates are handled by the REPL
	// Subclasses or callers can add their own handlers
}

// generateID creates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// RoutePrompt determines which agent should handle a prompt
func (tb *TaskBridge) RoutePrompt(prompt string) (agent.Type, agent.RoutingInfo) {
	if !tb.initialized || tb.orchestrator == nil {
		// Use default routing
		router := agent.NewRouter(nil)
		info := router.ExplainRouting(prompt)
		return info.MatchedAgent, info
	}

	router := tb.orchestrator.GetRouter()
	info := router.ExplainRouting(prompt)
	return info.MatchedAgent, info
}

// AgentStatus contains status information for display
type AgentStatus struct {
	Agents      []agent.AgentInfo
	ActiveTask  string
	ActiveAgent agent.Type
	TaskState   *TaskState
}

// GetStatus returns the current status of the agent system
func (tb *TaskBridge) GetStatus() AgentStatus {
	status := AgentStatus{
		Agents: tb.GetAgentInfos(),
	}

	if tb.orchestrator != nil {
		// Check for active task
		// This would need task manager integration
	}

	return status
}

// NewTaskBridgeFromProvider creates a TaskBridge using a single provider
// This is useful when you don't have a full provider pool
func NewTaskBridgeFromProvider(
	prov provider.Provider,
	toolReg *tools.Registry,
	taskMgr *storage.TaskManager,
	workingDir string,
	host string,
	apiKey string,
) *TaskBridge {
	// Create a pool with the single provider as default
	pool := provider.NewPool(prov, host, apiKey)

	return &TaskBridge{
		providerPool: pool,
		toolRegistry: toolReg,
		taskManager:  taskMgr,
		workingDir:   workingDir,
	}
}

// GetProviderPool returns the provider pool
func (tb *TaskBridge) GetProviderPool() *provider.Pool {
	return tb.providerPool
}

// SetWorkingDir updates the working directory
func (tb *TaskBridge) SetWorkingDir(dir string) {
	tb.workingDir = dir
}

// GetTaskState returns the current task state from the orchestrator
func (tb *TaskBridge) GetTaskState() *TaskState {
	if tb.orchestrator == nil {
		return nil
	}
	return tb.orchestrator.GetCurrentTaskState()
}
