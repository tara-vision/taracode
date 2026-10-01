package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

// secondSession adds a session with n messages to the repl's storage and makes the repl's own
// session the active one again.
func secondSession(t *testing.T, r *repl, name string, n int) *storage.Session {
	t.Helper()
	st := r.asst.GetStorage()
	other, err := st.CreateSession(name)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if err := st.AddMessage(other.ID, storage.ConversationMessage{Role: role, Content: "message"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetActiveSession(r.asst.GetSession().ID); err != nil {
		t.Fatal(err)
	}
	return other
}

func TestSessionShowsTheStoredSession(t *testing.T) {
	r, _ := projectREPL(t, ollamatest.Turn{Content: "hello back"})
	_ = captureStdoutForTest(t, func() {
		if err := r.asst.ProcessMessage("hello"); err != nil {
			t.Error(err)
		}
	})
	id := r.asst.GetSession().ID
	st := r.asst.GetStorage()
	if err := st.RenameSession(id, "triage"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateSessionSummary(id, "Said hello."); err != nil {
		t.Fatal(err)
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/session") })
	for _, want := range []string{
		"Current Session:", "ID: " + id[:8], "Name: triage", "Messages: 2", "Created: ", "Updated: ", "Summary: Said hello.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/session lacks %q:\n%s", want, out)
		}
	}
}

func TestSessionCommandsWithoutStorage(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	tests := []struct {
		command, stdout, stderr string
	}{
		{"/session", "No active session.", ""},
		{"/session new", "", "Error creating session: storage not initialized"},
		{"/session load abc", "", "Error loading session: storage not initialized"},
		{"/session delete abc", "", "Error: storage not initialized"},
		{"/session rename abc name", "", "Error: storage not initialized"},
		{"/sessions", "", "Error listing sessions: storage not initialized"},
		{"/session bogus", "Usage: /session [new [\"name\"]|load <id>|delete <id>|rename <id> <name>]", ""},
		{"/session load", "Usage: /session", ""},
	}
	for _, tt := range tests {
		stdout, stderr := captureOutput(t, func() { r.dispatch(tt.command) })
		if !strings.Contains(stdout, tt.stdout) || !strings.Contains(stderr, tt.stderr) {
			t.Errorf("%s: stdout %q, stderr %q", tt.command, stdout, stderr)
		}
	}
}

func TestSessionNewStartsAFreshConversation(t *testing.T) {
	r, _ := projectREPL(t)
	first := r.asst.GetSession().ID
	out := captureStdoutForTest(t, func() { r.dispatch("/session new") })
	if !strings.Contains(out, "Started new conversation session.") || r.asst.GetSession().ID == first {
		t.Fatalf("output %q, session %s", out, r.asst.GetSession().ID)
	}
	out = captureStdoutForTest(t, func() { r.dispatch(`/session new "Bug hunt"`) })
	if !strings.Contains(out, "Started new session: Bug hunt") || r.asst.GetSession().Name != "Bug hunt" {
		t.Fatalf("output %q, session %+v", out, r.asst.GetSession())
	}
}

func TestSessionLoadRebuildsTheConversation(t *testing.T) {
	r, _ := projectREPL(t)
	other := secondSession(t, r, "earlier", 4)
	out := captureStdoutForTest(t, func() { r.dispatch("/session load " + other.ID[:8]) })
	if !strings.Contains(out, "Loaded session with 4 messages.") || r.asst.GetSession().ID != other.ID ||
		r.asst.GetConversationLength() != 5 {
		t.Fatalf("output %q, session %s, %d messages", out, r.asst.GetSession().ID, r.asst.GetConversationLength())
	}
	_, stderr := captureOutput(t, func() { r.dispatch("/session load nope") })
	if !strings.Contains(stderr, "Error loading session: session not found: nope") {
		t.Fatalf("%q", stderr)
	}
}

func TestSessionDeleteAsksFirst(t *testing.T) {
	r, _ := projectREPL(t)
	other := secondSession(t, r, "earlier", 0)
	stdout, stderr := captureOutput(t, func() { r.dispatch("/session delete nope") })
	if !strings.Contains(stderr, "Error: session not found: nope") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	withStdin(t, "n\n", func() {
		stdout = captureStdoutForTest(t, func() { r.dispatch("/session delete " + other.ID) })
	})
	if !strings.Contains(stdout, "Delete session "+other.ID[:8]+"? [y/N]: ") || !strings.Contains(stdout, "Cancelled.") {
		t.Fatalf("%q", stdout)
	}
	withStdin(t, "y\n", func() {
		stdout = captureStdoutForTest(t, func() { r.dispatch("/session delete " + other.ID) })
	})
	sessions, err := r.asst.ListSessions()
	if err != nil || !strings.Contains(stdout, "Session deleted.") || len(sessions) != 1 {
		t.Fatalf("output %q, sessions %+v, err %v", stdout, sessions, err)
	}
	withStdin(t, "y\n", func() {
		_, stderr = captureOutput(t, func() { r.dispatch("/session delete " + r.asst.GetSession().ID) })
	})
	if !strings.Contains(stderr, "Error deleting session: cannot delete the currently active session") {
		t.Fatalf("%q", stderr)
	}
}

func TestSessionRename(t *testing.T) {
	r, _ := projectREPL(t)
	id := r.asst.GetSession().ID
	out := captureStdoutForTest(t, func() { r.dispatch(`/session rename ` + id[:8] + ` "Deploy review"`) })
	sessions, err := r.asst.ListSessions()
	if err != nil || !strings.Contains(out, "Session renamed to: Deploy review") || sessions[0].Name != "Deploy review" {
		t.Fatalf("output %q, sessions %+v, err %v", out, sessions, err)
	}
	_, stderr := captureOutput(t, func() { r.dispatch("/session rename " + id + " " + strings.Repeat("n", 101)) })
	if !strings.Contains(stderr, "Error renaming session: session name too long") {
		t.Fatalf("%q", stderr)
	}
}

func TestSessionsListsEverySession(t *testing.T) {
	r, _ := projectREPL(t)
	active := r.asst.GetSession().ID
	longName := "a very long session name that keeps going"
	other := secondSession(t, r, longName, 2)
	longSummary := strings.Repeat("s", 80)
	if err := r.asst.GetStorage().UpdateSessionSummary(other.ID, longSummary); err != nil {
		t.Fatal(err)
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/sessions") })
	for _, want := range []string{
		"Sessions:", active[:8] + "  (unnamed)", "  0 msgs  ", " *\n",
		other.ID[:8] + "  " + longName[:27] + "...", "  2 msgs  ", "\"" + longSummary[:67] + "...\"",
		"/session load <id>", "/session delete <id>", "/session rename <id> <n>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/sessions lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, " *") != 1 {
		t.Errorf("exactly one session is active:\n%s", out)
	}
}

// TestSessionCommandsWhenNoSessionCouldBeCreated: a project whose history directory cannot be
// written has storage but no session; the commands say so instead of failing.
func TestSessionCommandsWhenNoSessionCouldBeCreated(t *testing.T) {
	skipIfRoot(t)
	dir := t.TempDir()
	history := filepath.Join(dir, ".taracode", "history")
	if err := os.MkdirAll(history, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(history, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(history, 0o755) })
	r := replOn(t, fakeOllama(t), dir, false)
	if r.asst.GetStorage() == nil || r.asst.GetSession() != nil {
		t.Fatalf("storage %v, session %v", r.asst.GetStorage(), r.asst.GetSession())
	}
	for _, tt := range []struct{ command, want string }{
		{"/sessions", "No saved sessions."}, {"/session", "No active session."}, {"/status", "Session: None"},
	} {
		if out := captureStdoutForTest(t, func() { r.dispatch(tt.command) }); !strings.Contains(out, tt.want) {
			t.Errorf("%s lacks %q: %q", tt.command, tt.want, out)
		}
	}
}
