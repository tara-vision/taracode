package agent

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// newTestAssistant wires an Assistant to a fake Ollama in a temp project with all permission
// prompts pre-allowed. It starts in investigate mode; enterOperate switches.
func newTestAssistant(t *testing.T, streaming bool) (*Assistant, *ollamatest.Server) {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello from disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newForTest(dir, "gemma4:12b", srv.URL, streaming)
	a.permissions = policy.AllowAll()
	return a, srv
}

// enterOperate gives the assistant project storage and switches it to operate mode, which the edit
// tests need: investigate mode refuses every mutation before it reaches the preview.
func enterOperate(t *testing.T, a *Assistant) {
	t.Helper()
	if a.storage == nil {
		st, err := storage.NewManager(a.workingDir)
		if err != nil {
			t.Fatal(err)
		}
		a.storage = st
	}
	if err := a.SetMode(policy.ModeOperate); err != nil {
		t.Fatal(err)
	}
}

func TestPlainAnswerIsRecordedAndUsageCounted(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		a, srv := newTestAssistant(t, streaming)
		srv.Turns = []ollamatest.Turn{{Content: "Four.", PromptTokens: 11, CompletionTokens: 2}}

		if err := a.ProcessMessage("What is 2+2?"); err != nil {
			t.Fatalf("streaming=%v: %v", streaming, err)
		}

		if a.GetLastResponse() != "Four." {
			t.Fatalf("streaming=%v: last response = %q, want %q", streaming, a.GetLastResponse(), "Four.")
		}
		if a.sessionUsage.PromptTokens != 11 || a.sessionUsage.CompletionTokens != 2 || a.sessionUsage.TotalTokens != 13 {
			t.Fatalf("streaming=%v: usage = %+v", streaming, a.sessionUsage)
		}
		last := a.conversation[len(a.conversation)-1]
		if last.Role != openai.ChatMessageRoleAssistant || last.Content != "Four." {
			t.Fatalf("streaming=%v: assistant message not appended: %+v", streaming, last)
		}
		body := lastChatBody(t, srv)
		options, ok := body["options"].(map[string]any)
		if !ok || options["num_ctx"] != float64(32768) {
			t.Fatalf("streaming=%v: request lacks num_ctx: %v", streaming, body["options"])
		}
		if tools, ok := body["tools"].([]any); !ok || len(tools) == 0 {
			t.Fatalf("streaming=%v: request lacks tools", streaming)
		}
	}
}

func TestToolCallRoundTrip(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{Content: "The file says hello from disk."},
	}

	if err := a.ProcessMessage("What does hello.txt say?"); err != nil {
		t.Fatal(err)
	}

	if a.GetLastResponse() != "The file says hello from disk." {
		t.Fatalf("last response = %q", a.GetLastResponse())
	}
	second := lastChatBody(t, srv)
	toolMsg := lastMessage(t, second, 0)
	if toolMsg["role"] != "tool" || toolMsg["tool_name"] != "read_file" ||
		!strings.Contains(toolMsg["content"].(string), "hello from disk") {
		t.Fatalf("tool result not sent back: %v", toolMsg)
	}
	asst := lastMessage(t, second, 1)
	if calls, ok := asst["tool_calls"].([]any); !ok || len(calls) != 1 {
		t.Fatalf("assistant tool call not replayed: %v", asst)
	}
}

// TestToolCallIDsStayUniqueAcrossIterations covers the fix for ids restarting at call_0 on every
// reply: two tool calls made across two iterations of the same turn must not share an id, or
// toolNamesByID (last writer wins on a duplicate id) mislabels one of the replayed tool results
// once the third request replays both.
func TestToolCallIDsStayUniqueAcrossIterations(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{ToolCalls: []ollamatest.ToolCall{{Name: "list_files", Args: map[string]any{"path": "."}}}},
		{Content: "done"},
	}

	if err := a.ProcessMessage("do two things"); err != nil {
		t.Fatal(err)
	}

	third := lastChatBody(t, srv)
	readResult := lastMessage(t, third, 2)
	listResult := lastMessage(t, third, 0)
	if readResult["role"] != "tool" || readResult["tool_name"] != "read_file" {
		t.Fatalf("read_file result mislabeled: %v", readResult)
	}
	if listResult["role"] != "tool" || listResult["tool_name"] != "list_files" {
		t.Fatalf("list_files result mislabeled: %v", listResult)
	}
}

