package storage

import "time"

// Session represents a conversation session
type Session struct {
	ID         string                `json:"id"`
	Name       string                `json:"name,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
	Messages   []ConversationMessage `json:"messages"`
	Summary    string                `json:"summary,omitempty"`
	Tags       []string              `json:"tags,omitempty"`
	TotalUsage *TokenUsage           `json:"total_usage,omitempty"`
}

// ConversationMessage represents a single message in conversation
type ConversationMessage struct {
	Role      string          `json:"role"` // user, assistant, system, tool
	Content   string          `json:"content"`
	Timestamp time.Time       `json:"timestamp"`
	ToolCall  *ToolCallRecord `json:"tool_call,omitempty"`
	Usage     *TokenUsage     `json:"usage,omitempty"`
}

// ToolCallRecord captures tool execution details
type ToolCallRecord struct {
	Tool     string                 `json:"tool"`
	Params   map[string]interface{} `json:"params"`
	Result   string                 `json:"result"`
	Duration int64                  `json:"duration_ms"`
	Success  bool                   `json:"success"`
}

// TokenUsage tracks token consumption for an LLM call
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SessionIndex tracks all sessions
type SessionIndex struct {
	ActiveSessionID string            `json:"active_session_id"`
	Sessions        []SessionMetadata `json:"sessions"`
}

// SessionMetadata contains summary information about a session
type SessionMetadata struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
	Summary      string    `json:"summary,omitempty"`
}

// Plan represents a task plan
type Plan struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Status      PlanStatus `json:"status"`
	Tasks       []Task     `json:"tasks"`
}

// PlanStatus represents the state of a plan
type PlanStatus string

const (
	PlanStatusActive    PlanStatus = "active"
	PlanStatusCompleted PlanStatus = "completed"
	PlanStatusArchived  PlanStatus = "archived"
)

// Task represents a single task within a plan
type Task struct {
	ID          string     `json:"id"`
	Content     string     `json:"content"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Notes       string     `json:"notes,omitempty"`
	SubTasks    []Task     `json:"sub_tasks,omitempty"`
}

// TaskStatus represents the state of a task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusSkipped    TaskStatus = "skipped"
)

// CurrentState tracks runtime state
type CurrentState struct {
	ActivePlanID   string    `json:"active_plan_id,omitempty"`
	ActiveTaskID   string    `json:"active_task_id,omitempty"`
	LastActivity   time.Time `json:"last_activity"`
	WorkingContext string    `json:"working_context,omitempty"`
}

// Preferences stores user preferences for this project
type Preferences struct {
	AutoLoadContext   bool     `json:"auto_load_context"`
	MaxHistoryLength  int      `json:"max_history_length"`
	PreferredModel    string   `json:"preferred_model,omitempty"`
	ExcludeDirs       []string `json:"exclude_dirs,omitempty"`
	CustomPromptRules []string `json:"custom_prompt_rules,omitempty"`
}

// DefaultPreferences returns sensible default preferences
func DefaultPreferences() *Preferences {
	return &Preferences{
		AutoLoadContext:  true,
		MaxHistoryLength: 100,
	}
}
