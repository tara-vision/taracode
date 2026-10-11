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

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

// newTestAssistant wires an Assistant to a fake engine in a temp project with all permission
// prompts pre-allowed. It starts in investigate mode; enterOperate switches.
func newTestAssistant(t *testing.T, streaming bool) (*Assistant, *taraenginetest.Server) {
	t.Helper()
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{
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
		srv.Turns = []taraenginetest.Turn{{Content: "Four.", PromptTokens: 11, CompletionTokens: 2}}

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
		if tools, ok := lastChatBody(t, srv)["tools"].([]any); !ok || len(tools) == 0 {
			t.Fatalf("streaming=%v: request lacks tools", streaming)
		}
	}
}

func TestToolCallRoundTrip(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []taraenginetest.Turn{
		{ToolCalls: []taraenginetest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
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
	id, name := onlyToolCall(t, lastMessage(t, second, 1))
	if toolMsg["role"] != "tool" || name != "read_file" || id == "" || toolMsg["tool_call_id"] != id ||
		!strings.Contains(toolMsg["content"].(string), "hello from disk") {
		t.Fatalf("tool result not sent back to the call it answers: %v", toolMsg)
	}
}

// TestToolCallIDsStayUniqueAcrossIterations: two tool calls made across two iterations of the same
// turn do not share an id, and each replayed tool result carries the id of the call it answers once
// the third request replays both.
func TestToolCallIDsStayUniqueAcrossIterations(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []taraenginetest.Turn{
		{ToolCalls: []taraenginetest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{ToolCalls: []taraenginetest.ToolCall{{Name: "list_files", Args: map[string]any{"path": "."}}}},
		{Content: "done"},
	}

	if err := a.ProcessMessage("do two things"); err != nil {
		t.Fatal(err)
	}

	third := lastChatBody(t, srv)
	readID, readName := onlyToolCall(t, lastMessage(t, third, 3))
	listID, listName := onlyToolCall(t, lastMessage(t, third, 1))
	readResult := lastMessage(t, third, 2)
	listResult := lastMessage(t, third, 0)
	if readName != "read_file" || readResult["role"] != "tool" || readResult["tool_call_id"] != readID {
		t.Fatalf("read_file result mislabeled: %v", readResult)
	}
	if listName != "list_files" || listResult["role"] != "tool" || listResult["tool_call_id"] != listID {
		t.Fatalf("list_files result mislabeled: %v", listResult)
	}
	if readID == "" || readID == listID {
		t.Fatalf("the two calls share the id %q", readID)
	}
}

func TestToolOutputIsTruncated(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.truncationCfg = TruncationConfig{MaxLines: 3, MaxChars: 0}
	big := strings.Repeat("line\n", 20)
	if err := os.WriteFile(filepath.Join(a.workingDir, "big.txt"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.Turns = []taraenginetest.Turn{
		{ToolCalls: []taraenginetest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "big.txt"}}}},
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
	call := taraenginetest.Turn{ToolCalls: []taraenginetest.ToolCall{{Name: "list_files", Args: map[string]any{}}}}
	srv.Turns = []taraenginetest.Turn{call, call, call}

	if err := a.ProcessMessage("loop forever"); err != nil {
		t.Fatal(err)
	}

	// Two completions with tools, then the one final completion at the cap with none (ruling P3-R60).
	if n := countPath(srv, chatPath); n != 3 {
		t.Fatalf("expected 3 chat requests, got %d", n)
	}
}

func TestThinkingIsNotStoredInTheConversation(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []taraenginetest.Turn{{Content: "yes", Thinking: "let me think"}}

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
		srv.Turns = []taraenginetest.Turn{{Status: 500, Error: "boom"}}

		err := a.ProcessMessage("hi")

		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("streaming=%v: err = %v", streaming, err)
		}
	}
}

func TestEmptyReplyIsNudgedOnce(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []taraenginetest.Turn{{Content: ""}, {Content: "42"}}

	if err := a.ProcessMessage("what is the answer?"); err != nil {
		t.Fatal(err)
	}

	if a.GetLastResponse() != "42" {
		t.Fatalf("last response = %q, want %q", a.GetLastResponse(), "42")
	}
	if n := countPath(srv, chatPath); n != 2 {
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
func editTurns() []taraenginetest.Turn {
	args := map[string]any{"path": "hello.txt", "old": "hello from disk", "new": "goodbye"}
	return []taraenginetest.Turn{
		{ToolCalls: []taraenginetest.ToolCall{{Name: "edit_file", Args: args}}},
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
// time is it" without a tool since 3.1.0: a date or time question gets the real clock appended,
// matched on whole words so "timeout", "uptime", "downtime" and "update today's" pass through
// untouched like every other message.
func TestDateQuestionsCarryTheCurrentDateTime(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 10, 14, 0, time.FixedZone("CEST", 2*3600))
	if got := currentDateTime(now); got != "2026-09-25T13:10:14+02:00 (Friday, CEST)" {
		t.Fatalf("currentDateTime = %q", got)
	}

	got := injectDatetimeIfNeeded("What time is it?", now)
	want := "What time is it?\n\n[System: the current date and time, use it to answer the question]\n" +
		"2026-09-25T13:10:14+02:00 (Friday, CEST)"
	if got != want {
		t.Fatalf("date question = %q", got)
	}
	for _, q := range []string{"What day is it?", "tell me the time", "Current date?", "what's today"} {
		if !isDatetimeQuestion(q) {
			t.Errorf("%q not seen as a date question", q)
		}
	}
	for _, plain := range []string{
		"list the pods", "what's the current timeout on the ingress?", "can you update today's expired certs?",
		"why is there downtime now?", "is the uptime now above 99.9%?",
	} {
		if got := injectDatetimeIfNeeded(plain, now); got != plain {
			t.Errorf("plain message changed: %q", got)
		}
	}
}

// TestTheTurnSendsTheAnnotatedDateQuestion covers the wiring: the annotated message, not the bare
// one, is what the model receives.
func TestTheTurnSendsTheAnnotatedDateQuestion(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []taraenginetest.Turn{{Content: "It is now."}}

	if err := a.ProcessMessage("what time is it?"); err != nil {
		t.Fatal(err)
	}

	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "[System: the current date and time") {
		t.Fatalf("the model did not get the clock: %q", msg)
	}
}

// TestThePromptDateRefreshesWhenTheDayChanges covers a REPL left open past midnight: the prompt's
// "Today is" line is rebuilt at the first turn of a new day, so the model never reasons from
// yesterday.
func TestThePromptDateRefreshesWhenTheDayChanges(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []taraenginetest.Turn{{Content: "hi"}}
	a.promptDay = "2000-01-01"

	if err := a.ProcessMessage("hi"); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if a.promptDay != promptDate(now) {
		t.Fatalf("promptDay = %q, want %q", a.promptDay, promptDate(now))
	}
	messages, _ := lastChatBody(t, srv)["messages"].([]any)
	system, _ := messages[0].(map[string]any)
	if content, _ := system["content"].(string); !strings.Contains(content, dateLine(now)) {
		t.Fatalf("system prompt sent without today's date line:\n%s", content)
	}
}

// TestStreamedAnswerIsRenderedNotStreamedRaw guards the v2 presentation: the answer is assembled
// behind the spinner and rendered with glamour once, so markdown reaches the screen formatted and
// not as raw deltas. Reasoning is the one thing printed live, before the answer.
func TestStreamedAnswerIsRenderedNotStreamedRaw(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []taraenginetest.Turn{{Content: "- one\n- two", Thinking: "counting"}}

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
	srv.Turns = []taraenginetest.Turn{
		{ToolCalls: []taraenginetest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
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
	func() {
		defer func() { os.Stdout = original }() // also when fn fails the test
		fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

// chatPath is the route the engine serves chat on.
const chatPath = "/v1/chat/completions"

// lastChatBody returns the body of the last chat request; the server context check that
// closes every turn records a GET /api/loaded request after it.
func lastChatBody(t *testing.T, srv *taraenginetest.Server) map[string]any {
	t.Helper()
	for i := len(srv.Requests) - 1; i >= 0; i-- {
		if srv.Requests[i].Path == chatPath {
			return srv.Requests[i].Body
		}
	}
	t.Fatalf("no %s request recorded", chatPath)
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

// onlyToolCall returns the id and the function name of the one tool call a recorded assistant message
// replays, or fails.
func onlyToolCall(t *testing.T, assistant map[string]any) (id, name string) {
	t.Helper()
	calls, _ := assistant["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("assistant tool call not replayed: %v", assistant)
	}
	call, _ := calls[0].(map[string]any)
	function, _ := call["function"].(map[string]any)
	id, _ = call["id"].(string)
	name, _ = function["name"].(string)
	return id, name
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

func countPath(srv *taraenginetest.Server, path string) int {
	n := 0
	for _, r := range srv.Requests {
		if r.Path == path {
			n++
		}
	}
	return n
}
