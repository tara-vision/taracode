package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func newClient(t *testing.T) (*ollamatest.Server, *Client) {
	srv := ollamatest.New(t)
	return srv, New(srv.URL, http.DefaultClient)
}

func TestChatStreamsTextThinkingToolCallsAndUsage(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{Content: "Reading the file.", Thinking: "plan first", ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "main.go"}}}, PromptTokens: 20, CompletionTokens: 7}}

	var text, thinking string
	var calls []openai.ToolCall
	var usage *llm.Usage
	res, err := c.Chat(context.Background(), llm.Request{Model: "gemma4:12b", Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "read main.go"}}}, func(e llm.Event) error {
		switch e.Kind {
		case llm.EventText:
			text += e.Text
		case llm.EventThinking:
			thinking += e.Text
		case llm.EventToolCall:
			calls = append(calls, *e.ToolCall)
		case llm.EventUsage:
			usage = e.Usage
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "Reading the file." || thinking != "plan first" || res.Content != text || res.Thinking != thinking {
		t.Fatalf("text=%q thinking=%q res=%+v", text, thinking, res)
	}
	if len(calls) != 1 || calls[0].Function.Name != "read_file" || calls[0].ID == "" || !strings.Contains(calls[0].Function.Arguments, `"file_path":"main.go"`) {
		t.Fatalf("tool calls: %+v", calls)
	}
	if usage == nil || usage.PromptTokens != 20 || res.Usage.CompletionTokens != 7 || res.DoneReason != "stop" {
		t.Fatalf("usage=%v res=%+v", usage, res)
	}
}

func TestChatNonStreamAssemblesResult(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{Content: "42", PromptTokens: 5, CompletionTokens: 1}}
	res, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err != nil || res.Content != "42" || res.Usage.PromptTokens != 5 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if srv.Requests[0].Body["stream"] != false {
		t.Fatalf("non-stream request should send stream:false, got %v", srv.Requests[0].Body["stream"])
	}
}

func TestRequestCarriesOptionsToolsAndConvertedMessages(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{Content: "ok"}}
	temp := float32(0.2)
	req := llm.Request{
		Model: "m",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "sys"},
			{Role: openai.ChatMessageRoleUser, MultiContent: []openai.ChatMessagePart{
				{Type: openai.ChatMessagePartTypeText, Text: "look"},
				{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &openai.ChatMessageImageURL{URL: "data:image/png;base64,AAAA"}},
			}},
			{Role: openai.ChatMessageRoleAssistant, ToolCalls: []openai.ToolCall{{ID: "call_1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "read_file", Arguments: `{"file_path":"a"}`}}}},
			{Role: openai.ChatMessageRoleTool, ToolCallID: "call_1", Content: "file body"},
		},
		Tools:   []openai.Tool{{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "read_file", Description: "Read", Parameters: map[string]any{"type": "object"}}}},
		Options: llm.Options{NumCtx: 32768, KeepAlive: "-1", Think: llm.ThinkLow, Format: json.RawMessage(`{"type":"object"}`), Temperature: &temp, NumPredict: 64},
	}
	if _, err := c.Chat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	body := srv.Requests[0].Body
	opts := body["options"].(map[string]any)
	if opts["num_ctx"] != float64(32768) || opts["temperature"] != float64(temp) || opts["num_predict"] != float64(64) || body["keep_alive"] != "-1" || body["think"] != "low" {
		t.Fatalf("options not sent: %v", body)
	}
	if _, ok := body["format"].(map[string]any); !ok {
		t.Fatalf("format schema not sent: %v", body["format"])
	}
	msgs := body["messages"].([]any)
	user := msgs[1].(map[string]any)
	if user["content"] != "look" || user["images"].([]any)[0] != "AAAA" {
		t.Fatalf("image message not converted: %v", user)
	}
	asst := msgs[2].(map[string]any)
	fn := asst["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read_file" || fn["arguments"].(map[string]any)["file_path"] != "a" {
		t.Fatalf("assistant tool call not converted: %v", asst)
	}
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["content"] != "file body" || tool["tool_name"] != "read_file" {
		t.Fatalf("tool message not converted: %v", tool)
	}
	if body["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"] != "read_file" {
		t.Fatalf("tools not sent: %v", body["tools"])
	}
}

