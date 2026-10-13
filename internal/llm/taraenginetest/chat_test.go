package taraenginetest

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// events splits an SSE body into its data payloads, [DONE] included.
func events(body []byte) []string {
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			out = append(out, data)
		}
	}
	return out
}

func TestChatStreamsAScriptedTurn(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{Thinking: "let me see", Content: "two pods", PromptTokens: 46, CompletionTokens: 12,
		PromptEvalDuration: 5267 * time.Microsecond, EvalDuration: 3095 * time.Microsecond,
		ToolCalls: []ToolCall{{Name: "list_files", Args: map[string]any{"path": "."}}, {Name: "noop"}}}}
	status, header, body := call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"gemma4:12b","stream":true}`)
	if status != http.StatusOK || header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content type %q", status, header.Get("Content-Type"))
	}
	got := events(body)
	if got[len(got)-1] != "[DONE]" {
		t.Fatalf("the stream ends with [DONE]: %v", got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{`"reasoning_content":"let `, `"content":"two `, `"name":"list_files"`,
		`"arguments":"{\"path"`, `"finish_reason":"tool_calls"`, `"choices":[]`, `"prompt_ms":5.267`,
		`"predicted_ms":3.095`, `"prompt_tokens":46`, `"name":"noop"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("the stream lacks %s:\n%s", want, joined)
		}
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(got[len(got)-2]), &last); err != nil || last["timings"] == nil {
		t.Fatalf("the chunk before [DONE] carries the timings: %v %v", last, err)
	}
}

// TestChatNumbersToolCallsAcrossTheTurns: a real server gives every call an id of its own, so the ids run on from
// one turn to the next and a conversation that replays several replies never holds two calls with one id.
func TestChatNumbersToolCallsAcrossTheTurns(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{
		{ToolCalls: []ToolCall{{Name: "a"}, {Name: "b"}}},
		{Content: "no calls"},
		{ToolCalls: []ToolCall{{Name: "c"}}},
	}
	callID := regexp.MustCompile(`"id":"(call_\d+)"`)
	var got []string
	for range srv.Turns {
		_, _, body := call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"m","stream":true}`)
		for _, m := range callID.FindAllStringSubmatch(string(body), -1) {
			got = append(got, m[1])
		}
	}
	if want := []string{"call_0", "call_1", "call_2"}; !slices.Equal(got, want) {
		t.Fatalf("tool call ids %v, want %v", got, want)
	}
}

func TestChatFinishReasons(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{Content: "hi"}, {Content: "cut off", DoneReason: "length"}}
	for _, want := range []string{`"finish_reason":"stop"`, `"finish_reason":"length"`} {
		_, _, body := call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"m","stream":true}`)
		if !strings.Contains(string(body), want) {
			t.Fatalf("want %s in %s", want, body)
		}
	}
}

func TestChatScriptsErrorsAndCuts(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{
		{Status: http.StatusNotFound, Error: `model "x" is not a pulled model`, Hint: "taraengine list shows the models"},
		{Status: http.StatusServiceUnavailable, Error: "the engine backend is not running"},
		{Content: "half an answer", Cut: true},
	}
	status, _, body := call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"x"}`)
	if status != http.StatusNotFound || !strings.Contains(string(body), `"hint":"taraengine list shows the models"`) {
		t.Fatalf("a scripted refusal: %d %s", status, body)
	}
	status, header, _ := call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"x"}`)
	if status != http.StatusServiceUnavailable || header.Get("Retry-After") != "2" {
		t.Fatalf("a scripted 503: %d %q", status, header.Get("Retry-After"))
	}
	_, _, body = call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"x"}`)
	if got := events(body); strings.Contains(string(body), "[DONE]") || strings.Contains(string(body), "finish_reason\":\"") ||
		len(got) == 0 {
		t.Fatalf("a cut stream has the deltas and nothing after them: %s", body)
	}
	status, _, body = call(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"x"}`)
	if status != http.StatusInternalServerError || !strings.Contains(string(body), "no scripted turn left") {
		t.Fatalf("no turn left: %d %s", status, body)
	}
}
