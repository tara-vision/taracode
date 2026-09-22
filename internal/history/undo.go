package history

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Undo reverts the last undoable operation
func (m *Manager) Undo() (*UndoResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find the most recent undoable operation
	var opIndex = -1
	for i := len(m.history.Operations) - 1; i >= 0; i-- {
		op := m.history.Operations[i]
		if op.IsUndoable() && !op.Undone && op.Success {
			opIndex = i
			break
		}
	}

	if opIndex == -1 {
		return nil, fmt.Errorf("nothing to undo")
	}

	return m.undoOperation(opIndex)
}

// UndoN reverts the last N undoable operations
func (m *Manager) UndoN(count int) ([]UndoResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []UndoResult

	for i := 0; i < count; i++ {
		// Find the most recent undoable operation not yet undone
		var opIndex = -1
		for j := len(m.history.Operations) - 1; j >= 0; j-- {
			op := m.history.Operations[j]
			if op.IsUndoable() && !op.Undone && op.Success {
				opIndex = j
				break
			}
		}

		if opIndex == -1 {
			if len(results) == 0 {
				return nil, fmt.Errorf("nothing to undo")
			}
			break // No more operations to undo
		}

		result, err := m.undoOperation(opIndex)
		if err != nil {
			result = &UndoResult{
				OperationID: m.history.Operations[opIndex].ID,
				Tool:        m.history.Operations[opIndex].Tool,
				Target:      m.history.Operations[opIndex].Target,
				Success:     false,
				Message:     err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// UndoDryRun previews what undo would do without applying changes
func (m *Manager) UndoDryRun(count int) ([]UndoResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []UndoResult
	undoneIDs := make(map[int]bool)

	for i := 0; i < count; i++ {
		// Find the most recent undoable operation not yet "virtually" undone
		var op *Operation
		for j := len(m.history.Operations) - 1; j >= 0; j-- {
			candidate := &m.history.Operations[j]
			if candidate.IsUndoable() && !candidate.Undone && candidate.Success && !undoneIDs[candidate.ID] {
				op = candidate
				break
			}
		}

		if op == nil {
			if len(results) == 0 {
				return nil, fmt.Errorf("nothing to undo")
			}
			break
		}

		undoneIDs[op.ID] = true

		result := UndoResult{
			OperationID:  op.ID,
			Tool:         op.Tool,
			Target:       op.Target,
			Success:      true,
			RestoredFrom: op.BackupPath,
		}

		switch op.Type {
		case OpTypeWrite, OpTypeEdit:
			if op.BackupPath != "" {
				result.Message = fmt.Sprintf("Would restore %s from backup", op.Target)
			} else {
				result.Message = fmt.Sprintf("Would restore %s (no backup available)", op.Target)
				result.Success = false
			}
		case OpTypeDelete:
			if op.DeletedContent != "" {
				result.Message = fmt.Sprintf("Would recreate %s", op.Target)
			} else {
				result.Message = fmt.Sprintf("Would recreate %s (content not captured)", op.Target)
				result.Success = false
			}
		case OpTypeMove:
			result.Message = fmt.Sprintf("Would move %s back to %s", op.Target, op.OriginalPath)
		case OpTypeCopy:
			result.Message = fmt.Sprintf("Would delete copied file %s", op.CreatedPath)
		default:
			result.Message = "Operation cannot be undone"
			result.Success = false
		}

		results = append(results, result)
	}

	return results, nil
}

// undoOperation performs the actual undo (must be called with lock held)
func (m *Manager) undoOperation(opIndex int) (*UndoResult, error) {
	op := &m.history.Operations[opIndex]

	result := &UndoResult{
		OperationID:  op.ID,
		Tool:         op.Tool,
		Target:       op.Target,
		RestoredFrom: op.BackupPath,
	}

	var err error

	switch op.Type {
	case OpTypeWrite, OpTypeEdit:
		err = m.undoWriteEdit(op)
	case OpTypeDelete:
		err = m.undoDelete(op)
	case OpTypeMove:
		err = m.undoMove(op)
	case OpTypeCopy:
		err = m.undoCopy(op)
	default:
		return nil, fmt.Errorf("operation type %s cannot be undone", op.Type)
	}

	if err != nil {
		result.Success = false
		result.Message = err.Error()
		return result, err
	}

	// Mark as undone
	now := time.Now()
	op.Undone = true
	op.UndoneAt = &now

	// Save updated history
	if err := m.save(); err != nil {
		return nil, fmt.Errorf("failed to save history after undo: %w", err)
	}

	result.Success = true
	result.Message = fmt.Sprintf("Reverted %s on %s", op.Tool, op.Target)

	return result, nil
}

func (m *Manager) undoWriteEdit(op *Operation) error {
	if op.BackupPath == "" {
		return fmt.Errorf("no backup available for %s", op.Target)
	}

	// Read backup content
	backupContent, err := os.ReadFile(op.BackupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup %s: %w", op.BackupPath, err)
	}

	// Restore original file
	//nolint:gosec // matches the 0644 used for every other file this package writes
	if err := os.WriteFile(op.Target, backupContent, 0644); err != nil {
		return fmt.Errorf("failed to restore %s: %w", op.Target, err)
	}

	return nil
}

func (m *Manager) undoDelete(op *Operation) error {
	if op.DeletedContent == "" {
		return fmt.Errorf("deleted content not captured for %s", op.Target)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(op.Target)
	//nolint:gosec // matches the 0755 used for every directory this package creates
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Recreate the file
	//nolint:gosec // matches the 0644 used for every other file this package writes
	if err := os.WriteFile(op.Target, []byte(op.DeletedContent), 0644); err != nil {
		return fmt.Errorf("failed to recreate %s: %w", op.Target, err)
	}

	return nil
}

func (m *Manager) undoMove(op *Operation) error {
	if op.OriginalPath == "" {
		return fmt.Errorf("original path not recorded for move operation")
	}

	// Ensure parent directory of original path exists
	dir := filepath.Dir(op.OriginalPath)
	//nolint:gosec // matches the 0755 used for every directory this package creates
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Move back to original location
	if err := os.Rename(op.Target, op.OriginalPath); err != nil {
		return fmt.Errorf("failed to move %s back to %s: %w", op.Target, op.OriginalPath, err)
	}

	return nil
}

func (m *Manager) undoCopy(op *Operation) error {
	target := op.CreatedPath
	if target == "" {
		target = op.Target
	}

	// Remove the copied file
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return nil // Already removed, that's fine
		}
		return fmt.Errorf("failed to remove copied file %s: %w", target, err)
	}

	return nil
}
