package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// MaxOperations is the maximum number of operations to keep in history
	MaxOperations = 100
	// MaxBackupSize is the maximum file size to backup (10MB)
	MaxBackupSize = 10 * 1024 * 1024
)

// Manager handles operation history tracking and undo functionality
type Manager struct {
	rootDir   string // .taracode directory path
	sessionID string
	history   *OperationHistory
	mu        sync.RWMutex
}

// NewManager creates a new history manager for the given session
func NewManager(taracodeDir, sessionID string) (*Manager, error) {
	m := &Manager{
		rootDir:   taracodeDir,
		sessionID: sessionID,
	}

	// Ensure history directory exists
	historyDir := filepath.Join(taracodeDir, "history")
	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create history directory: %w", err)
	}

	// Load existing history or create new
	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

// load reads the operation history from disk
func (m *Manager) load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	historyPath := m.getHistoryPath()
	data, err := os.ReadFile(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize new history
			m.history = &OperationHistory{
				SessionID:  m.sessionID,
				Operations: []Operation{},
				NextID:     1,
			}
			return nil
		}
		return fmt.Errorf("failed to read history file: %w", err)
	}

	var history OperationHistory
	if err := json.Unmarshal(data, &history); err != nil {
		// Corrupted file, start fresh
		m.history = &OperationHistory{
			SessionID:  m.sessionID,
			Operations: []Operation{},
			NextID:     1,
		}
		return nil
	}

	m.history = &history
	return nil
}

// save writes the operation history to disk
func (m *Manager) save() error {
	historyPath := m.getHistoryPath()
	data, err := json.MarshalIndent(m.history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}
	return os.WriteFile(historyPath, data, 0644)
}

func (m *Manager) getHistoryPath() string {
	return filepath.Join(m.rootDir, "history", fmt.Sprintf("operations_%s.json", m.sessionID))
}

// Record adds a new operation to the history
func (m *Manager) Record(op Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Assign ID
	op.ID = m.history.NextID
	m.history.NextID++

	// Add to history
	m.history.Operations = append(m.history.Operations, op)

	// Trim if exceeding max
	if len(m.history.Operations) > MaxOperations {
		// Remove oldest operations (keep the most recent MaxOperations)
		m.history.Operations = m.history.Operations[len(m.history.Operations)-MaxOperations:]
	}

	return m.save()
}

// RecordOperation is a convenience method to create and record an operation
func (m *Manager) RecordOperation(tool string, params map[string]interface{}, target string, success bool, result string, backupPath string) error {
	op := Operation{
		Timestamp:  time.Now(),
		Tool:       tool,
		Type:       ToolToOperationType(tool),
		Params:     params,
		Target:     target,
		BackupPath: backupPath,
		Result:     result,
		Success:    success,
	}
	return m.Record(op)
}

// GetHistory returns the operation history
func (m *Manager) GetHistory(limit int) []Operation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.history.Operations) {
		limit = len(m.history.Operations)
	}

	// Return most recent operations (last 'limit' items)
	start := len(m.history.Operations) - limit
	if start < 0 {
		start = 0
	}

	// Create a copy to avoid race conditions
	result := make([]Operation, limit)
	copy(result, m.history.Operations[start:])

	return result
}

// GetAllHistory returns all operations
func (m *Manager) GetAllHistory() []Operation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Operation, len(m.history.Operations))
	copy(result, m.history.Operations)
	return result
}

// GetOperation returns a specific operation by ID
func (m *Manager) GetOperation(id int) (*Operation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.history.Operations {
		if m.history.Operations[i].ID == id {
			op := m.history.Operations[i]
			return &op, nil
		}
	}
	return nil, fmt.Errorf("operation not found: %d", id)
}

// GetUndoableOperations returns operations that can be undone (most recent first)
func (m *Manager) GetUndoableOperations() []Operation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var undoable []Operation
	for i := len(m.history.Operations) - 1; i >= 0; i-- {
		op := m.history.Operations[i]
		if op.IsUndoable() && !op.Undone && op.Success {
			undoable = append(undoable, op)
		}
	}
	return undoable
}

