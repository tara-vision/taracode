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
	AuditLog   *AuditLog             `json:"audit_log,omitempty"` // Security audit log (security mode only)
}

// ConversationMessage represents a single message in conversation
type ConversationMessage struct {
	Role       string           `json:"role"` // user, assistant, system, tool
	Content    string           `json:"content"`
	Timestamp  time.Time        `json:"timestamp"`
	ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`   // Multiple tool calls per message (native function calling)
	ToolCallID string           `json:"tool_call_id,omitempty"` // For tool response messages
	ToolCall   *ToolCallRecord  `json:"tool_call,omitempty"`    // Deprecated: kept for backward compatibility with old sessions
	Usage      *TokenUsage      `json:"usage,omitempty"`
}

// ToolCallRecord captures tool execution details
type ToolCallRecord struct {
	ID       string                 `json:"id,omitempty"` // Tool call ID for native function calling
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

// OperatingMode defines the operational mode of the assistant
type OperatingMode string

const (
	ModeDevOps   OperatingMode = "devops"   // Default DevOps mode
	ModeSecurity OperatingMode = "security" // Security/DevSecOps mode
)

// CurrentState tracks runtime state
type CurrentState struct {
	ActivePlanID   string        `json:"active_plan_id,omitempty"`
	ActiveTaskID   string        `json:"active_task_id,omitempty"`
	LastActivity   time.Time     `json:"last_activity"`
	WorkingContext string        `json:"working_context,omitempty"`
	Mode           OperatingMode `json:"mode,omitempty"`
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

// ProjectConfig stores project initialization metadata
type ProjectConfig struct {
	ProjectRoot   string    `json:"project_root"`
	InitializedAt time.Time `json:"initialized_at"`
	Version       string    `json:"version"`
	ProjectType   string    `json:"type,omitempty"`           // Detected project type (Go, Node.js, Python, etc.)
	DetectedTools []string  `json:"detected_tools,omitempty"` // Relevant taracode tools for this project
	Frameworks    []string  `json:"frameworks,omitempty"`     // Detected frameworks (docker, kubernetes, terraform)
}

// =============================================================================
// Security Audit Log Types
// =============================================================================

// AuditAction represents the type of audit action
type AuditAction string

const (
	AuditActionAllow    AuditAction = "allow"     // User allowed the operation
	AuditActionDeny     AuditAction = "deny"      // User denied the operation
	AuditActionAllowAll AuditAction = "allow_all" // User allowed all remaining operations
	AuditActionDenyAll  AuditAction = "deny_all"  // User denied all remaining operations
)

// AuditEntry records a single security audit decision
type AuditEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	ToolName    string                 `json:"tool_name"`
	Category    string                 `json:"category"` // write, execute, git, destructive
	Action      AuditAction            `json:"action"`
	Params      map[string]interface{} `json:"params,omitempty"`
	Target      string                 `json:"target,omitempty"`      // Primary target (file path, command, etc.)
	Implication string                 `json:"implication,omitempty"` // Security implication description
	BatchIndex  int                    `json:"batch_index,omitempty"` // Position in batch (1-based), 0 if not batch
	BatchTotal  int                    `json:"batch_total,omitempty"` // Total in batch, 0 if not batch
}

// AuditLog contains all audit entries for a session
type AuditLog struct {
	Entries     []AuditEntry `json:"entries"`
	TotalAllow  int          `json:"total_allow"`  // Count of allowed operations
	TotalDeny   int          `json:"total_deny"`   // Count of denied operations
	SessionMode string       `json:"session_mode"` // Operating mode when audit occurred
}
