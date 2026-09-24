package agent

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// chatBodies are the bodies of the /api/chat requests the fake served, in order.
func chatBodies(srv *ollamatest.Server) []map[string]any {
	var out []map[string]any
	for _, r := range srv.Requests {
		if r.Path == "/api/chat" {
			out = append(out, r.Body)
		}
	}
	return out
}

// carriesNudge reports whether a request's messages include the cap nudge.
func carriesNudge(body map[string]any) bool {
	messages, _ := body["messages"].([]any)
	for _, m := range messages {
		if msg, _ := m.(map[string]any); msg["content"] == capNudge {
			return true
		}
	}
	return false
}

// storesNudge reports whether the assistant's own history holds the cap nudge.
func storesNudge(a *Assistant) bool {
	for _, m := range a.conversation {
		if m.Content == capNudge {
			return true
		}
	}
	return false
}

// TestTheCapAnswersWithTheFindingsSoFar pins rulings P3-R60 and P3-R63, headlessly and in both
// streaming modes: a model that calls a tool on every turn, for one turn more than the cap, gets one
// final completion with no tools offered and the nudge, whose text is the turn's answer. The tool
// call it makes there is not run, the nudge is never stored, and the next turn's request carries none.
func TestTheCapAnswersWithTheFindingsSoFar(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		a, srv := newTestAssistant(t, streaming)
		var out bytes.Buffer
		a.out = &out
		a.maxIterations = 2
		var events []ToolEvent
		a.observer = func(ev ToolEvent) { events = append(events, ev) }
		read := ollamatest.ToolCall{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}
		srv.Turns = []ollamatest.Turn{
			{ToolCalls: []ollamatest.ToolCall{read}},
			{ToolCalls: []ollamatest.ToolCall{read}},
			{Content: "So far: hello.txt says hello from disk.", ToolCalls: []ollamatest.ToolCall{read}},
			{Content: "Next turn."},
		}
		if err := a.ProcessMessage("what does hello.txt say?"); err != nil {
			t.Fatal(err)
		}
		if got := a.GetLastResponse(); got != "So far: hello.txt says hello from disk." {
			t.Fatalf("streaming=%v: answer %q", streaming, got)
		}
		if st := a.LastTurn(); !st.Truncated || st.Completions != 3 || st.ToolCalls != 2 || len(events) != 2 {
			t.Fatalf("streaming=%v: turn %+v, events %d", streaming, st, len(events))
		}
		chats := chatBodies(srv)
		if _, offered := chats[1]["tools"]; !offered {
			t.Fatalf("streaming=%v: the requests before the cap must offer the tools", streaming)
		}
		if _, offered := chats[2]["tools"]; offered || !carriesNudge(chats[2]) {
			t.Fatalf("streaming=%v: the final request offered tools or lacks the nudge: %v", streaming, chats[2])
		}
		last := a.conversation[len(a.conversation)-1]
		if last.Role != openai.ChatMessageRoleAssistant || len(last.ToolCalls) != 0 || storesNudge(a) ||
			!strings.Contains(out.String(), "Reached the limit of 2 tool iterations; answering with the findings so far (context.max_tool_iterations)") {
			t.Fatalf("streaming=%v: last message %+v, output %q", streaming, last, out.String())
		}
		if err := a.ProcessMessage("and now?"); err != nil {
			t.Fatal(err)
		}
		if next := chatBodies(srv)[3]; carriesNudge(next) {
			t.Fatalf("streaming=%v: the next turn's request carries the nudge", streaming)
		}
	}
}

// TestTheCapWarnsWhenTheFinalCompletionFails: an engine error on the final completion is reported
// once, the turn stays truncated with no answer and no error, and neither the history nor the next
// turn's request is left with the nudge (ruling P3-R63).
func TestTheCapWarnsWhenTheFinalCompletionFails(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	var out bytes.Buffer
	a.out = &out
	a.maxIterations = 2
	call := ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}}
	srv.Turns = []ollamatest.Turn{call, call, {Status: http.StatusInternalServerError, Error: "the model crashed"},
		{Content: "Next turn."}}
	if err := a.ProcessMessage("loop"); err != nil {
		t.Fatalf("a failed final completion must not fail the turn: %v", err)
	}
	if st := a.LastTurn(); !st.Truncated || a.GetLastResponse() != "" ||
		!strings.Contains(out.String(), "No answer at the iteration cap:") || storesNudge(a) {
		t.Fatalf("turn %+v, answer %q, output %q", st, a.GetLastResponse(), out.String())
	}
	if last := a.conversation[len(a.conversation)-1]; last.Role != openai.ChatMessageRoleTool {
		t.Fatalf("the history must end with the last tool result, not %q", last.Role)
	}
	if err := a.ProcessMessage("and now?"); err != nil {
		t.Fatal(err)
	}
	if next := chatBodies(srv)[3]; carriesNudge(next) {
		t.Fatal("the next turn's request carries the nudge")
	}
}

// TestTheCapReturnsAContextError: a turn whose context ends by the cap makes no final completion and
// returns the context's error.
func TestTheCapReturnsAContextError(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	var out bytes.Buffer
	a.out = &out
	a.maxIterations = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.observer = func(ToolEvent) { cancel() }
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{Content: "never asked"},
	}
	if err := a.ProcessMessageContext(ctx, "read"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if n := len(chatBodies(srv)); n != 1 || !a.LastTurn().Truncated {
		t.Fatalf("%d chat requests, turn %+v", n, a.LastTurn())
	}
}

// TestTheCapWarnsOnAnEmptyAnswer: a final completion with no text, only a tool call or only reasoning,
// is reported instead of passed over in silence, and nothing is stored for it (ruling P3-R63).
func TestTheCapWarnsOnAnEmptyAnswer(t *testing.T) {
	read := ollamatest.ToolCall{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}
	for name, final := range map[string]ollamatest.Turn{
		"a tool call only": {ToolCalls: []ollamatest.ToolCall{read}},
		"reasoning only":   {Thinking: "I would read it again."},
	} {
		a, srv := newTestAssistant(t, false)
		var out bytes.Buffer
		a.out = &out
		a.maxIterations = 1
		srv.Turns = []ollamatest.Turn{{ToolCalls: []ollamatest.ToolCall{read}}, final}
		if err := a.ProcessMessage("read"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		last := a.conversation[len(a.conversation)-1]
		if a.GetLastResponse() != "" || last.Role != openai.ChatMessageRoleTool ||
			!strings.Contains(out.String(), "No answer at the iteration cap: the model returned no text") {
			t.Fatalf("%s: answer %q, last %q, output %q", name, a.GetLastResponse(), last.Role, out.String())
		}
	}
}
