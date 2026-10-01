package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	projectcontext "github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

// twentyLines is a file read_file returns whole: 20 lines, 150 characters, no trailing newline.
func twentyLines() string {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// readNotesTurns is one user turn on the fake: a read_file call for notes.txt, then the answer,
// 1500 prompt and 50 completion tokens in all.
func readNotesTurns() []ollamatest.Turn {
	return []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "notes.txt"}}},
			PromptTokens: 600, CompletionTokens: 20},
		{Content: "Five lines shown.", PromptTokens: 900, CompletionTokens: 30},
	}
}

// storedSession writes a session with messages into dir/.taracode before any assistant exists; it
// is the active session, so agent.New loads it.
func storedSession(t *testing.T, dir string, messages []storage.ConversationMessage) string {
	t.Helper()
	st, err := storage.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession("investigation")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range messages {
		if err := st.AddMessage(session.ID, msg); err != nil {
			t.Fatal(err)
		}
	}
	return session.ID
}

// TestContextShowsTheLoadedSession drives /context through every section: the budget with the
// server's window, a truncated read, the session as loaded, the token usage, the project context,
// the memories, the files read and the mode settings.
func TestContextShowsTheLoadedSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(twentyLines()), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := storedSession(t, dir, []storage.ConversationMessage{
		{Role: "user", Content: "check the values"},
		{Role: "assistant", ToolCalls: []storage.ToolCallRecord{
			{Tool: "read_file", Params: map[string]any{"path": "deploy/values.yaml"}}}},
		{Role: "user", Content: "thanks"},
	})
	module := "github.com/example/" + strings.Repeat("m", 60)
	tools := []string{"kubectl", "helm", "terraform", "docker", "git", "cloud", "scan"}
	st, err := storage.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProjectContext(&projectcontext.ProjectContext{ProjectType: "go", ModuleName: module,
		ImportantFiles: []projectcontext.FileAnalysis{{Path: "main.go"}, {Path: "go.mod"}}, DetectedTools: tools}); err != nil {
		t.Fatal(err)
	}

	srv := fakeOllama(t, readNotesTurns()...)
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, SizeVRAM: 1 << 30}}
	r := replOn(t, srv, dir, false, func(o *agent.Options) { o.Truncation = agent.TruncationConfig{MaxLines: 5} })
	_ = captureStdoutForTest(t, r.enableProject)
	if _, err := r.memory.Create(storage.MemoryCategoryLearning, "Staging is blue", "", nil, storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	_ = captureStdoutForTest(t, func() {
		if err := r.asst.ProcessMessage("read notes.txt"); err != nil {
			t.Error(err)
		}
	})

	out := captureStdoutForTest(t, func() { r.dispatch("/context") })
	registry := r.asst.ToolRegistry()
	for _, want := range []string{
		"Context Window", "Context Budget: ", "Server context:   32.8k tokens (Ollama num_ctx)",
		"Context window (requested): 32.8k tokens",
		"Truncated Outputs: 1", "read_file: 20 -> 5 lines",
		"Session: " + sessionID[:8], "Messages: 2 user, 1 assistant", "Tool calls: 1",
		"LLM Tokens: 1550 (prompt: 1500, completion: 50)",
		"Project Context (from TARACODE.md)", "Type: go", "Module: " + module[:47] + "...", "Important files: 2",
		"Detected tools: " + strings.Join(tools, ", ")[:42] + "...",
		"Project Memories: 1 total, 1 in context", "[learning] Staging is blue",
		"Files Read This Session", "deploy/values.yaml",
		fmt.Sprintf("Mode: investigate (%d of %d tools exposed)", registry.Available(r.asst.Mode()), len(registry.Names())),
		"Compaction: enabled (threshold: 75%)", "Max iterations: 20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/context lacks %q:\n%s", want, out)
		}
	}
}

// TestContextOfAFreshEphemeralSession leaves out every section that has nothing to show.
func TestContextOfAFreshEphemeralSession(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true, func(o *agent.Options) { o.Compaction.Enabled = false })
	out := captureStdoutForTest(t, func() { r.dispatch("/context") })
	for _, absent := range []string{"Session:", "LLM Tokens", "Project Context", "Project Memories", "Files Read", "Truncated"} {
		if strings.Contains(out, absent) {
			t.Errorf("/context shows %q for a fresh ephemeral session:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "Compaction: disabled") || !strings.Contains(out, "Context Budget:") {
		t.Fatalf("/context:\n%s", out)
	}
}

func TestCompactNeedsALongerConversation(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true, func(o *agent.Options) { o.Compaction.Enabled = false })
	out := captureStdoutForTest(t, func() { r.dispatch("/compact") })
	for _, want := range []string{
		"Current context: ", "(1 messages)", "Note: Auto-compaction is disabled. Forcing manual compaction.",
		"Compaction failed: conversation too short to compact (1 messages, need at least 11)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/compact lacks %q:\n%s", want, out)
		}
	}
}