// Undo reverts the last undoable operation
func (m *Manager) Undo() (*UndoResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find the most recent undoable operation
	var opIndex int = -1
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
		var opIndex int = -1
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Recreate the file
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

// Clear removes all operations from history
func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.history.Operations = []Operation{}
	m.history.NextID = 1
	return m.save()
}

// CreateBackup creates a backup of a file before modification
// Returns the backup path or empty string if backup was skipped
func (m *Manager) CreateBackup(filePath string) (string, error) {
	// Check file size
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // New file, no backup needed
		}
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() > MaxBackupSize {
		// File too large, skip backup but warn
		return "", nil
	}

	// Read original content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	// Create backup directory if needed
	backupDir := filepath.Join(m.rootDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup filename: filename.YYYYMMDD-HHMMSS
	baseName := filepath.Base(filePath)
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s.%s", baseName, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	// Write backup
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return backupPath, nil
}

// CaptureDeletedContent reads file content before deletion
func (m *Manager) CaptureDeletedContent(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", err
	}

	// Skip if too large
	if info.Size() > MaxBackupSize {
		return "", nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// GetStats returns statistics about the operation history
func (m *Manager) GetStats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]int{
		"total":     len(m.history.Operations),
		"undone":    0,
		"undoable":  0,
		"mutations": 0,
	}

	for _, op := range m.history.Operations {
		if op.Undone {
			stats["undone"]++
		}
		if op.IsUndoable() && !op.Undone && op.Success {
			stats["undoable"]++
		}
		if op.IsFileMutation() {
			stats["mutations"]++
		}
	}

	return stats
}

// FileDiff represents a diff for a single file
type FileDiff struct {
	Path        string
	Operation   string // "modified", "created", "deleted", "moved", "copied"
	OriginalPath string // For move operations
	Diff        string // Unified diff content
}

// GenerateDiff generates unified diffs for all file changes in the session
func (m *Manager) GenerateDiff() ([]FileDiff, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect unique files and their most recent state
	fileChanges := make(map[string]*Operation)

	for i := range m.history.Operations {
		op := &m.history.Operations[i]
		if !op.IsFileMutation() || !op.Success || op.Undone {
			continue
		}
		// Store the most recent operation per file
		fileChanges[op.Target] = op
	}

	var diffs []FileDiff

	// Sort files for consistent output
	var paths []string
	for path := range fileChanges {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		op := fileChanges[path]
		diff, err := m.generateFileDiff(op)
		if err != nil {
			continue // Skip files we can't diff
		}
		diffs = append(diffs, diff)
	}

	return diffs, nil
}

// generateFileDiff generates a diff for a single file operation
func (m *Manager) generateFileDiff(op *Operation) (FileDiff, error) {
	diff := FileDiff{
		Path: op.Target,
	}

	switch op.Type {
	case OpTypeWrite, OpTypeEdit:
		diff.Operation = "modified"
		if op.BackupPath == "" {
			// New file
			diff.Operation = "created"
			currentContent, err := os.ReadFile(op.Target)
			if err != nil {
				return diff, err
			}
			diff.Diff = generateUnifiedDiff("/dev/null", op.Target, "", string(currentContent))
		} else {
			// Modified file
			originalContent, err := os.ReadFile(op.BackupPath)
			if err != nil {
				return diff, err
			}
			currentContent, err := os.ReadFile(op.Target)
			if err != nil {
				return diff, err
			}
			diff.Diff = generateUnifiedDiff("a/"+op.Target, "b/"+op.Target, string(originalContent), string(currentContent))
		}

	case OpTypeDelete:
		diff.Operation = "deleted"
		if op.DeletedContent != "" {
			diff.Diff = generateUnifiedDiff("a/"+op.Target, "/dev/null", op.DeletedContent, "")
		}

	case OpTypeMove:
		diff.Operation = "moved"
		diff.OriginalPath = op.OriginalPath
		diff.Diff = fmt.Sprintf("rename from %s\nrename to %s\n", op.OriginalPath, op.Target)

	case OpTypeCopy:
		diff.Operation = "copied"
		diff.OriginalPath = op.OriginalPath
		diff.Diff = fmt.Sprintf("copy from %s\ncopy to %s\n", op.OriginalPath, op.Target)

	case OpTypeCreate:
		diff.Operation = "created"
		diff.Diff = fmt.Sprintf("new directory %s\n", op.Target)
	}

	return diff, nil
}

