package agent

import (
	"bytes"
	gocontext "context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestProcessMessageWithImagesSendsTheImages(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{{Content: "A diagram."}}
	img := &ImageData{Path: "pic.png", MimeType: "image/png", Base64: "cG5n"}
	_ = captureStdout(t, func() {
		if err := a.ProcessMessageWithImages("what is this", []*ImageData{img}); err != nil {
			t.Error(err)
		}
	})
	last := lastMessage(t, lastChatBody(t, srv), 0)
	images, _ := last["images"].([]any)
	if last["content"] != "what is this" || len(images) != 1 || images[0] != "cG5n" {
		t.Fatalf("the user message sent %v", last)
	}
	user := findMessage(t, a.conversation, openai.ChatMessageRoleUser)
	if len(user.MultiContent) != 2 || user.MultiContent[0].Text != "what is this" ||
		user.MultiContent[1].ImageURL.URL != "data:image/png;base64,cG5n" || user.MultiContent[1].ImageURL.Detail != openai.ImageURLDetailAuto {
		t.Fatalf("the stored message %+v", user)
	}
}

func TestToolCallsOfIgnoresArgumentsThatAreNotJSON(t *testing.T) {
	calls, display := toolCallsOf(&llm.Result{Content: "checking", ToolCalls: []openai.ToolCall{
		{ID: "1", Function: openai.FunctionCall{Name: "read_file", Arguments: "not json"}},
		{ID: "2", Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"src"}`}},
	}})
	if len(calls) != 2 || len(calls[0].Params) != 0 || calls[1].Params["path"] != "src" || display != "checking" {
		t.Fatalf("calls %+v display %q", calls, display)
	}
}

// TestToolSpinnersNumberTheCallsOfAReply: with spinners on, a reply with two calls shows each with
// its position.
func TestToolSpinnersNumberTheCallsOfAReply(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.enableSpinner = true
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}},
			{Name: "list_files", Args: map[string]any{}}}},
		{Content: "Done."},
	}
	out := captureStdout(t, func() {
		if err := a.ProcessMessage("look around"); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{"Running read_file (1/2)...", "Running list_files (2/2)..."} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

// TestATurnCompactsAConversationOverTheBudget: with compaction on and the budget tiny, the turn
// summarises the older messages before it asks the model, and says so.
func TestATurnCompactsAConversationOverTheBudget(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.compactionCfg = CompactionConfig{Enabled: true, Threshold: 0.5, KeepRecent: 1, MaxTokens: 100}
	for i := 0; i < 3; i++ {
		a.conversation = append(a.conversation,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "question"},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "answer"})
	}
	srv.Turns = []ollamatest.Turn{{Content: "They asked three questions."}, {Content: "Here you go."}}
	out := captureStdout(t, func() {
		if err := a.ProcessMessage("and one more"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "Context compacted: ") || len(a.compactionState.Events) == 0 {
		t.Fatalf("output %q, events %+v", out, a.compactionState.Events)
	}
	if !strings.Contains(a.conversation[1].Content, "[Session context (compacted): They asked three questions.]") {
		t.Fatalf("the summary replaces the old messages: %+v", a.conversation[1])
	}
}

func TestGenerateSummaryRejectsAnEmptyAnswer(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Turns = []ollamatest.Turn{{Content: "   "}}
	a := newForTest(t.TempDir(), "gemma4:12b", srv.URL, false)
	_, err := generateSummary(gocontext.Background(), []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleTool, Content: strings.Repeat("t", 150)},
	}, a.llm, "gemma4:12b", llm.Options{})
	if err == nil || err.Error() != "empty summary response" {
		t.Fatalf("err = %v", err)
	}
	if prompt := lastMessage(t, lastChatBody(t, srv), 0)["content"].(string); !strings.Contains(prompt,
		"Tool result: "+strings.Repeat("t", 100)+"...") {
		t.Fatalf("a tool result is cut to 100 characters: %q", prompt)
	}
	if _, err := generateSummary(gocontext.Background(), nil, nil, "m", llm.Options{}); err == nil {
		t.Fatal("no client, no summary")
	}
}

func TestCompactConversationLeavesAShortConversation(t *testing.T) {
	short := []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: "s"}}
	out, event, err := CompactConversation(gocontext.Background(), short, nil, CompactionConfig{KeepRecent: 4}, nil, "m",
		llm.Options{})
	if err != nil || event != nil || len(out) != 1 {
		t.Fatalf("%v %v %v", out, event, err)
	}
}

func TestBuildFallbackSummaryCountsInTheSingular(t *testing.T) {
	got := buildFallbackSummary([]openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: "check the pods\nthen the nodes"},
		{Role: openai.ChatMessageRoleTool, Content: "pods"},
	})
	if got != "[Session context (compacted): 2 messages summarized, 1 user query, 1 tool call. Topics: check the pods]" {
		t.Fatalf("%q", got)
	}
	tool := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleTool, Content: "pods"}
	if got := buildFallbackSummary([]openai.ChatCompletionMessage{tool, tool}); got !=
		"[Session context (compacted): 2 messages summarized, 2 tool calls]" {
		t.Fatalf("%q", got)
	}
}

func TestTruncateRuneSafeAndToolDefinitions(t *testing.T) {
	if got := truncateRuneSafe("h\u00e9llo w\u00f6rld", 5); got != "h\u00e9llo..." {
		t.Fatalf("%q", got)
	}
	defs := []openai.Tool{{Type: openai.ToolTypeFunction}, {Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{Name: "abcd", Description: "efgh"}}}
	if got := EstimateToolDefsTokens(defs); got != 2 {
		t.Fatalf("a definition without a function is skipped: %d", got)
	}
}

func TestLoadImageRefusesAFileOver20MB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(20*1024*1024 + 1); err != nil { // sparse: nothing is written
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImage(path); err == nil || err.Error() != "image "+path+" is too large (max 20MB)" {
		t.Fatalf("err = %v", err)
	}
}

// cancelAtCap is the fake's client, except that the final request at the iteration cap is never
// answered: the caller cancels the turn while it is in flight.
type cancelAtCap struct {
	llm.Client
	cancel gocontext.CancelFunc
}

func (c cancelAtCap) Chat(ctx gocontext.Context, req llm.Request, onEvent func(llm.Event) error) (*llm.Result, error) {
	if req.Messages[len(req.Messages)-1].Content == capNudge {
		c.cancel()
		return nil, errors.New("request cancelled")
	}
	return c.Client.Chat(ctx, req, onEvent)
}

// TestACancelledTurnAtTheCapReturnsTheCancellation: when the turn is cancelled during the final
// request at the iteration cap, the turn ends with the cancellation, not a warning about the
// missing answer.
func TestACancelledTurnAtTheCapReturnsTheCancellation(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.maxIterations = 1
	srv.Turns = []ollamatest.Turn{toolCall("list_files", map[string]any{})}
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	defer cancel()
	a.llm = cancelAtCap{Client: a.llm, cancel: cancel}
	var out bytes.Buffer
	a.out = &out
	if err := a.ProcessMessageContext(ctx, "look around"); !errors.Is(err, gocontext.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out.String(), "No answer at the iteration cap") || a.GetLastResponse() != "" {
		t.Fatalf("output %q, answer %q", out.String(), a.GetLastResponse())
	}
}
