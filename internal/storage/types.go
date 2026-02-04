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

// =============================================================================
// Task Execution Types (v0.3.27)
// =============================================================================

// TaskExecution represents an autonomous multi-step task execution
type TaskExecution struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	OriginalTask string              `json:"original_task"` // User's original natural language request
	Steps        []TaskStep          `json:"steps"`
	Status       TaskExecutionStatus `json:"status"`
	CurrentStep  int                 `json:"current_step"` // 0-indexed, -1 if not started
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	StartedAt    *time.Time          `json:"started_at,omitempty"`
	CompletedAt  *time.Time          `json:"completed_at,omitempty"`
	Checkpoints  []TaskCheckpoint    `json:"checkpoints,omitempty"`
	Error        string              `json:"error,omitempty"`
	SessionID    string              `json:"session_id,omitempty"` // Associated session
}

// TaskExecutionStatus represents the state of a task execution
type TaskExecutionStatus string

const (
	TaskExecStatusPlanning   TaskExecutionStatus = "planning"    // LLM is generating the plan
	TaskExecStatusPending    TaskExecutionStatus = "pending"     // Plan ready, waiting for user approval
	TaskExecStatusRunning    TaskExecutionStatus = "running"     // Currently executing steps
	TaskExecStatusPaused     TaskExecutionStatus = "paused"      // User paused execution
	TaskExecStatusCompleted  TaskExecutionStatus = "completed"   // All steps completed successfully
	TaskExecStatusFailed     TaskExecutionStatus = "failed"      // A step failed and couldn't recover
	TaskExecStatusAborted    TaskExecutionStatus = "aborted"     // User aborted the task
	TaskExecStatusRolledBack TaskExecutionStatus = "rolled_back" // User rolled back changes
)

// TaskStep represents a single step in a task execution
type TaskStep struct {
	ID           string         `json:"id"`
	Index        int            `json:"index"` // 0-indexed position in the plan
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Action       TaskAction     `json:"action"`
	Status       TaskStepStatus `json:"status"`
	Output       string         `json:"output,omitempty"`
	Error        string         `json:"error,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	Duration     int64          `json:"duration_ms,omitempty"`
	RetryCount   int            `json:"retry_count,omitempty"`
	Checkpoint   bool           `json:"checkpoint,omitempty"`   // Create checkpoint before this step
	Verification *TaskVerify    `json:"verification,omitempty"` // Optional verification after step
}

// TaskStepStatus represents the state of a step
type TaskStepStatus string

const (
	StepStatusPending   TaskStepStatus = "pending"
	StepStatusRunning   TaskStepStatus = "running"
	StepStatusCompleted TaskStepStatus = "completed"
	StepStatusFailed    TaskStepStatus = "failed"
	StepStatusSkipped   TaskStepStatus = "skipped"
	StepStatusVerifying TaskStepStatus = "verifying"
	StepStatusRetrying  TaskStepStatus = "retrying"
)

// TaskAction defines what a step should do
type TaskAction struct {
	Type    TaskActionType         `json:"type"`
	Tool    string                 `json:"tool,omitempty"`    // Tool name to execute
	Params  map[string]interface{} `json:"params,omitempty"`  // Tool parameters
	Command string                 `json:"command,omitempty"` // Shell command (for execute type)
	Prompt  string                 `json:"prompt,omitempty"`  // LLM prompt (for analyze type)
}

// TaskActionType defines the type of action
type TaskActionType string

const (
	ActionTypeTool    TaskActionType = "tool"    // Execute a taracode tool
	ActionTypeCommand TaskActionType = "command" // Execute a shell command
	ActionTypeAnalyze TaskActionType = "analyze" // Ask LLM to analyze something
	ActionTypeManual  TaskActionType = "manual"  // Requires manual user action
)

// TaskVerify defines how to verify a step completed successfully
type TaskVerify struct {
	Type      TaskVerifyType         `json:"type"`
	Command   string                 `json:"command,omitempty"`  // Shell command to run
	Expected  string                 `json:"expected,omitempty"` // Expected output (substring match)
	Tool      string                 `json:"tool,omitempty"`     // Tool to run for verification
	Params    map[string]interface{} `json:"params,omitempty"`
	Timeout   int                    `json:"timeout,omitempty"`    // Timeout in seconds
	OnFailure string                 `json:"on_failure,omitempty"` // "retry", "skip", "abort", "rollback"
}

// TaskVerifyType defines the verification method
type TaskVerifyType string

const (
	VerifyTypeCommand  TaskVerifyType = "command"  // Run a shell command
	VerifyTypeTool     TaskVerifyType = "tool"     // Run a taracode tool
	VerifyTypeExitCode TaskVerifyType = "exitcode" // Check command exit code
	VerifyTypeContains TaskVerifyType = "contains" // Check output contains string
)

// TaskCheckpoint captures the state at a point in time for rollback
type TaskCheckpoint struct {
	ID        string       `json:"id"`
	StepIndex int          `json:"step_index"` // Step index this checkpoint was created before
	CreatedAt time.Time    `json:"created_at"`
	Files     []FileBackup `json:"files,omitempty"` // Files backed up at this checkpoint
	Note      string       `json:"note,omitempty"`
}

// FileBackup tracks a file backup for rollback
type FileBackup struct {
	OriginalPath string `json:"original_path"`
	BackupPath   string `json:"backup_path"`
	WasCreated   bool   `json:"was_created"` // True if file didn't exist before (delete on rollback)
}

// TaskIndex tracks all task executions
type TaskIndex struct {
	ActiveTaskID string         `json:"active_task_id,omitempty"`
	Tasks        []TaskMetadata `json:"tasks"`
}

// TaskMetadata contains summary info about a task execution
type TaskMetadata struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Status      TaskExecutionStatus `json:"status"`
	StepCount   int                 `json:"step_count"`
	CurrentStep int                 `json:"current_step"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

// TaskTemplate defines a reusable task definition (YAML)
type TaskTemplate struct {
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Variables   map[string]string  `yaml:"variables,omitempty" json:"variables,omitempty"`
	Steps       []TaskTemplateStep `yaml:"steps" json:"steps"`
}

// TaskTemplateStep defines a step in a task template
type TaskTemplateStep struct {
	Name       string                 `yaml:"name" json:"name"`
	Action     string                 `yaml:"action" json:"action"` // Tool name or "command"
	Params     map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`
	Verify     *TaskTemplateVerify    `yaml:"verify,omitempty" json:"verify,omitempty"`
	Checkpoint bool                   `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty"`
	OnFailure  string                 `yaml:"on_failure,omitempty" json:"on_failure,omitempty"`
	Timeout    int                    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// TaskTemplateVerify defines verification in a template
type TaskTemplateVerify struct {
	Command  string `yaml:"command,omitempty" json:"command,omitempty"`
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	Timeout  int    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}
