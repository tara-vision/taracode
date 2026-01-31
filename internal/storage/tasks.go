package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TaskManager handles task execution persistence
type TaskManager struct {
	rootDir   string // .taracode directory path
	taskIndex *TaskIndex
}

// NewTaskManager creates a new task manager
func NewTaskManager(taracodeDir string) (*TaskManager, error) {
	tm := &TaskManager{
		rootDir: taracodeDir,
	}

	// Ensure tasks directory exists
	tasksDir := filepath.Join(taracodeDir, "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}

	// Ensure checkpoints directory exists
	checkpointsDir := filepath.Join(tasksDir, "checkpoints")
	if err := os.MkdirAll(checkpointsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoints directory: %w", err)
	}

	// Load task index
	if err := tm.loadIndex(); err != nil {
		return nil, err
	}

	return tm, nil
}

func (tm *TaskManager) loadIndex() error {
	tm.taskIndex = &TaskIndex{}
	indexPath := filepath.Join(tm.rootDir, "tasks", "index.json")

	if data, err := os.ReadFile(indexPath); err == nil {
		if err := json.Unmarshal(data, tm.taskIndex); err != nil {
			// Reset to empty index if corrupted
			tm.taskIndex = &TaskIndex{}
		}
	}

	return nil
}

func (tm *TaskManager) saveIndex() error {
	indexPath := filepath.Join(tm.rootDir, "tasks", "index.json")
	data, err := json.MarshalIndent(tm.taskIndex, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, data, 0644)
}

// CreateTask creates a new task execution
func (tm *TaskManager) CreateTask(name, description, originalTask string) (*TaskExecution, error) {
	task := &TaskExecution{
		ID:           uuid.New().String(),
		Name:         name,
		Description:  description,
		OriginalTask: originalTask,
		Steps:        []TaskStep{},
		Status:       TaskExecStatusPlanning,
		CurrentStep:  -1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Checkpoints:  []TaskCheckpoint{},
	}

	if err := tm.SaveTask(task); err != nil {
		return nil, err
	}

	// Add to index
	tm.taskIndex.Tasks = append(tm.taskIndex.Tasks, TaskMetadata{
		ID:          task.ID,
		Name:        task.Name,
		Status:      task.Status,
		StepCount:   0,
		CurrentStep: -1,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
	})

	if err := tm.saveIndex(); err != nil {
		return nil, err
	}

	return task, nil
}

// SaveTask saves a task execution to disk
func (tm *TaskManager) SaveTask(task *TaskExecution) error {
	task.UpdatedAt = time.Now()

	taskPath := filepath.Join(tm.rootDir, "tasks", fmt.Sprintf("task_%s.json", task.ID))
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(taskPath, data, 0644); err != nil {
		return err
	}

	// Update index
	tm.updateIndexEntry(task)
	return tm.saveIndex()
}

func (tm *TaskManager) updateIndexEntry(task *TaskExecution) {
	for i, meta := range tm.taskIndex.Tasks {
		if meta.ID == task.ID {
			tm.taskIndex.Tasks[i] = TaskMetadata{
				ID:          task.ID,
				Name:        task.Name,
				Status:      task.Status,
				StepCount:   len(task.Steps),
				CurrentStep: task.CurrentStep,
				CreatedAt:   task.CreatedAt,
				UpdatedAt:   task.UpdatedAt,
			}
			return
		}
	}
	// Task not in index - add it
	tm.taskIndex.Tasks = append(tm.taskIndex.Tasks, TaskMetadata{
		ID:          task.ID,
		Name:        task.Name,
		Status:      task.Status,
		StepCount:   len(task.Steps),
		CurrentStep: task.CurrentStep,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
	})
}

// LoadTask loads a task execution from disk
func (tm *TaskManager) LoadTask(id string) (*TaskExecution, error) {
	// Support partial ID matching
	fullID := tm.resolveTaskID(id)
	if fullID == "" {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	taskPath := filepath.Join(tm.rootDir, "tasks", fmt.Sprintf("task_%s.json", fullID))
	data, err := os.ReadFile(taskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load task: %w", err)
	}

	var task TaskExecution
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// resolveTaskID finds the full task ID from a partial ID
func (tm *TaskManager) resolveTaskID(partialID string) string {
	for _, meta := range tm.taskIndex.Tasks {
		if meta.ID == partialID || strings.HasPrefix(meta.ID, partialID) {
			return meta.ID
		}
	}
	return ""
}

// DeleteTask removes a task execution
func (tm *TaskManager) DeleteTask(id string) error {
	fullID := tm.resolveTaskID(id)
	if fullID == "" {
		return fmt.Errorf("task not found: %s", id)
	}

	// Delete task file
	taskPath := filepath.Join(tm.rootDir, "tasks", fmt.Sprintf("task_%s.json", fullID))
	os.Remove(taskPath)

	// Delete checkpoints directory
	checkpointDir := filepath.Join(tm.rootDir, "tasks", "checkpoints", fullID)
	os.RemoveAll(checkpointDir)

	// Remove from index
	newTasks := []TaskMetadata{}
	for _, meta := range tm.taskIndex.Tasks {
		if meta.ID != fullID {
			newTasks = append(newTasks, meta)
		}
	}
	tm.taskIndex.Tasks = newTasks

	if tm.taskIndex.ActiveTaskID == fullID {
		tm.taskIndex.ActiveTaskID = ""
	}

	return tm.saveIndex()
}

// ListTasks returns all task metadata
func (tm *TaskManager) ListTasks() []TaskMetadata {
	return tm.taskIndex.Tasks
}

// ListTasksByStatus returns tasks with a specific status
func (tm *TaskManager) ListTasksByStatus(status TaskExecutionStatus) []TaskMetadata {
	result := []TaskMetadata{}
	for _, meta := range tm.taskIndex.Tasks {
		if meta.Status == status {
			result = append(result, meta)
		}
	}
	return result
}

// GetActiveTask returns the currently active task, if any
func (tm *TaskManager) GetActiveTask() (*TaskExecution, error) {
	if tm.taskIndex.ActiveTaskID == "" {
		return nil, nil
	}
	return tm.LoadTask(tm.taskIndex.ActiveTaskID)
}

// SetActiveTask sets the currently active task
func (tm *TaskManager) SetActiveTask(taskID string) error {
	if taskID != "" {
		fullID := tm.resolveTaskID(taskID)
		if fullID == "" {
			return fmt.Errorf("task not found: %s", taskID)
		}
		tm.taskIndex.ActiveTaskID = fullID
	} else {
		tm.taskIndex.ActiveTaskID = ""
	}
	return tm.saveIndex()
}

// CreateCheckpoint creates a checkpoint for a task at the current step
func (tm *TaskManager) CreateCheckpoint(task *TaskExecution, note string) (*TaskCheckpoint, error) {
	checkpoint := &TaskCheckpoint{
		ID:        uuid.New().String(),
		StepIndex: task.CurrentStep,
		CreatedAt: time.Now(),
		Files:     []FileBackup{},
		Note:      note,
	}

	// Create checkpoint directory
	checkpointDir := filepath.Join(tm.rootDir, "tasks", "checkpoints", task.ID, checkpoint.ID)
	if err := os.MkdirAll(checkpointDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	task.Checkpoints = append(task.Checkpoints, *checkpoint)

	if err := tm.SaveTask(task); err != nil {
		return nil, err
	}

	return checkpoint, nil
}

// BackupFile backs up a file to a checkpoint
func (tm *TaskManager) BackupFile(task *TaskExecution, checkpointID, filePath string) error {
	// Find checkpoint
	var checkpoint *TaskCheckpoint
	for i := range task.Checkpoints {
		if task.Checkpoints[i].ID == checkpointID {
			checkpoint = &task.Checkpoints[i]
			break
		}
	}
	if checkpoint == nil {
		return fmt.Errorf("checkpoint not found: %s", checkpointID)
	}

	// Create backup
	checkpointDir := filepath.Join(tm.rootDir, "tasks", "checkpoints", task.ID, checkpointID)
	backupName := strings.ReplaceAll(filePath, "/", "_")
	backupPath := filepath.Join(checkpointDir, backupName)

	// Check if file exists
	wasCreated := false
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		wasCreated = true
	} else {
		// Copy file to backup
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file for backup: %w", err)
		}
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write backup: %w", err)
		}
	}

	// Record backup
	checkpoint.Files = append(checkpoint.Files, FileBackup{
		OriginalPath: filePath,
		BackupPath:   backupPath,
		WasCreated:   wasCreated,
	})

	return tm.SaveTask(task)
}

// RollbackToCheckpoint restores files from a checkpoint
func (tm *TaskManager) RollbackToCheckpoint(task *TaskExecution, checkpointID string) error {
	// Find checkpoint
	var checkpoint *TaskCheckpoint
	var checkpointIndex int
	for i := range task.Checkpoints {
		if task.Checkpoints[i].ID == checkpointID {
			checkpoint = &task.Checkpoints[i]
			checkpointIndex = i
			break
		}
	}
	if checkpoint == nil {
		return fmt.Errorf("checkpoint not found: %s", checkpointID)
	}

	// Restore files in reverse order
	for i := len(checkpoint.Files) - 1; i >= 0; i-- {
		backup := checkpoint.Files[i]

		if backup.WasCreated {
			// File was created during task, delete it
			os.Remove(backup.OriginalPath)
		} else {
			// Restore from backup
			data, err := os.ReadFile(backup.BackupPath)
			if err != nil {
				return fmt.Errorf("failed to read backup: %w", err)
			}
			if err := os.WriteFile(backup.OriginalPath, data, 0644); err != nil {
				return fmt.Errorf("failed to restore file: %w", err)
			}
		}
	}

	// Update task state
	task.CurrentStep = checkpoint.StepIndex - 1
	task.Status = TaskExecStatusPaused

	// Remove checkpoints after this one
	task.Checkpoints = task.Checkpoints[:checkpointIndex+1]

	return tm.SaveTask(task)
}

// GetLatestCheckpoint returns the most recent checkpoint for a task
func (tm *TaskManager) GetLatestCheckpoint(task *TaskExecution) *TaskCheckpoint {
	if len(task.Checkpoints) == 0 {
		return nil
	}
	return &task.Checkpoints[len(task.Checkpoints)-1]
}

// CleanupOldTasks removes completed/aborted tasks older than the given duration
func (tm *TaskManager) CleanupOldTasks(olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for _, meta := range tm.taskIndex.Tasks {
		// Only cleanup completed, failed, or aborted tasks
		if meta.Status != TaskExecStatusCompleted &&
			meta.Status != TaskExecStatusFailed &&
			meta.Status != TaskExecStatusAborted &&
			meta.Status != TaskExecStatusRolledBack {
			continue
		}

		if meta.UpdatedAt.Before(cutoff) {
			if err := tm.DeleteTask(meta.ID); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}