func TestToolOutputIsTruncated(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.truncationCfg = TruncationConfig{MaxLines: 3, MaxChars: 0}
	big := strings.Repeat("line\n", 20)
	if err := os.WriteFile(filepath.Join(a.workingDir, "big.txt"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "big.txt"}}}},
		{Content: "done"},
	}

	if err := a.ProcessMessage("read big.txt"); err != nil {
		t.Fatal(err)
	}

	if len(a.truncationEvents) != 1 || a.truncationEvents[0].ToolName != "read_file" {
		t.Fatalf("truncation events: %+v", a.truncationEvents)
	}
	tool := findMessage(t, a.conversation, openai.ChatMessageRoleTool)
	if !strings.Contains(tool.Content, "truncated") {
		t.Fatalf("truncated output not sent to the model: %+v", tool)
	}
}

func TestMaxIterationsStopsTheLoop(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.maxIterations = 2
	call := ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: "list_files", Args: map[string]any{}}}}
	srv.Turns = []ollamatest.Turn{call, call, call}

	if err := a.ProcessMessage("loop forever"); err != nil {
		t.Fatal(err)
	}

	// Two completions with tools, then the one final completion at the cap with none (ruling P3-R60).
	if n := countPath(srv, "/api/chat"); n != 3 {
		t.Fatalf("expected 3 chat requests, got %d", n)
	}
}

func TestThinkingIsNotStoredInTheConversation(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []ollamatest.Turn{{Content: "yes", Thinking: "let me think"}}

	if err := a.ProcessMessage("is it?"); err != nil {
		t.Fatal(err)
	}

	for _, m := range a.conversation {
		if strings.Contains(m.Content, "let me think") {
			t.Fatalf("thinking leaked into the conversation: %q", m.Content)
		}
	}
	if a.GetLastResponse() != "yes" {
		t.Fatalf("last response = %q, want %q", a.GetLastResponse(), "yes")
	}
}

func TestServerErrorIsReturnedNotSwallowed(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		a, srv := newTestAssistant(t, streaming)
		srv.Turns = []ollamatest.Turn{{Status: 500, Error: "boom"}}

		err := a.ProcessMessage("hi")

		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("streaming=%v: err = %v", streaming, err)
		}
	}
}

func TestEmptyReplyIsNudgedOnce(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{{Content: ""}, {Content: "42"}}

	if err := a.ProcessMessage("what is the answer?"); err != nil {
		t.Fatal(err)
	}

	if a.GetLastResponse() != "42" {
		t.Fatalf("last response = %q, want %q", a.GetLastResponse(), "42")
	}
	if n := countPath(srv, "/api/chat"); n != 2 {
		t.Fatalf("expected 2 chat requests (one nudge), got %d", n)
	}
	var nudges int
	for _, m := range a.conversation {
		if m.Role == openai.ChatMessageRoleUser && strings.Contains(m.Content, "answer the question directly") {
			nudges++
		}
	}
	if nudges != 1 {
		t.Fatalf("nudge messages = %d, want 1", nudges)
	}
}

// withEditPreview turns previews on and makes the decision deterministic, so these tests never
// depend on a terminal.
func withEditPreview(t *testing.T, a *Assistant, choice ui.EditPreviewChoice) {
	t.Helper()
	a.previewEdits = true
	a.previewThreshold = 0
	t.Cleanup(func() {
		a.previewEdits = false
		a.previewThreshold = 0
	})
	a.confirmEditPreview = func(*ui.EditPreview) ui.EditPreviewChoice { return choice }
}

