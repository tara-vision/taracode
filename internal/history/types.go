package history

import "time"

// OperationType categorizes the type of operation
type OperationType string

const (
	OpTypeWrite      OperationType = "write"   // write_file, append_file
	OpTypeEdit       OperationType = "edit"    // edit_file, insert_lines, replace_lines, delete_lines
	OpTypeDelete     OperationType = "delete"  // delete_file
	OpTypeMove       OperationType = "move"    // move_file
	OpTypeCopy       OperationType = "copy"    // copy_file
	OpTypeRead       OperationType = "read"    // read_file (non-mutating, for context)
	OpTypeCreate     OperationType = "create"  // create_directory
	OpTypeExecute    OperationType = "execute" // execute_command (non-undoable)
)

// Operation represents a single tool operation in the history
type Operation struct {
	ID          int                    `json:"id"`                    // Sequential ID within session
	Timestamp   time.Time              `json:"timestamp"`             // When operation occurred
	Tool        string                 `json:"tool"`                  // Tool name (e.g., "edit_file")
	Type        OperationType          `json:"type"`                  // Operation category
	Params      map[string]interface{} `json:"params"`                // Tool parameters
	Target      string                 `json:"target"`                // Primary target (file path)
	BackupPath  string                 `json:"backup_path,omitempty"` // Path to backup file (for undoable ops)
	Result      string                 `json:"result"`                // "success" or error message
	Success     bool                   `json:"success"`               // Whether operation succeeded
	LinesChanged int                   `json:"lines_changed,omitempty"` // Number of lines affected
	BytesWritten int64                 `json:"bytes_written,omitempty"` // Bytes written/modified
	Undone      bool                   `json:"undone,omitempty"`      // Whether this op was undone
	UndoneAt    *time.Time             `json:"undone_at,omitempty"`   // When it was undone

	// For delete operations, store content to restore
	DeletedContent string `json:"deleted_content,omitempty"`

	// For move operations, store original path
	OriginalPath string `json:"original_path,omitempty"`

	// For copy operations, store created path
	CreatedPath string `json:"created_path,omitempty"`
}

// OperationHistory contains all operations for a session
type OperationHistory struct {
	SessionID  string      `json:"session_id"`
	Operations []Operation `json:"operations"`
	NextID     int         `json:"next_id"` // Next operation ID to assign
}

// IsUndoable returns whether this operation type can be undone
func (o *Operation) IsUndoable() bool {
	switch o.Type {
	case OpTypeWrite, OpTypeEdit, OpTypeDelete, OpTypeMove, OpTypeCopy:
		return true
	default:
		return false
	}
}

// IsFileMutation returns whether this operation modifies files
func (o *Operation) IsFileMutation() bool {
	switch o.Type {
	case OpTypeWrite, OpTypeEdit, OpTypeDelete, OpTypeMove, OpTypeCopy, OpTypeCreate:
		return true
	default:
		return false
	}
}

// ToolToOperationType maps tool names to operation types
func ToolToOperationType(toolName string) OperationType {
	switch toolName {
	case "write_file", "append_file":
		return OpTypeWrite
	case "edit_file", "insert_lines", "replace_lines", "delete_lines":
		return OpTypeEdit
	case "delete_file":
		return OpTypeDelete
	case "move_file":
		return OpTypeMove
	case "copy_file":
		return OpTypeCopy
	case "read_file", "list_files", "find_files", "search_files":
		return OpTypeRead
	case "create_directory":
		return OpTypeCreate
	case "execute_command":
		return OpTypeExecute
	default:
		return OpTypeExecute // Default to execute for unknown tools
	}
}

// UndoResult contains the result of an undo operation
type UndoResult struct {
	OperationID int       `json:"operation_id"`
	Tool        string    `json:"tool"`
	Target      string    `json:"target"`
	Success     bool      `json:"success"`
	Message     string    `json:"message"`
	RestoredFrom string   `json:"restored_from,omitempty"` // Backup path used
}