func TestThinkMapping(t *testing.T) {
	cases := map[llm.Think]any{llm.ThinkAuto: nil, llm.ThinkOff: false, llm.ThinkOn: true, llm.ThinkHigh: "high"}
	for think, want := range cases {
		srv, c := newClient(t)
		srv.Turns = []ollamatest.Turn{{Content: "x"}}
		_, _ = c.Chat(context.Background(), llm.Request{Model: "m", Options: llm.Options{Think: think}}, nil)
		got, present := srv.Requests[0].Body["think"]
		if want == nil && present {
			t.Fatalf("think %q should be omitted, sent %v", think, got)
		}
		if want != nil && got != want {
			t.Fatalf("think %q: sent %v want %v", think, got, want)
		}
	}
}

// TestToolCallIDsAreUniqueAcrossChatCalls covers the fix for ids restarting at call_0 on every
// reply: two consecutive Chat calls against the same client must not hand out the same id, or a
// restored session's replayed tool results get routed to the wrong tool_name.
func TestToolCallIDsAreUniqueAcrossChatCalls(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "a"}}}},
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "b"}}}},
	}
	res1, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.ToolCalls) != 1 || len(res2.ToolCalls) != 1 {
		t.Fatalf("tool calls: %+v %+v", res1.ToolCalls, res2.ToolCalls)
	}
	if res1.ToolCalls[0].ID == "" || res1.ToolCalls[0].ID == res2.ToolCalls[0].ID {
		t.Fatalf("tool call ids collided across two Chat calls: %q vs %q", res1.ToolCalls[0].ID, res2.ToolCalls[0].ID)
	}
}

// TestToolCallWithNoArgumentsDefaultsToAnEmptyObject covers fromWireToolCall's empty-arguments
// branch: Ollama sends a null "arguments" value for a tool call with no parameters, which must
// still decode as valid JSON downstream rather than being left empty.
func TestToolCallWithNoArgumentsDefaultsToAnEmptyObject(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{ToolCalls: []ollamatest.ToolCall{{Name: "get_datetime", Args: nil}}}}
	res, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Function.Arguments != "{}" {
		t.Fatalf("tool calls: %+v", res.ToolCalls)
	}
}

func TestServerErrorIsReturned(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{Status: 400, Error: "model does not support tools"}}
	_, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support tools") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err=%v", err)
	}
}

func TestModelsShowLoadedVersionUnload(t *testing.T) {
	srv, c := newClient(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools", "thinking"}, ContextLength: 262144, Family: "gemma4", ParameterSize: "12B", Size: 7_600_000_000}}
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, SizeVRAM: 9_000_000_000}}
	ctx := context.Background()

	models, err := c.Models(ctx)
	if err != nil || len(models) != 1 || models[0].Name != "gemma4:12b" || models[0].Family != "gemma4" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	details, err := c.Show(ctx, "gemma4:12b")
	if err != nil || !details.Has("tools") || details.ContextLength != 262144 {
		t.Fatalf("details=%+v err=%v", details, err)
	}
	if _, err := c.Show(ctx, "missing"); err == nil {
		t.Fatal("expected an error for an unknown model")
	}
	loaded, err := c.Loaded(ctx)
	if err != nil || loaded[0].ContextLength != 32768 {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	if v, err := c.Version(ctx); err != nil || v != "0.34.2" {
		t.Fatalf("version=%q err=%v", v, err)
	}
	if err := c.Unload(ctx, "gemma4:12b"); err != nil || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("unload err=%v recorded=%v", err, srv.Unloaded)
	}
}

func TestChatContextCancel(t *testing.T) {
	srv, c := newClient(t)
	srv.Turns = []ollamatest.Turn{{Content: "slow"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Chat(ctx, llm.Request{Model: "m"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
