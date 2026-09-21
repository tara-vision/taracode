package assistant

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestGenerateSummaryGoesThroughTheClientAndIsStored(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	a.storage, a.session = store, session
	a.session.Messages = []storage.ConversationMessage{
		{Role: "user", Content: "deploy the app"},
		{Role: "assistant", Content: "deployed"},
	}
	srv.Turns = []ollamatest.Turn{{Content: "The app was deployed."}}

	summary, err := a.GenerateSummary()
	if err != nil {
		t.Fatal(err)
	}

	if summary != "The app was deployed." {
		t.Fatalf("summary = %q", summary)
	}
	if a.session.Summary != summary {
		t.Fatalf("summary not stored on the session: %q", a.session.Summary)
	}
	if a.sessionUsage.TotalTokens == 0 {
		t.Fatal("usage was not estimated for a reply without token counts")
	}
	options, _ := lastChatBody(t, srv)["options"].(map[string]any)
	if options["num_predict"] != float64(100) {
		t.Fatalf("summary request options = %v", options)
	}
}

func TestGenerateSummarySkipsShortSessions(t *testing.T) {
	a, _ := newTestAssistant(t, false)

	summary, err := a.GenerateSummary()

	if err != nil || summary != "" {
		t.Fatalf("summary = %q, err = %v", summary, err)
	}
}

// TestLoadSessionRebuildsToolCallRoundTrip covers NewSession followed by a turn with a tool call,
// then LoadSession: the reloaded conversation must carry the same tool call and its result
// (convertToOpenAIToolCalls round trip), read back from disk through real storage.
func TestLoadSessionRebuildsToolCallRoundTrip(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	a.storage = store

	if err := a.NewSession("x"); err != nil {
		t.Fatal(err)
	}
	id := a.session.ID

	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "hello.txt"}}}},
		{Content: "The file says hello from disk."},
	}
	if err := a.ProcessMessage("what does hello.txt say?"); err != nil {
		t.Fatal(err)
	}

	if err := a.LoadSession(id); err != nil {
		t.Fatal(err)
	}

	asst := findMessage(t, a.conversation, openai.ChatMessageRoleAssistant)
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("tool call not restored on the assistant message: %+v", asst)
	}
	tool := findMessage(t, a.conversation, openai.ChatMessageRoleTool)
	if tool.ToolCallID != asst.ToolCalls[0].ID {
		t.Fatalf("tool result id mismatch: tool=%q assistant=%q", tool.ToolCallID, asst.ToolCalls[0].ID)
	}
	if !strings.Contains(tool.Content, "hello from disk") {
		t.Fatalf("tool result content not restored: %+v", tool)
	}
}

// TestTurnRecordsUserAssistantAndToolMessages covers what a turn with a tool call writes to a
// real session: the user message, the assistant message carrying a tool call record, and one
// record per tool result.
func TestTurnRecordsUserAssistantAndToolMessages(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	a.storage, a.session = store, session
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "hello.txt"}}}},
		{Content: "It says hello from disk."},
	}

	if err := a.ProcessMessage("what does hello.txt say?"); err != nil {
		t.Fatal(err)
	}

	saved, err := store.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawUser, sawAssistantWithToolCall, sawToolResult bool
	for _, m := range saved.Messages {
		switch m.Role {
		case "user":
			if m.Content == "what does hello.txt say?" {
				sawUser = true
			}
		case "assistant":
			if len(m.ToolCalls) == 1 && m.ToolCalls[0].Tool == "read_file" {
				sawAssistantWithToolCall = true
			}
		case "tool":
			if m.ToolCall != nil && m.ToolCall.Tool == "read_file" && strings.Contains(m.ToolCall.Result, "hello from disk") {
				sawToolResult = true
			}
		}
	}
	if !sawUser {
		t.Errorf("no recorded user message: %+v", saved.Messages)
	}
	if !sawAssistantWithToolCall {
		t.Errorf("no recorded assistant message with a tool call record: %+v", saved.Messages)
	}
	if !sawToolResult {
		t.Errorf("no recorded tool result: %+v", saved.Messages)
	}
}
