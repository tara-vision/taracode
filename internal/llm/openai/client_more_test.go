package openaiclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// sseAdapter is an adapter on a server that streams chunks as server-sent events, then [DONE].
func sseAdapter(t *testing.T, chunks ...string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = fmt.Fprint(w, "data: "+c+"\n\n")
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return newAdapter(srv)
}

func failingAdapter(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"error":{"message":"engine down","type":"server_error"}}`)
	}))
	t.Cleanup(srv.Close)
	return newAdapter(srv)
}

var hi = []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}}

func ignoreEvents(llm.Event) error { return nil }

func TestRequestCarriesTemperatureAndTopP(t *testing.T) {
	srv, reqs := fakeOpenAI(t)
	temp, topP := float32(0.25), float32(0.5)
	if _, err := newAdapter(srv).Chat(context.Background(), llm.Request{Model: "m", Messages: hi,
		Options: llm.Options{Temperature: &temp, TopP: &topP}}, nil); err != nil {
		t.Fatal(err)
	}
	if body := (*reqs)[0]; body["temperature"] != 0.25 || body["top_p"] != 0.5 {
		t.Fatalf("%v", body)
	}
}

func TestServerFailuresAreErrors(t *testing.T) {
	c := failingAdapter(t)
	if _, err := c.Chat(context.Background(), llm.Request{Model: "m", Messages: hi}, nil); err == nil ||
		!strings.HasPrefix(err.Error(), "openai: ") || !strings.Contains(err.Error(), "engine down") {
		t.Fatalf("non-stream: %v", err)
	}
	if _, err := c.Chat(context.Background(), llm.Request{Model: "m", Messages: hi}, ignoreEvents); err == nil ||
		!strings.Contains(err.Error(), "engine down") {
		t.Fatalf("stream: %v", err)
	}
	if _, err := c.Models(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "openai: list models: ") {
		t.Fatalf("models: %v", err)
	}
	broken := sseAdapter(t, `{"choices":[{"delta":{"content":"a"}}]`)
	if _, err := broken.Chat(context.Background(), llm.Request{Model: "m", Messages: hi}, ignoreEvents); err == nil ||
		!strings.HasPrefix(err.Error(), "openai: stream: ") {
		t.Fatalf("a broken chunk: %v", err)
	}
}

// TestToolCallFragmentsMergeLateIDsAndNames: a fragment may carry the id or the name after the first
// one; a call without a type is a function call.
func TestToolCallFragmentsMergeLateIDsAndNames(t *testing.T) {
	c := sseAdapter(t,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_late","function":{"name":"read_file","arguments":"th\":\"a\"}"}}]}}]}`,
	)
	res, err := c.Chat(context.Background(), llm.Request{Model: "m", Messages: hi}, ignoreEvents)
	if err != nil || len(res.ToolCalls) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	call := res.ToolCalls[0]
	if call.ID != "call_late" || call.Function.Name != "read_file" || call.Function.Arguments != `{"path":"a"}` ||
		call.Type != openai.ToolTypeFunction {
		t.Fatalf("%+v", call)
	}
}

// TestStreamStopsOnTheCallersError: an error the callback returns for text, a tool call or the usage
// ends the turn with it.
func TestStreamStopsOnTheCallersError(t *testing.T) {
	chunks := []string{
		`{"choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"x","arguments":"{}"}}]}}]}`,
	}
	stop := errors.New("stop")
	for _, kind := range []llm.EventKind{llm.EventText, llm.EventToolCall, llm.EventUsage} {
		_, err := sseAdapter(t, chunks...).Chat(context.Background(), llm.Request{Model: "m", Messages: hi}, func(e llm.Event) error {
			if e.Kind == kind {
				return stop
			}
			return nil
		})
		if !errors.Is(err, stop) {
			t.Errorf("kind %v: %v", kind, err)
		}
	}
}

func TestToRequestLeavesUnsetOptionsOut(t *testing.T) {
	out := toRequest(llm.Request{Model: "m", Options: llm.Options{Think: llm.ThinkOn}}, false)
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"temperature", "top_p", "max_tokens", "reasoning_effort", "stream_options"} {
		if strings.Contains(string(data), `"`+absent+`"`) {
			t.Errorf("%s is set: %s", absent, data)
		}
	}
}