// generateUnifiedDiff creates a unified diff between two strings
func generateUnifiedDiff(oldName, newName, oldContent, newContent string) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s\n", oldName))
	sb.WriteString(fmt.Sprintf("+++ %s\n", newName))

	// Simple diff algorithm - find changes
	hunks := generateHunks(oldLines, newLines)
	for _, hunk := range hunks {
		sb.WriteString(hunk)
	}

	return sb.String()
}

// splitLines splits content into lines, preserving empty strings for empty content
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	scanner := bufio.NewScanner(strings.NewReader(content))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// generateHunks creates unified diff hunks
func generateHunks(oldLines, newLines []string) []string {
	var hunks []string

	// Use a simple LCS-based diff
	lcs := computeLCS(oldLines, newLines)

	oldIdx, newIdx, lcsIdx := 0, 0, 0
	var currentHunk strings.Builder
	hunkOldStart, hunkNewStart := 1, 1
	hunkOldCount, hunkNewCount := 0, 0
	inHunk := false

	flushHunk := func() {
		if inHunk && currentHunk.Len() > 0 {
			header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkOldStart, hunkOldCount, hunkNewStart, hunkNewCount)
			hunks = append(hunks, header+currentHunk.String())
			currentHunk.Reset()
			inHunk = false
			hunkOldCount, hunkNewCount = 0, 0
		}
	}

	for oldIdx < len(oldLines) || newIdx < len(newLines) {
		if lcsIdx < len(lcs) && oldIdx < len(oldLines) && newIdx < len(newLines) &&
			oldLines[oldIdx] == lcs[lcsIdx] && newLines[newIdx] == lcs[lcsIdx] {
			// Context line (unchanged)
			if inHunk {
				currentHunk.WriteString(" " + oldLines[oldIdx] + "\n")
				hunkOldCount++
				hunkNewCount++
			}
			oldIdx++
			newIdx++
			lcsIdx++
		} else if oldIdx < len(oldLines) && (lcsIdx >= len(lcs) || oldLines[oldIdx] != lcs[lcsIdx]) {
			// Deleted line
			if !inHunk {
				inHunk = true
				hunkOldStart = oldIdx + 1
				hunkNewStart = newIdx + 1
			}
			currentHunk.WriteString("-" + oldLines[oldIdx] + "\n")
			hunkOldCount++
			oldIdx++
		} else if newIdx < len(newLines) && (lcsIdx >= len(lcs) || newLines[newIdx] != lcs[lcsIdx]) {
			// Added line
			if !inHunk {
				inHunk = true
				hunkOldStart = oldIdx + 1
				hunkNewStart = newIdx + 1
			}
			currentHunk.WriteString("+" + newLines[newIdx] + "\n")
			hunkNewCount++
			newIdx++
		}
	}

	flushHunk()
	return hunks
}

// computeLCS computes the longest common subsequence of two string slices
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	if m == 0 || n == 0 {
		return nil
	}

	// DP table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack to find LCS
	lcsLen := dp[m][n]
	lcs := make([]string, lcsLen)
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcsLen--
			lcs[lcsLen] = a[i-1]
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	return lcs
}

// ExportDiff exports all diffs to a string
func (m *Manager) ExportDiff() (string, error) {
	diffs, err := m.GenerateDiff()
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, d := range diffs {
		sb.WriteString(fmt.Sprintf("# %s: %s\n", d.Operation, d.Path))
		if d.OriginalPath != "" {
			sb.WriteString(fmt.Sprintf("# (from %s)\n", d.OriginalPath))
		}
		sb.WriteString(d.Diff)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// GetModifiedFiles returns a list of files modified during the session
func (m *Manager) GetModifiedFiles() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[string]bool)
	var files []string

	for _, op := range m.history.Operations {
		if !op.IsFileMutation() || !op.Success || op.Undone {
			continue
		}
		if !seen[op.Target] {
			seen[op.Target] = true
			files = append(files, op.Target)
		}
	}

	sort.Strings(files)
	return files
}
