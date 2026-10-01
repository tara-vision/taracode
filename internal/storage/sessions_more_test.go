package storage

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	return m, root
}

// replaceWithDir puts a non-empty directory where a file is expected, so writing it, opening it
// for writing and removing it all fail, whoever runs the test.
func replaceWithDir(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// replaceWithFile puts a regular file where a directory is expected.
func replaceWithFile(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

// farFuture cannot be written as JSON: encoding/json refuses a year past 9999.
var farFuture = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)

func sessionPath(root, id string) string {
	return filepath.Join(root, ".taracode", "history", "session_"+id+".json")
}

func TestNewManagerNeedsADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "project")
	if err := os.WriteFile(file, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(file); err == nil || !strings.HasPrefix(err.Error(), "failed to create directory ") {
		t.Fatalf("err = %v", err)
	}
	m, root := newManager(t)
	if m.GetRootDir() != filepath.Join(root, ".taracode") {
		t.Fatalf("root %q", m.GetRootDir())
	}
}

func TestSessionLookupsReportUnknownIDs(t *testing.T) {
	m, _ := newManager(t)
	if s, err := m.GetActiveSession(); s != nil || err != nil {
		t.Fatalf("no session is active yet: %v %v", s, err)
	}
	if _, err := m.CreateSession("a"); err != nil {
		t.Fatal(err)
	}
	b, err := m.CreateSession("b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ResolveSessionID(""); err == nil || err.Error() != "ambiguous session ID '' matches 2 sessions" {
		t.Fatalf("an empty prefix matches both: %v", err)
	}
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"SetActiveSession", func() error { return m.SetActiveSession("zzz") }},
		{"DeleteSession", func() error { return m.DeleteSession("zzz") }},
		{"RenameSession", func() error { return m.RenameSession("zzz", "x") }},
		{"UpdateSessionSummary", func() error { return m.UpdateSessionSummary("zzz", "x") }},
	} {
		if err := op.run(); err == nil || err.Error() != "session not found: zzz" {
			t.Errorf("%s: %v", op.name, err)
		}
	}
	if err := m.AddMessage("zzz", ConversationMessage{}); err == nil || !strings.HasPrefix(err.Error(), "session not found: ") {
		t.Errorf("AddMessage takes a full ID: %v", err)
	}
	if err := m.DeleteSession(b.ID); err == nil || err.Error() != "cannot delete the currently active session" {
		t.Errorf("DeleteSession(active): %v", err)
	}
}

func TestSetActiveSessionIsPersisted(t *testing.T) {
	m, root := newManager(t)
	a, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateSession("b"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveSession(a.ID[:8]); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if active, err := reopened.GetActiveSession(); err != nil || active == nil || active.ID != a.ID || active.Name != "a" {
		t.Fatalf("active %+v, err %v", active, err)
	}
}

func TestSessionFilesThatAreGoneOrBroken(t *testing.T) {
	m, root := newManager(t)
	s, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	path := sessionPath(root, s.ID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"GetSession": func() error { _, err := m.GetSession(s.ID); return err }(),
		"Rename":     m.RenameSession(s.ID, "x"),
		"Summary":    m.UpdateSessionSummary(s.ID, "x"),
	} {
		if err == nil || !strings.HasPrefix(err.Error(), "session not found: ") {
			t.Errorf("%s of a session without a file: %v", name, err)
		}
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetSession(s.ID); err == nil || !strings.HasPrefix(err.Error(), "failed to parse session: ") {
		t.Errorf("GetSession of a broken file: %v", err)
	}
	if err := m.AddMessage(s.ID, ConversationMessage{}); err == nil || !strings.HasPrefix(err.Error(), "failed to parse session: ") {
		t.Errorf("AddMessage to a broken file: %v", err)
	}
}

func TestRenameSessionLimitsTheName(t *testing.T) {
	m, _ := newManager(t)
	s, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RenameSession(s.ID, strings.Repeat("n", 101)); err == nil || err.Error() != "session name too long (max 100 characters)" {
		t.Fatalf("101 characters: %v", err)
	}
	if err := m.RenameSession(s.ID, strings.Repeat("n", 100)); err != nil {
		t.Fatalf("100 characters: %v", err)
	}
}

func TestAddMessageKeepsTheNewestMessages(t *testing.T) {
	m, _ := newManager(t)
	prefs := m.GetPreferences()
	prefs.MaxHistoryLength = 2
	if err := m.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	s, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"one", "two", "three"} {
		if err := m.AddMessage(s.ID, ConversationMessage{Role: "user", Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := m.GetSession(s.ID)
	if err != nil || len(got.Messages) != 2 || got.Messages[0].Content != "two" || got.Messages[1].Content != "three" {
		t.Fatalf("%+v %v", got, err)
	}
	if list, _ := m.ListSessions(); list[0].MessageCount != 2 {
		t.Fatalf("index %+v", list)
	}
}

func TestSessionWritesReportFailures(t *testing.T) {
	m, root := newManager(t)
	s, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	unencodable := ConversationMessage{ToolCalls: []ToolCallRecord{{Tool: "x", Params: map[string]interface{}{"n": math.NaN()}}}}
	if err := m.AddMessage(s.ID, unencodable); err == nil || !strings.HasPrefix(err.Error(), "failed to marshal session: ") {
		t.Fatalf("AddMessage: %v", err)
	}
	history := filepath.Join(root, ".taracode", "history")
	replaceWithDir(t, filepath.Join(history, "sessions.json"))
	if _, err := m.CreateSession("b"); err == nil || !strings.Contains(err.Error(), "sessions.json") {
		t.Fatalf("an index that cannot be written: %v", err)
	}
	replaceWithFile(t, history)
	if _, err := m.CreateSession("c"); err == nil || !strings.Contains(err.Error(), "session_") {
		t.Fatalf("a session file that cannot be written: %v", err)
	}
}

func TestDeleteSessionFiles(t *testing.T) {
	m, root := newManager(t)
	a, _ := m.CreateSession("a")
	b, _ := m.CreateSession("b")
	if _, err := m.CreateSession("c"); err != nil {
		t.Fatal(err)
	}
	replaceWithDir(t, sessionPath(root, a.ID))
	if err := m.DeleteSession(a.ID); err == nil || !strings.HasPrefix(err.Error(), "failed to delete session file: ") {
		t.Fatalf("a file it cannot remove: %v", err)
	}
	if err := os.Remove(sessionPath(root, b.ID)); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteSession(b.ID); err != nil {
		t.Fatalf("a file already gone is not an error: %v", err)
	}
	list, _ := m.ListSessions()
	for _, meta := range list {
		if meta.ID == b.ID {
			t.Fatal("the deleted session is still listed")
		}
	}
}

func TestSessionUpdatesReportAFileTheyCannotWrite(t *testing.T) {
	skipIfRoot(t)
	m, root := newManager(t)
	s, err := m.CreateSession("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sessionPath(root, s.ID), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := m.RenameSession(s.ID, "b"); err == nil || !os.IsPermission(err) {
		t.Errorf("RenameSession: %v", err)
	}
	if err := m.UpdateSessionSummary(s.ID, "sum"); err == nil || !os.IsPermission(err) {
		t.Errorf("UpdateSessionSummary: %v", err)
	}
}
