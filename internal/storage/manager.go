package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tara-vision/taracode/internal/context"
)

// Manager handles all persistence operations for .taracode directory
type Manager struct {
	rootDir      string // .taracode directory path
	mu           sync.RWMutex
	sessionIndex *SessionIndex
	currentState *CurrentState
	preferences  *Preferences
}

// NewManager creates a new storage manager for the given project root
func NewManager(projectRoot string) (*Manager, error) {
	taracodeDir := filepath.Join(projectRoot, ".taracode")

	m := &Manager{
		rootDir: taracodeDir,
	}

	// Ensure directory structure exists
	if err := m.ensureDirectories(); err != nil {
		return nil, err
	}

	// Load existing data
	if err := m.loadAll(); err != nil {
		return nil, err
	}

	return m, nil
}

// GetRootDir returns the .taracode directory path
func (m *Manager) GetRootDir() string {
	return m.rootDir
}

func (m *Manager) ensureDirectories() error {
	dirs := []string{
		m.rootDir,
		filepath.Join(m.rootDir, "context"),
		filepath.Join(m.rootDir, "context", "summaries"),
		filepath.Join(m.rootDir, "history"),
		filepath.Join(m.rootDir, "plans"),
		filepath.Join(m.rootDir, "plans", "archive"),
		filepath.Join(m.rootDir, "state"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

func (m *Manager) loadAll() error {
	// Load session index
	m.sessionIndex = &SessionIndex{}
	indexPath := filepath.Join(m.rootDir, "history", "sessions.json")
	if data, err := os.ReadFile(indexPath); err == nil {
		json.Unmarshal(data, m.sessionIndex)
	}

	// Load current state
	m.currentState = &CurrentState{LastActivity: time.Now()}
	statePath := filepath.Join(m.rootDir, "state", "current.json")
	if data, err := os.ReadFile(statePath); err == nil {
		json.Unmarshal(data, m.currentState)
	}

	// Load preferences
	m.preferences = DefaultPreferences()
	prefsPath := filepath.Join(m.rootDir, "state", "preferences.json")
	if data, err := os.ReadFile(prefsPath); err == nil {
		json.Unmarshal(data, m.preferences)
	}

	return nil
}

// ============= Session Management =============

// CreateSession creates a new conversation session
func (m *Manager) CreateSession(name string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &Session{
		ID:        uuid.New().String(),
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []ConversationMessage{},
	}

	// Save session file
	if err := m.saveSession(session); err != nil {
		return nil, err
	}

	// Update index
	meta := SessionMetadata{
		ID:           session.ID,
		Name:         session.Name,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
		MessageCount: 0,
	}
	m.sessionIndex.Sessions = append(m.sessionIndex.Sessions, meta)
	m.sessionIndex.ActiveSessionID = session.ID

	if err := m.saveSessionIndex(); err != nil {
		return nil, err
	}

	return session, nil
}

// GetSession retrieves a session by ID
func (m *Manager) GetSession(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessionPath := filepath.Join(m.rootDir, "history", fmt.Sprintf("session_%s.json", id))
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	return &session, nil
}

// GetActiveSession returns the currently active session, or nil if none
func (m *Manager) GetActiveSession() (*Session, error) {
	m.mu.RLock()
	activeID := m.sessionIndex.ActiveSessionID
	m.mu.RUnlock()

	if activeID == "" {
		return nil, nil
	}

	return m.GetSession(activeID)
}

// SetActiveSession sets the active session by ID
func (m *Manager) SetActiveSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessionIndex.ActiveSessionID = id
	return m.saveSessionIndex()
}

// AddMessage adds a message to the active session
func (m *Manager) AddMessage(sessionID string, msg ConversationMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := m.getSessionUnsafe(sessionID)
	if err != nil {
		return err
	}

	session.Messages = append(session.Messages, msg)
	session.UpdatedAt = time.Now()

	// Trim if exceeding max history
	if m.preferences.MaxHistoryLength > 0 && len(session.Messages) > m.preferences.MaxHistoryLength {
		session.Messages = session.Messages[len(session.Messages)-m.preferences.MaxHistoryLength:]
	}

	if err := m.saveSession(session); err != nil {
		return err
	}

	// Update index
	for i := range m.sessionIndex.Sessions {
		if m.sessionIndex.Sessions[i].ID == sessionID {
			m.sessionIndex.Sessions[i].UpdatedAt = session.UpdatedAt
			m.sessionIndex.Sessions[i].MessageCount = len(session.Messages)
			break
		}
	}

	return m.saveSessionIndex()
}

// ListSessions returns metadata for all sessions
func (m *Manager) ListSessions() ([]SessionMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]SessionMetadata, len(m.sessionIndex.Sessions))
	copy(result, m.sessionIndex.Sessions)
	return result, nil
}

