package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// CreatePlan creates a new task plan
func (m *Manager) CreatePlan(title string, taskContents []string) (*Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	plan := &Plan{
		ID:        uuid.New().String(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    PlanStatusActive,
		Tasks:     make([]Task, len(taskContents)),
	}

	for i, content := range taskContents {
		plan.Tasks[i] = Task{
			ID:        uuid.New().String(),
			Content:   content,
			Status:    TaskStatusPending,
			CreatedAt: now,
		}
	}

	if err := m.savePlan(plan); err != nil {
		return nil, err
	}

	// Update current state
	m.currentState.ActivePlanID = plan.ID
	if len(plan.Tasks) > 0 {
		m.currentState.ActiveTaskID = plan.Tasks[0].ID
	}
	_ = m.saveCurrentState()

	return plan, nil
}

// GetActivePlan returns the currently active plan, or nil if none
func (m *Manager) GetActivePlan() (*Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentState.ActivePlanID == "" {
		return nil, nil
	}

	planPath := filepath.Join(m.rootDir, "plans", "active.json")
	data, err := os.ReadFile(planPath) //nolint:gosec // planPath is built from the fixed .taracode root, not user input
	if err != nil {
		return nil, nil
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	return &plan, nil
}

// UpdateTaskStatus updates the status of a task in a plan
func (m *Manager) UpdateTaskStatus(planID, taskID string, status TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, err := m.getActivePlanUnsafe()
	if err != nil || plan == nil || plan.ID != planID {
		return fmt.Errorf("plan not found")
	}

	now := time.Now()
	for i := range plan.Tasks {
		if plan.Tasks[i].ID == taskID {
			plan.Tasks[i].Status = status
			if status == TaskStatusCompleted {
				plan.Tasks[i].CompletedAt = &now
			}
			break
		}
	}

	plan.UpdatedAt = now
	return m.savePlan(plan)
}

// ArchivePlan moves the active plan to archive
func (m *Manager) ArchivePlan(planID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plan, err := m.getActivePlanUnsafe()
	if err != nil || plan == nil || plan.ID != planID {
		return fmt.Errorf("plan not found")
	}

	plan.Status = PlanStatusArchived
	plan.UpdatedAt = time.Now()

	// Move to archive
	archivePath := filepath.Join(m.rootDir, "plans", "archive", fmt.Sprintf("plan_%s.json", plan.ID))
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	//nolint:gosec // matches the 0644 used for every other file this package writes
	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		return err
	}

	// Remove active plan
	_ = os.Remove(filepath.Join(m.rootDir, "plans", "active.json"))

	// Update state
	m.currentState.ActivePlanID = ""
	m.currentState.ActiveTaskID = ""
	return m.saveCurrentState()
}

func (m *Manager) getActivePlanUnsafe() (*Plan, error) {
	planPath := filepath.Join(m.rootDir, "plans", "active.json")
	//nolint:gosec // planPath is built from the fixed .taracode root, not user input
	data, err := os.ReadFile(planPath)
	if err != nil {
		return nil, nil
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w", err)
	}

	return &plan, nil
}

func (m *Manager) savePlan(plan *Plan) error {
	planPath := filepath.Join(m.rootDir, "plans", "active.json")
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}
	//nolint:gosec // matches the 0644 used for every other file this package writes
	return os.WriteFile(planPath, data, 0644)
}