// fivePairs is a stored conversation long enough for /compact: five questions and five answers.
func fivePairs() []storage.ConversationMessage {
	var messages []storage.ConversationMessage
	for i := 1; i <= 5; i++ {
		messages = append(messages,
			storage.ConversationMessage{Role: "user", Content: fmt.Sprintf("question %d", i), Timestamp: time.Now()},
			storage.ConversationMessage{Role: "assistant", Content: fmt.Sprintf("answer %d", i), Timestamp: time.Now()})
	}
	return messages
}

// TestCompactThenStats compacts a loaded conversation through the model's summary, then shows the
// compaction, a truncated read, the token usage and the file operations in /stats.
func TestCompactThenStats(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(twentyLines()), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := storedSession(t, dir, fivePairs())
	turns := append([]ollamatest.Turn{{Content: "They asked five questions."}}, readNotesTurns()...)
	srv := fakeOllama(t, turns...)
	r := replOn(t, srv, dir, false, func(o *agent.Options) { o.Truncation = agent.TruncationConfig{MaxLines: 5} })
	_ = captureStdoutForTest(t, r.enableProject)
	if err := r.asst.LoadSession(sessionID); err != nil {
		t.Fatal(err)
	}

	out := captureStdoutForTest(t, func() { r.dispatch("/compact") })
	if !strings.Contains(out, "Current context: ") || !strings.Contains(out, "(11 -> 10 messages)") {
		t.Fatalf("/compact:\n%s", out)
	}
	summaryRequest := srv.Requests[len(srv.Requests)-1]
	messages, _ := summaryRequest.Body["messages"].([]any)
	if first, _ := messages[0].(map[string]any); !strings.Contains(fmt.Sprint(first["content"]), "Summarize this conversation history") {
		t.Fatalf("the summary request: %+v", summaryRequest.Body)
	}

	_ = captureStdoutForTest(t, func() {
		if err := r.asst.ProcessMessage("read notes.txt"); err != nil {
			t.Error(err)
		}
	})
	for _, op := range []string{"write_file", "write_file", "edit_file"} {
		if err := r.history.RecordOperation(op, nil, filepath.Join(dir, "x.txt"), true, "ok", ""); err != nil {
			t.Fatal(err)
		}
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/stats") })
	for _, want := range []string{
		"Session Statistics", "Compactions: 1", "(1 msgs removed)",
		"LLM Tokens: 1550 total", "Prompt: 1500  Completion: 50",
		"Truncated outputs: 1", "Chars saved: 116", "Redactions: 0",
		"File operations: 3", "  write: 2", "  edit: 1",
		"Compaction: enabled (75% threshold)", "Max iterations: 20 per message",
		"Model options: temp=0.7 top_p=0.9 num_predict=model default",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/stats lacks %q:\n%s", want, out)
		}
	}
}

func TestStatsOfAQuietSession(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true, func(o *agent.Options) { o.Compaction.Enabled = false })
	r.opts.Generation.NumPredict = 256
	out := captureStdoutForTest(t, func() { r.dispatch("/stats") })
	for _, absent := range []string{"LLM Tokens", "Compactions:", "Truncated outputs", "File operations"} {
		if strings.Contains(out, absent) {
			t.Errorf("/stats shows %q for a quiet session:\n%s", absent, out)
		}
	}
	if !strings.Contains(out, "Compaction: disabled") || !strings.Contains(out, "num_predict=256") {
		t.Fatalf("/stats:\n%s", out)
	}
}

func TestUsageReportsTheSessionTokens(t *testing.T) {
	r := replOn(t, fakeOllama(t, ollamatest.Turn{Content: "hi", PromptTokens: 321, CompletionTokens: 12}), t.TempDir(), true)
	_ = captureStdoutForTest(t, func() {
		if err := r.asst.ProcessMessage("hello"); err != nil {
			t.Error(err)
		}
	})
	out := captureStdoutForTest(t, func() { r.dispatch("/usage") })
	for _, want := range []string{"Session Usage:", "Prompt tokens:     321", "Completion tokens: 12", "Total tokens:      333"} {
		if !strings.Contains(out, want) {
			t.Errorf("/usage lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanShowsTheActivePlan(t *testing.T) {
	ephemeral := replOn(t, fakeOllama(t), t.TempDir(), true)
	if out := captureStdoutForTest(t, func() { ephemeral.dispatch("/plan") }); !strings.Contains(out, "Run /init first.") {
		t.Fatalf("%q", out)
	}

	r, _ := projectREPL(t)
	if out := captureStdoutForTest(t, func() { r.dispatch("/plan") }); !strings.Contains(out, "No active plan.") {
		t.Fatalf("%q", out)
	}
	st := r.asst.GetStorage()
	plan, err := st.CreatePlan("Roll out v2", []string{"build", "deploy", "smoke", "announce"})
	if err != nil {
		t.Fatal(err)
	}
	for i, status := range []storage.TaskStatus{storage.TaskStatusCompleted, storage.TaskStatusInProgress, storage.TaskStatusSkipped} {
		if err := st.UpdateTaskStatus(plan.ID, plan.Tasks[i].ID, status); err != nil {
			t.Fatal(err)
		}
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/plan") })
	for _, want := range []string{
		"Plan: Roll out v2", "Status: active", "  1. [x] build", "  2. [>] deploy", "  3. [-] smoke", "  4. [ ] announce",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/plan lacks %q:\n%s", want, out)
		}
	}
}
