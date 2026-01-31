package orchestrator

import (
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/storage"
)

// OrchestratorConfig defines orchestrator configuration
type OrchestratorConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	DefaultRouting     string `yaml:"default_routing" json:"default_routing"` // auto, manual, task-based
	FallbackModel      string `yaml:"fallback_model" json:"fallback_model"`
	TimeoutMultiplier  float64 `yaml:"timeout_multiplier" json:"timeout_multiplier"`
	MaxReplans         int    `yaml:"max_replans" json:"max_replans"`
	AutoDiagnostics    bool   `yaml:"auto_diagnostics" json:"auto_diagnostics"`
}

// DefaultOrchestratorConfig returns default configuration
func DefaultOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		Enabled:           true,
		DefaultRouting:    "auto",
		FallbackModel:     "gemma3:27b",
		TimeoutMultiplier: 1.0,
		MaxReplans:        3,
		AutoDiagnostics:   true,
	}
}

// TaskState tracks the state of a task during orchestration
type TaskState struct {
	TaskID       string                      `json:"task_id"`
	Status       storage.TaskExecutionStatus `json:"status"`
	CurrentStep  int                         `json:"current_step"`
	TotalSteps   int                         `json:"total_steps"`
	ActiveAgent  agent.Type                  `json:"active_agent"`
	StartedAt    time.Time                   `json:"started_at"`
	UpdatedAt    time.Time                   `json:"updated_at"`
	ReplanCount  int                         `json:"replan_count"`
	Context      *SharedContext              `json:"context"`
	Handoffs     []agent.Handoff             `json:"handoffs"`
}

// SharedContext manages context shared between agents
type SharedContext struct {
	Items       []agent.ContextItem `json:"items"`
	TokenBudget int                 `json:"token_budget"`
	TokensUsed  int                 `json:"tokens_used"`
}

// NewSharedContext creates a new shared context
func NewSharedContext(tokenBudget int) *SharedContext {
	return &SharedContext{
		Items:       make([]agent.ContextItem, 0),
		TokenBudget: tokenBudget,
		TokensUsed:  0,
	}
}

// Add adds a context item, managing the token budget
func (sc *SharedContext) Add(item agent.ContextItem) {
	// Estimate tokens (rough: 4 chars per token)
	estimatedTokens := (len(item.Key) + len(item.Value)) / 4

	// If over budget, remove lowest importance items
	for sc.TokensUsed+estimatedTokens > sc.TokenBudget && len(sc.Items) > 0 {
		// Find lowest importance item
		lowestIdx := 0
		for i, existing := range sc.Items {
			if existing.Importance < sc.Items[lowestIdx].Importance {
				lowestIdx = i
			}
		}
		// Remove it
		removedItem := sc.Items[lowestIdx]
		sc.Items = append(sc.Items[:lowestIdx], sc.Items[lowestIdx+1:]...)
		sc.TokensUsed -= (len(removedItem.Key) + len(removedItem.Value)) / 4
	}

	sc.Items = append(sc.Items, item)
	sc.TokensUsed += estimatedTokens
}

// AddAll adds multiple context items
func (sc *SharedContext) AddAll(items []agent.ContextItem) {
	for _, item := range items {
		sc.Add(item)
	}
}

// Get returns all context items
func (sc *SharedContext) Get() []agent.ContextItem {
	return sc.Items
}

// GetBySource returns context items from a specific agent
func (sc *SharedContext) GetBySource(source agent.Type) []agent.ContextItem {
	var result []agent.ContextItem
	for _, item := range sc.Items {
		if item.Source == source {
			result = append(result, item)
		}
	}
	return result
}

// GetTopN returns the N most important context items
func (sc *SharedContext) GetTopN(n int) []agent.ContextItem {
	if len(sc.Items) <= n {
		return sc.Items
	}

	// Sort by importance (descending) - simple selection
	sorted := make([]agent.ContextItem, len(sc.Items))
	copy(sorted, sc.Items)

	for i := 0; i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Importance > sorted[maxIdx].Importance {
				maxIdx = j
			}
		}
		sorted[i], sorted[maxIdx] = sorted[maxIdx], sorted[i]
	}

	return sorted[:n]
}

// Clear removes all context items
func (sc *SharedContext) Clear() {
	sc.Items = make([]agent.ContextItem, 0)
	sc.TokensUsed = 0
}

// StepResult represents the result of executing a step
type StepResult struct {
	StepIndex   int                  `json:"step_index"`
	AgentType   agent.Type           `json:"agent_type"`
	Result      *agent.ExecutionResult `json:"result"`
	StartedAt   time.Time            `json:"started_at"`
	CompletedAt time.Time            `json:"completed_at"`
}

// ExecutionEvent represents an event during task execution
type ExecutionEvent struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	StepIndex int         `json:"step_index,omitempty"`
	Agent     agent.Type  `json:"agent,omitempty"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

// EventType defines the type of execution event
type EventType string

const (
	EventTaskStarted      EventType = "task_started"
	EventStepStarted      EventType = "step_started"
	EventStepCompleted    EventType = "step_completed"
	EventStepFailed       EventType = "step_failed"
	EventAgentHandoff     EventType = "agent_handoff"
	EventDiagnosticsStart EventType = "diagnostics_start"
	EventReplanStarted    EventType = "replan_started"
	EventTaskCompleted    EventType = "task_completed"
	EventTaskFailed       EventType = "task_failed"
	EventTaskPaused       EventType = "task_paused"
)

// EventHandler is called when an execution event occurs
type EventHandler func(event ExecutionEvent)
