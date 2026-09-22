package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//nolint:gocyclo // one sequential scenario exercising create, rename, list, resolve, summary, persistence and delete together
func TestSessionsLifecycle(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession("first")
	if err != nil {
		t.Fatal(err)
	}
	if m.GetActiveSessionID() != s.ID {
		t.Fatal("a new session becomes active")
	}
	if err := m.AddMessage(s.ID, ConversationMessage{Role: "user", Content: "hi", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := m.GetSession(s.ID)
	if err != nil || len(got.Messages) != 1 || got.Name != "first" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := m.RenameSession(s.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.ListSessions()
	if len(list) != 1 || list[0].Name != "renamed" || list[0].MessageCount != 1 {
		t.Fatalf("%+v", list)
	}
	if id, err := m.ResolveSessionID(s.ID[:4]); err != nil || id != s.ID {
		t.Fatalf("%q %v", id, err)
	}
	if err := m.UpdateSessionSummary(s.ID, "sum"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if active, _ := reopened.GetActiveSession(); active == nil || active.Summary != "sum" {
		t.Fatalf("persistence: %+v", active)
	}
	// DeleteSession refuses to delete the active session (internal/storage/manager.go), so make
	// another session active first.
	if _, err := m.CreateSession("second"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteSession(s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetSession(s.ID); err == nil {
		t.Fatal("deleted")
	}
}

func TestPreferencesProjectConfigAndPlans(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetPreferredModel("m1"); err != nil || m.GetPreferredModel() != "m1" {
		t.Fatalf("%v %q", err, m.GetPreferredModel())
	}
	cfg := &ProjectConfig{ProjectRoot: "/w", InitializedAt: time.Now(), Version: "3", ProjectType: "go"}
	if err := m.SaveProjectConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if back, err := m.LoadProjectConfig(); err != nil || back.ProjectType != "go" {
		t.Fatalf("%+v %v", back, err)
	}
	plan, err := m.CreatePlan("ship", []string{"a", "b"})
	if err != nil || len(plan.Tasks) != 2 {
		t.Fatal(err)
	}
	if err := m.UpdateTaskStatus(plan.ID, plan.Tasks[0].ID, TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	active, _ := m.GetActivePlan()
	if active == nil || active.Tasks[0].Status != TaskStatusCompleted {
		t.Fatalf("%+v", active)
	}
	if err := m.ArchivePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if active, _ := m.GetActivePlan(); active != nil {
		t.Fatal("archived plans are not active")
	}
}

func TestBackups(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := m.CreateBackup(src)
	if err != nil || !strings.HasPrefix(path, m.GetBackupDir()) {
		t.Fatalf("%q %v", path, err)
	}
	if list, _ := m.ListBackups("a.txt"); len(list) != 1 {
		t.Fatalf("%v", list)
	}
}
