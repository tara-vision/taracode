package assistant

import (
	"bytes"
	gocontext "context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
)

// newTestAssistant wires an Assistant to a fake Ollama in a temp project with all tool prompts
// pre-allowed.
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
	a.permMgr = permissions.NewManagerAllowAll()
	return a, srv
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
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "hello.txt"}}}},
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
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "hello.txt"}}}},
		{ToolCalls: []ollamatest.ToolCall{{Name: "list_files", Args: map[string]any{"directory": "."}}}},
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
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "big.txt"}}}},
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
	call := ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: "get_datetime", Args: map[string]any{}}}}
	srv.Turns = []ollamatest.Turn{call, call, call}

	if err := a.ProcessMessage("loop forever"); err != nil {
		t.Fatal(err)
	}

	if n := countPath(srv, "/api/chat"); n != 2 {
		t.Fatalf("expected 2 chat requests, got %d", n)
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

// TestJSONFallbackWhenTheModelHasNoNativeTools covers the path taken after a "does not support
// tools" error: tools leave the request, tool calls are parsed out of the content, the stored reply
// keeps the call the model wrote and the results go back as one user message.
func TestJSONFallbackWhenTheModelHasNoNativeTools(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{
		{Status: 400, Error: "model gemma4:12b does not support tools"},
		{Content: `Reading it now. {"tool": "read_file", "params": {"file_path": "hello.txt"}}`},
		{Content: "It says hello from disk."},
	}

	if err := a.ProcessMessage("what does hello.txt say?"); err != nil {
		t.Fatal(err)
	}

	if a.useNativeTools {
		t.Fatal("useNativeTools still true after a no-tool-support error")
	}
	if a.GetLastResponse() != "It says hello from disk." {
		t.Fatalf("last response = %q", a.GetLastResponse())
	}
	last := lastChatBody(t, srv)
	if _, ok := last["tools"]; ok {
		t.Fatalf("tools still sent after the fallback: %v", last["tools"])
	}
	result := lastMessage(t, last, 0)
	if result["role"] != "user" || !strings.Contains(result["content"].(string), "hello from disk") {
		t.Fatalf("fallback tool result not sent back as a user message: %v", result)
	}
	asst := findMessage(t, a.conversation, openai.ChatMessageRoleAssistant)
	if !strings.Contains(asst.Content, "read_file") {
		t.Fatalf("the stored reply dropped the tool call the model wrote: %q", asst.Content)
	}
}

// withEditPreview turns previews on and makes the decision deterministic, so these tests never
// depend on a terminal.
func withEditPreview(t *testing.T, a *Assistant, choice ui.EditPreviewChoice) {
	t.Helper()
	viper.Set("preview_edits", true)
	viper.Set("preview_threshold", 0)
	t.Cleanup(func() {
		viper.Set("preview_edits", false)
		viper.Set("preview_threshold", 0)
	})
	a.confirmEditPreview = func(*ui.EditPreview) ui.EditPreviewChoice { return choice }
}

// editTurns scripts a model that edits hello.txt and then acknowledges the outcome.
func editTurns() []ollamatest.Turn {
	args := map[string]any{"file_path": "hello.txt", "old_string": "hello from disk", "new_string": "goodbye"}
	return []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "edit_file", Args: args}}},
		{Content: "Done."},
	}
}

// TestFallbackCallWithAnIDStillGoesBackAsAUserMessage pins the routing rule: only a reply that
// came through the native path may answer with tool-role messages, whatever id the parsed JSON
// happens to carry.
func TestFallbackCallWithAnIDStillGoesBackAsAUserMessage(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.useNativeTools = false
	before := len(a.conversation)

	a.runToolCalls([]*ToolCall{{
		ID:     "fallback-arbitrary-id",
		Tool:   "read_file",
		Params: map[string]interface{}{"file_path": "hello.txt"},
	}}, "reply", false)

	if len(a.conversation) != before+1 {
		t.Fatalf("expected one aggregated message, got %d new ones", len(a.conversation)-before)
	}
	added := a.conversation[len(a.conversation)-1]
	if added.Role != openai.ChatMessageRoleUser || !strings.Contains(added.Content, "hello from disk") {
		t.Fatalf("fallback result was misrouted: %+v", added)
	}
}

// TestDeclinedEditPreviewIsSentBackToTheModel guards the v2 dead store: the cancellation message
// now reaches the model as the tool result. It also guards the status line, which must not claim
// an edit that never happened.
func TestDeclinedEditPreviewIsSentBackToTheModel(t *testing.T) {
	a, srv := newTestAssistant(t, false)
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

// TestHostFailoverRetriesOnTheFallbackHost covers the v2.0 multi-host retry: a dead primary host
// makes the turn switch to the pool's fallback and answer from there.
func TestHostFailoverRetriesOnTheFallbackHost(t *testing.T) {
	backup := ollamatest.New(t)
	backup.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}}}
	backup.Turns = []ollamatest.Turn{{Content: "Answered by the fallback."}}

	cfg := provider.NewHostsConfig()
	cfg.DefaultHost = "primary"
	cfg.Hosts["primary"] = provider.HostConfig{Name: "primary", URL: deadHost, Vendor: "ollama", Fallback: "backup"}
	cfg.Hosts["backup"] = provider.HostConfig{Name: "backup", URL: backup.URL, Vendor: "ollama"}
	pool := provider.NewHostPool(cfg)
	if err := pool.Connect(gocontext.Background(), "backup"); err != nil {
		t.Fatalf("connect backup: %v", err)
	}

	a := newForTest(t.TempDir(), "gemma4:12b", deadHost, false)
	a.permMgr = permissions.NewManagerAllowAll()
	a.SetHostPool(pool)

	if err := a.ProcessMessage("anyone home?"); err != nil {
		t.Fatal(err)
	}

	if a.GetLastResponse() != "Answered by the fallback." {
		t.Fatalf("last response = %q", a.GetLastResponse())
	}
	if n := countPath(backup, "/api/chat"); n != 1 {
		t.Fatalf("fallback host saw %d chat requests, want 1", n)
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
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "hello.txt"}}}},
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

// deadHost is a loopback port nothing listens on, so requests fail with "connection refused".
const deadHost = "http://127.0.0.1:1"

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