// editTurns scripts a model that edits hello.txt and then acknowledges the outcome.
func editTurns() []ollamatest.Turn {
	args := map[string]any{"path": "hello.txt", "old": "hello from disk", "new": "goodbye"}
	return []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "edit_file", Args: args}}},
		{Content: "Done."},
	}
}

// TestDeclinedEditPreviewIsSentBackToTheModel guards the v2 dead store: the cancellation message
// now reaches the model as the tool result. It also guards the status line, which must not claim
// an edit that never happened.
func TestDeclinedEditPreviewIsSentBackToTheModel(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	enterOperate(t, a)
	withEditPreview(t, a, ui.EditPreviewCancel)
	srv.Turns = editTurns()

	out := captureStdout(t, func() {
		if err := a.ProcessMessage("replace the greeting"); err != nil {
			t.Fatal(err)
		}
	})

	body, err := os.ReadFile(filepath.Join(a.workingDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello from disk" {
		t.Fatalf("file was edited although the preview was declined: %q", body)
	}
	if strings.Contains(out, "Edited") {
		t.Fatalf("a refused tool call printed a success status line: %q", out)
	}
	result := lastMessage(t, lastChatBody(t, srv), 0)
	if result["role"] != "tool" || !strings.Contains(result["content"].(string), "cancelled") {
		t.Fatalf("cancellation not sent back to the model: %v", result)
	}
}

// TestAcceptedEditPreviewAppliesTheEdit is the other half: an approved preview edits the file and
// the status line reports it.
func TestAcceptedEditPreviewAppliesTheEdit(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	enterOperate(t, a)
	withEditPreview(t, a, ui.EditPreviewApply)
	srv.Turns = editTurns()

	out := captureStdout(t, func() {
		if err := a.ProcessMessage("replace the greeting"); err != nil {
			t.Fatal(err)
		}
	})

	body, err := os.ReadFile(filepath.Join(a.workingDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "goodbye" {
		t.Fatalf("approved edit was not applied: %q", body)
	}
	if !strings.Contains(out, "Edited") {
		t.Fatalf("an applied edit printed no status line: %q", out)
	}
	result := lastMessage(t, lastChatBody(t, srv), 0)
	if result["role"] != "tool" || strings.Contains(result["content"].(string), "cancelled") {
		t.Fatalf("edit result not sent back to the model: %v", result)
	}
}

// TestBackupThenApplyFailureWarnsOnScreen covers a third edit-preview outcome: an approved
// "backup then apply" whose CreateBackup fails must not fail silently. The refusal already reaches
// the model as the tool result (executeOne denies it); this pins the on-screen warning.
func TestBackupThenApplyFailureWarnsOnScreen(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.previewEdits = true
	a.previewThreshold = 0
	t.Cleanup(func() {
		a.previewEdits = false
		a.previewThreshold = 0
	})
	storageMgr, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	a.storage = storageMgr
	enterOperate(t, a)
	// Remove the file between the preview decision and the backup step, so CreateBackup fails
	// deterministically instead of relying on filesystem permissions.
	a.confirmEditPreview = func(*ui.EditPreview) ui.EditPreviewChoice {
		if err := os.Remove(filepath.Join(a.workingDir, "hello.txt")); err != nil {
			t.Fatal(err)
		}
		return ui.EditPreviewBackupThenApply
	}
	srv.Turns = editTurns()

	out := captureStdout(t, func() {
		if err := a.ProcessMessage("replace the greeting"); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "Failed to create backup") {
		t.Fatalf("no backup-failure warning printed: %q", out)
	}
	result := lastMessage(t, lastChatBody(t, srv), 0)
	if result["role"] != "tool" || !strings.Contains(result["content"].(string), "Failed to create backup") {
		t.Fatalf("backup failure not sent back to the model: %v", result)
	}
}

// TestDateQuestionsCarryTheCurrentDateTime covers the date/time injection that answers "what
// time is it" without a tool since 3.1.0: the question gets the real clock appended, every other
// message passes through untouched.
func TestDateQuestionsCarryTheCurrentDateTime(t *testing.T) {
	a, _ := newTestAssistant(t, false)

	got := a.injectDatetimeIfNeeded("What time is it?")
	if !strings.Contains(got, "[System: the current date and time") || !strings.Contains(got, time.Now().Format("2006-01-02")) {
		t.Fatalf("date question not annotated: %q", got)
	}
	if got := a.injectDatetimeIfNeeded("list the pods"); got != "list the pods" {
		t.Fatalf("plain message changed: %q", got)
	}
}

// TestStreamedAnswerIsRenderedNotStreamedRaw guards the v2 presentation: the answer is assembled
// behind the spinner and rendered with glamour once, so markdown reaches the screen formatted and
// not as raw deltas. Reasoning is the one thing printed live, before the answer.
func TestStreamedAnswerIsRenderedNotStreamedRaw(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []ollamatest.Turn{{Content: "- one\n- two", Thinking: "counting"}}

	out := captureStdout(t, func() {
		if err := a.ProcessMessage("list two things"); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(out, "- one") {
		t.Fatalf("raw markdown reached the screen instead of the rendered answer: %q", out)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("answer missing from the output: %q", out)
	}
	if !strings.Contains(out, "counting") {
		t.Fatalf("reasoning was not printed: %q", out)
	}
	if strings.Index(out, "counting") > strings.Index(out, "one") {
		t.Fatalf("reasoning must arrive before the answer: %q", out)
	}
}

// TestSpinnersRunThroughATurn exercises the status line and the per-tool spinner, which the rest
// of the suite switches off. Spinner.Stop waits for its goroutine, so the captured pipe is safe.
func TestSpinnersRunThroughATurn(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	a.enableSpinner = true
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{Content: "It greets you.", PromptTokens: 5, CompletionTokens: 3},
	}

	out := captureStdout(t, func() {
		if err := a.ProcessMessage("what does hello.txt say?"); err != nil {
			t.Fatal(err)
		}
	})

	if a.GetLastResponse() != "It greets you." {
		t.Fatalf("last response = %q", a.GetLastResponse())
	}
	if !strings.Contains(out, "It greets you.") {
		t.Fatalf("answer missing from the output: %q", out)
	}
	if a.sessionUsage.TotalTokens != 8 {
		t.Fatalf("usage = %+v", a.sessionUsage)
	}
}

// captureStdout runs fn with os.Stdout replaced by a pipe and returns everything it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()

	fn()

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// lastChatBody returns the body of the last /api/chat request; the server context check that
// closes every turn records a /api/ps request after it.
func lastChatBody(t *testing.T, srv *ollamatest.Server) map[string]any {
	t.Helper()
	for i := len(srv.Requests) - 1; i >= 0; i-- {
		if srv.Requests[i].Path == "/api/chat" {
			return srv.Requests[i].Body
		}
	}
	t.Fatal("no /api/chat request recorded")
	return nil
}

// lastMessage returns the last message of a recorded chat request body.
func lastMessage(t *testing.T, body map[string]any, offsetFromEnd int) map[string]any {
	t.Helper()
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) <= offsetFromEnd {
		t.Fatalf("request has no message at offset %d: %v", offsetFromEnd, body["messages"])
	}
	msg, ok := messages[len(messages)-1-offsetFromEnd].(map[string]any)
	if !ok {
		t.Fatalf("message at offset %d is not an object", offsetFromEnd)
	}
	return msg
}

// findMessage returns the first conversation message with the given role, or fails.
func findMessage(t *testing.T, conversation []openai.ChatCompletionMessage, role string) openai.ChatCompletionMessage {
	t.Helper()
	for _, m := range conversation {
		if m.Role == role {
			return m
		}
	}
	t.Fatalf("no %s message in the conversation", role)
	return openai.ChatCompletionMessage{}
}

func countPath(srv *ollamatest.Server, path string) int {
	n := 0
	for _, r := range srv.Requests {
		if r.Path == path {
			n++
		}
	}
	return n
}
