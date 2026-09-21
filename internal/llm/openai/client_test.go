package openaiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// fakeOpenAI answers /v1/chat/completions (stream and non-stream) and /v1/models.
func fakeOpenAI(t *testing.T) (*httptest.Server, *[]map[string]any) {
	var requests []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": "qwen3.8:27b", "object": "model"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests = append(requests, req)
		if stream, _ := req["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hel"}}]}`+"\n\n")
			_, _ = fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo"}}]}`+"\n\n")
			_, _ = fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"file_path\":"}}]}}]}`+"\n\n")
			_, _ = fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
			_, _ = fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`+"\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "object": "chat.completion", "choices": []map[string]any{{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "42"}}}, "usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 1, "total_tokens": 4}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &requests
}

func newAdapter(srv *httptest.Server) *Client {
	cfg := openai.DefaultConfig("")
	cfg.BaseURL = srv.URL + "/v1"
	return New(openai.NewClientWithConfig(cfg))
}

func TestStreamAssemblesTextToolCallsAndUsage(t *testing.T) {
	srv, reqs := fakeOpenAI(t)
	c := newAdapter(srv)
	var text string
	var calls []openai.ToolCall
	res, err := c.Chat(context.Background(), llm.Request{Model: "m", Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}}, Options: llm.Options{Think: llm.ThinkHigh, NumPredict: 50}}, func(e llm.Event) error {
		if e.Kind == llm.EventText {
			text += e.Text
		}
		if e.Kind == llm.EventToolCall {
			calls = append(calls, *e.ToolCall)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" || res.Content != "hello" || len(calls) != 1 || calls[0].ID != "call_a" || calls[0].Function.Arguments != `{"file_path":"a.go"}` {
		t.Fatalf("text=%q calls=%+v", text, calls)
	}
	if res.Usage.PromptTokens != 9 || res.DoneReason != "tool_calls" {
		t.Fatalf("res=%+v", res)
	}
	body := (*reqs)[0]
	if body["reasoning_effort"] != "high" || body["max_tokens"] != float64(50) || body["stream_options"] == nil {
		t.Fatalf("request options not mapped: %v", body)
	}
}

func TestNonStreamAndUnsupportedCalls(t *testing.T) {
	srv, _ := fakeOpenAI(t)
	c := newAdapter(srv)
	res, err := c.Chat(context.Background(), llm.Request{Model: "m"}, nil)
	if err != nil || res.Content != "42" || res.Usage.CompletionTokens != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	models, err := c.Models(context.Background())
	if err != nil || len(models) != 1 || models[0].Name != "qwen3.8:27b" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if _, err := c.Show(context.Background(), "m"); !errors.Is(err, llm.ErrNotSupported) {
		t.Fatalf("Show should be unsupported, got %v", err)
	}
	if _, err := c.Loaded(context.Background()); !errors.Is(err, llm.ErrNotSupported) {
		t.Fatalf("Loaded should be unsupported, got %v", err)
	}
	if v, err := c.Version(context.Background()); err != nil || v != "" {
		t.Fatalf("Version should be empty and nil, got %q %v", v, err)
	}
	if err := c.Unload(context.Background(), "m"); !errors.Is(err, llm.ErrNotSupported) {
		t.Fatalf("Unload should be unsupported, got %v", err)
	}
}
