package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/storage"
)

// Orchestrator coordinates multi-agent task execution
type Orchestrator struct {
	config        OrchestratorConfig
	registry      *agent.Registry
	router        *agent.Router
	taskManager   *storage.TaskManager
	eventHandlers []EventHandler
	taskStates    map[string]*TaskState
	mu            sync.RWMutex
}

// New creates a new orchestrator
func New(registry *agent.Registry, taskManager *storage.TaskManager) *Orchestrator {
	return &Orchestrator{
		config:        DefaultOrchestratorConfig(),
		registry:      registry,
		router:        agent.NewRouter(registry),
		taskManager:   taskManager,
		eventHandlers: make([]EventHandler, 0),
		taskStates:    make(map[string]*TaskState),
	}
}

// SetConfig updates the orchestrator configuration
func (o *Orchestrator) SetConfig(cfg OrchestratorConfig) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.config = cfg
}

// GetConfig returns the current configuration
func (o *Orchestrator) GetConfig() OrchestratorConfig {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.config
}

// AddEventHandler adds an event handler
func (o *Orchestrator) AddEventHandler(handler EventHandler) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.eventHandlers = append(o.eventHandlers, handler)
}

// emitEvent sends an event to all handlers
func (o *Orchestrator) emitEvent(event ExecutionEvent) {
	o.mu.RLock()
	handlers := make([]EventHandler, len(o.eventHandlers))
	copy(handlers, o.eventHandlers)
	o.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

// GetRouter returns the agent router
func (o *Orchestrator) GetRouter() *agent.Router {
	return o.router
}

// GetRegistry returns the agent registry
func (o *Orchestrator) GetRegistry() *agent.Registry {
	return o.registry
}

// GetTaskState returns the state of a task
func (o *Orchestrator) GetTaskState(taskID string) (*TaskState, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	state, ok := o.taskStates[taskID]
	return state, ok
}

// ExecuteTask runs a complete task through the multi-agent system
func (o *Orchestrator) ExecuteTask(ctx context.Context, task *storage.TaskExecution, workingDir string) error {
	// Initialize task state
	state := &TaskState{
		TaskID:      task.ID,
		Status:      storage.TaskExecStatusRunning,
		CurrentStep: -1,
		TotalSteps:  len(task.Steps),
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Context:     NewSharedContext(4096), // 4K token budget for shared context
		Handoffs:    make([]agent.Handoff, 0),
	}

	o.mu.Lock()
	o.taskStates[task.ID] = state
	o.mu.Unlock()

	// Emit task started event
	o.emitEvent(ExecutionEvent{
		Type:      EventTaskStarted,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Task '%s' started with %d steps", task.Name, len(task.Steps)),
		Data:      task,
	})

	// Update task status
	task.Status = storage.TaskExecStatusRunning
	now := time.Now()
	task.StartedAt = &now
	if err := o.taskManager.SaveTask(task); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	// Execute steps
	var lastResult *agent.ExecutionResult
	var priorResults []agent.StepResult

	for i := 0; i < len(task.Steps); i++ {
		step := &task.Steps[i]

		// Update state
		o.mu.Lock()
		state.CurrentStep = i
		state.UpdatedAt = time.Now()
		o.mu.Unlock()

		// Determine which agent should handle this step
		agentType := o.determineAgentForStep(step)

		o.mu.Lock()
		state.ActiveAgent = agentType
		o.mu.Unlock()

		// Get the agent
		selectedAgent, err := o.registry.Get(agentType)
		if err != nil {
			return fmt.Errorf("failed to get agent %s: %w", agentType, err)
		}

		// Create handoff if switching agents
		if lastResult != nil && lastResult.AgentType != agentType {
			handoff := agent.Handoff{
				FromAgent:    lastResult.AgentType,
				ToAgent:      agentType,
				StepIndex:    i,
				Context:      state.Context.Get(),
				Continuation: step.Description,
				Timestamp:    time.Now(),
			}
			state.Handoffs = append(state.Handoffs, handoff)

			o.emitEvent(ExecutionEvent{
				Type:      EventAgentHandoff,
				Timestamp: time.Now(),
				StepIndex: i,
				Agent:     agentType,
				Message:   fmt.Sprintf("Handoff from %s to %s", lastResult.AgentType.DisplayName(), agentType.DisplayName()),
				Data:      handoff,
			})
		}

		// Emit step started event
		o.emitEvent(ExecutionEvent{
			Type:      EventStepStarted,
			Timestamp: time.Now(),
			StepIndex: i,
			Agent:     agentType,
			Message:   fmt.Sprintf("Step %d: %s", i+1, step.Name),
		})

		// Build execution context
		execCtx := &agent.ExecutionContext{
			TaskID:       task.ID,
			StepIndex:    i,
			Prompt:       o.buildStepPrompt(step),
			SharedMemory: make(map[string]interface{}),
			PriorResults: priorResults,
			Context:      state.Context.Get(),
			WorkingDir:   workingDir,
		}

		// Execute the step
		startTime := time.Now()
		step.Status = storage.StepStatusRunning
		stepStartTime := time.Now()
		step.StartedAt = &stepStartTime

		result, err := selectedAgent.Execute(ctx, execCtx)
		endTime := time.Now()

		// Update step with results
		step.CompletedAt = &endTime
		step.Duration = endTime.Sub(startTime).Milliseconds()

		if err != nil || (result != nil && !result.Success) {
			// Step failed
			step.Status = storage.StepStatusFailed
			if err != nil {
				step.Error = err.Error()
			} else if result != nil {
				step.Error = result.Error
			}
			step.Output = result.Output

			o.emitEvent(ExecutionEvent{
				Type:      EventStepFailed,
				Timestamp: time.Now(),
				StepIndex: i,
				Agent:     agentType,
				Message:   fmt.Sprintf("Step %d failed: %s", i+1, step.Error),
			})

			// Try diagnostics if enabled
			if o.config.AutoDiagnostics {
				diagResult := o.runDiagnostics(ctx, result, state, workingDir)
				if diagResult != nil {
					// Add diagnostics context
					state.Context.AddAll(diagResult.Context)

					// Check if retry is recommended
					canRetry := false
					for _, item := range diagResult.Context {
						if item.Key == "can_retry" && item.Value == "true" {
							canRetry = true
							break
						}
					}

					if canRetry && step.Verification != nil && step.Verification.OnFailure == "retry" {
						// Retry the step
						i-- // Decrement to retry same step
						continue
					}
				}
			}

			// Handle failure based on verification settings
			if step.Verification != nil {
				switch step.Verification.OnFailure {
				case "skip":
					step.Status = storage.StepStatusSkipped
					continue
				case "rollback":
					task.Status = storage.TaskExecStatusRolledBack
					return o.taskManager.SaveTask(task)
				default: // abort
					task.Status = storage.TaskExecStatusFailed
					task.Error = step.Error
					o.emitEvent(ExecutionEvent{
						Type:      EventTaskFailed,
						Timestamp: time.Now(),
						Message:   fmt.Sprintf("Task failed at step %d: %s", i+1, step.Error),
					})
					return o.taskManager.SaveTask(task)
				}
			} else {
				// Default to abort
				task.Status = storage.TaskExecStatusFailed
				task.Error = step.Error
				return o.taskManager.SaveTask(task)
			}
		}

		// Step succeeded
		step.Status = storage.StepStatusCompleted
		step.Output = result.Output

		// Add context from this step
		state.Context.AddAll(result.Context)

		// Track prior results
		priorResults = append(priorResults, agent.StepResult{
			StepIndex:  i,
			AgentType:  agentType,
			Output:     result.Output,
			Success:    result.Success,
			Error:      result.Error,
			Context:    result.Context,
			TokensUsed: result.TokensUsed,
			Duration:   result.Duration,
		})

		lastResult = result

		o.emitEvent(ExecutionEvent{
			Type:      EventStepCompleted,
			Timestamp: time.Now(),
			StepIndex: i,
			Agent:     agentType,
			Message:   fmt.Sprintf("Step %d completed successfully", i+1),
		})

		// Save progress
		task.CurrentStep = i
		if err := o.taskManager.SaveTask(task); err != nil {
			return fmt.Errorf("failed to save task progress: %w", err)
		}
	}

	// Task completed successfully
	task.Status = storage.TaskExecStatusCompleted
	completedAt := time.Now()
	task.CompletedAt = &completedAt

	o.mu.Lock()
	state.Status = storage.TaskExecStatusCompleted
	state.UpdatedAt = time.Now()
	o.mu.Unlock()

	o.emitEvent(ExecutionEvent{
		Type:      EventTaskCompleted,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Task '%s' completed successfully", task.Name),
	})

	return o.taskManager.SaveTask(task)
}

// determineAgentForStep determines which agent should handle a step
func (o *Orchestrator) determineAgentForStep(step *storage.TaskStep) agent.Type {
	// Check if step has explicit agent assignment
	// This would come from the planner agent output

	// Check action type
	switch step.Action.Type {
	case storage.ActionTypeTool:
		return o.router.RouteByToolName(step.Action.Tool)
	case storage.ActionTypeCommand:
		// Commands default to coder, but check description
		return o.router.DetermineAgentType(step.Description)
	case storage.ActionTypeAnalyze:
		return agent.TypePlanner
	case storage.ActionTypeManual:
		return agent.TypeCoder // Manual steps still use coder for guidance
	}

	// Fall back to content-based routing
	return o.router.DetermineAgentType(step.Description)
}

// buildStepPrompt builds the prompt for a step
func (o *Orchestrator) buildStepPrompt(step *storage.TaskStep) string {
	prompt := step.Description

	switch step.Action.Type {
	case storage.ActionTypeTool:
		prompt = fmt.Sprintf("%s\n\nUse the tool '%s' with parameters: %v",
			step.Description, step.Action.Tool, step.Action.Params)
	case storage.ActionTypeCommand:
		prompt = fmt.Sprintf("%s\n\nExecute the command: %s",
			step.Description, step.Action.Command)
	case storage.ActionTypeAnalyze:
		if step.Action.Prompt != "" {
			prompt = step.Action.Prompt
		}
	}

	return prompt
}

// runDiagnostics runs the diagnostics agent on a failed step
func (o *Orchestrator) runDiagnostics(ctx context.Context, failedResult *agent.ExecutionResult, state *TaskState, workingDir string) *agent.ExecutionResult {
	o.emitEvent(ExecutionEvent{
		Type:      EventDiagnosticsStart,
		Timestamp: time.Now(),
		StepIndex: state.CurrentStep,
		Agent:     agent.TypeDiagnostics,
		Message:   "Analyzing failure...",
	})

	diagAgent, err := o.registry.Get(agent.TypeDiagnostics)
	if err != nil {
		return nil
	}

	// Build diagnostic context
	execCtx := &agent.ExecutionContext{
		TaskID:    state.TaskID,
		StepIndex: state.CurrentStep,
		Prompt: fmt.Sprintf("Step %d failed with error: %s\n\nOutput: %s",
			state.CurrentStep+1, failedResult.Error, failedResult.Output),
		Context:    state.Context.Get(),
		WorkingDir: workingDir,
	}

	result, err := diagAgent.Execute(ctx, execCtx)
	if err != nil {
		return nil
	}

	return result
}

// PauseTask pauses task execution
func (o *Orchestrator) PauseTask(taskID string) error {
	o.mu.Lock()
	state, ok := o.taskStates[taskID]
	if ok {
		state.Status = storage.TaskExecStatusPaused
		state.UpdatedAt = time.Now()
	}
	o.mu.Unlock()

	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	o.emitEvent(ExecutionEvent{
		Type:      EventTaskPaused,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Task %s paused", taskID),
	})

	return nil
}

// GetActiveAgent returns the currently active agent for a task
func (o *Orchestrator) GetActiveAgent(taskID string) (agent.Type, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	state, ok := o.taskStates[taskID]
	if !ok {
		return "", false
	}
	return state.ActiveAgent, true
}

// GetAgentStates returns the current state of all agents
func (o *Orchestrator) GetAgentStates() map[agent.Type]agent.State {
	return o.registry.GetStates()
}

// GetCurrentTaskState returns the most recently active task state
func (o *Orchestrator) GetCurrentTaskState() *TaskState {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var latest *TaskState
	for _, state := range o.taskStates {
		if latest == nil || state.UpdatedAt.After(latest.UpdatedAt) {
			latest = state
		}
	}
	return latest
}