func (m *Manager) getSessionUnsafe(id string) (*Session, error) {
	sessionPath := filepath.Join(m.rootDir, "history", fmt.Sprintf("session_%s.json", id))
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	return &session, nil
}

func (m *Manager) saveSession(session *Session) error {
	sessionPath := filepath.Join(m.rootDir, "history", fmt.Sprintf("session_%s.json", session.ID))
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}
	return os.WriteFile(sessionPath, data, 0644)
}

func (m *Manager) saveSessionIndex() error {
	indexPath := filepath.Join(m.rootDir, "history", "sessions.json")
	data, err := json.MarshalIndent(m.sessionIndex, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, data, 0644)
}

// ============= Plan Management =============

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
	m.saveCurrentState()

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
	if err := os.WriteFile(archivePath, data, 0644); err != nil {
		return err
	}

	// Remove active plan
	os.Remove(filepath.Join(m.rootDir, "plans", "active.json"))

	// Update state
	m.currentState.ActivePlanID = ""
	m.currentState.ActiveTaskID = ""
	return m.saveCurrentState()
}

func (m *Manager) getActivePlanUnsafe() (*Plan, error) {
	planPath := filepath.Join(m.rootDir, "plans", "active.json")
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
	return os.WriteFile(planPath, data, 0644)
}

// ============= Context Management =============

// SaveProjectContext saves the project context to disk
func (m *Manager) SaveProjectContext(ctx *context.ProjectContext) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	contextPath := filepath.Join(m.rootDir, "context", "project.json")
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project context: %w", err)
	}
	return os.WriteFile(contextPath, data, 0644)
}

// LoadProjectContext loads the project context from disk
func (m *Manager) LoadProjectContext() (*context.ProjectContext, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	contextPath := filepath.Join(m.rootDir, "context", "project.json")
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return nil, nil // Not found is not an error
	}

	var ctx context.ProjectContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("failed to parse project context: %w", err)
	}

	return &ctx, nil
}

// SaveFileSummary saves an individual file analysis to disk
func (m *Manager) SaveFileSummary(path string, analysis *context.FileAnalysis) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a safe filename from the path
	safeName := sanitizeFilename(path)
	summaryPath := filepath.Join(m.rootDir, "context", "summaries", safeName+".json")

	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal file analysis: %w", err)
	}
	return os.WriteFile(summaryPath, data, 0644)
}

// ============= State Management =============

// GetCurrentState returns the current runtime state
func (m *Manager) GetCurrentState() *CurrentState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	state := *m.currentState
	return &state
}

// UpdateCurrentState updates the runtime state
func (m *Manager) UpdateCurrentState(state *CurrentState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentState = state
	return m.saveCurrentState()
}

func (m *Manager) saveCurrentState() error {
	statePath := filepath.Join(m.rootDir, "state", "current.json")
	data, err := json.MarshalIndent(m.currentState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal current state: %w", err)
	}
	return os.WriteFile(statePath, data, 0644)
}

// GetPreferences returns the current preferences
func (m *Manager) GetPreferences() *Preferences {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	prefs := *m.preferences
	return &prefs
}

// SavePreferences saves the preferences to disk
func (m *Manager) SavePreferences(prefs *Preferences) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.preferences = prefs
	prefsPath := filepath.Join(m.rootDir, "state", "preferences.json")
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal preferences: %w", err)
	}
	return os.WriteFile(prefsPath, data, 0644)
}

// sanitizeFilename creates a safe filename from a path
func sanitizeFilename(path string) string {
	// Replace path separators and other problematic characters
	result := path
	result = filepath.ToSlash(result) // Normalize to forward slashes
	result = replaceChars(result, "/", "_")
	result = replaceChars(result, "\\", "_")
	result = replaceChars(result, ":", "_")
	result = replaceChars(result, "*", "_")
	result = replaceChars(result, "?", "_")
	result = replaceChars(result, "\"", "_")
	result = replaceChars(result, "<", "_")
	result = replaceChars(result, ">", "_")
	result = replaceChars(result, "|", "_")
	return result
}

func replaceChars(s, old, new string) string {
	result := s
	for {
		replaced := false
		for i := 0; i < len(result); i++ {
			if i+len(old) <= len(result) && result[i:i+len(old)] == old {
				result = result[:i] + new + result[i+len(old):]
				replaced = true
				break
			}
		}
		if !replaced {
			break
		}
	}
	return result
}
