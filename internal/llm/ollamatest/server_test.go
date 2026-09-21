package ollamatest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestChatStreamsScriptedTurn(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{Content: "hello world", Thinking: "hmm", ToolCalls: []ToolCall{{Name: "read_file", Args: map[string]any{"file_path": "a.go"}}}, PromptTokens: 12, CompletionTokens: 3}}

	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"options":{"num_ctx":4096},"think":true}`
	resp, err := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var lines []map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad ndjson line %q: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(lines))
	}
	last := lines[len(lines)-1]
	if last["done"] != true || last["prompt_eval_count"].(float64) != 12 {
		t.Fatalf("bad final chunk: %v", last)
	}
	var text, thinking string
	var calls int
	for _, m := range lines {
		msg, _ := m["message"].(map[string]any)
		if msg == nil {
			continue
		}
		text += msg["content"].(string)
		thinking += msg["thinking"].(string)
		if tc, ok := msg["tool_calls"].([]any); ok {
			calls += len(tc)
		}
	}
	if text != "hello world" || thinking != "hmm" || calls != 1 {
		t.Fatalf("assembled text=%q thinking=%q calls=%d", text, thinking, calls)
	}
	if got := srv.Requests[0].Body["options"].(map[string]any)["num_ctx"]; got != float64(4096) {
		t.Fatalf("request not recorded: %v", srv.Requests[0].Body)
	}
}

func TestChatNonStreamAndExhaustion(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{Content: "once"}}
	resp, _ := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(`{"model":"m","messages":[],"stream":false}`))
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	if m["message"].(map[string]any)["content"] != "once" || m["done"] != true {
		t.Fatalf("bad reply: %v", m)
	}
	resp, _ = http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(`{"model":"m","messages":[],"stream":false}`))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("exhausted script should answer 500, got %d", resp.StatusCode)
	}
}

func TestModelEndpoints(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools", "thinking", "vision"}, ContextLength: 262144, Family: "gemma4", ParameterSize: "12B", Size: 7_600_000_000}}
	srv.Loaded = []LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, SizeVRAM: 9_000_000_000}}
	srv.Version = "0.34.2"

	var tags map[string]any
	resp, _ := http.Get(srv.URL + "/api/tags")
	_ = json.NewDecoder(resp.Body).Decode(&tags)
	if len(tags["models"].([]any)) != 1 {
		t.Fatalf("tags: %v", tags)
	}
	var show map[string]any
	resp, _ = http.Post(srv.URL+"/api/show", "application/json", bytes.NewReader([]byte(`{"model":"gemma4:12b"}`)))
	_ = json.NewDecoder(resp.Body).Decode(&show)
	if show["model_info"].(map[string]any)["gemma4.context_length"] != float64(262144) {
		t.Fatalf("show: %v", show)
	}
	resp, _ = http.Post(srv.URL+"/api/show", "application/json", bytes.NewReader([]byte(`{"model":"nope"}`)))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown model should be 404, got %d", resp.StatusCode)
	}
	var ps map[string]any
	resp, _ = http.Get(srv.URL + "/api/ps")
	_ = json.NewDecoder(resp.Body).Decode(&ps)
	if ps["models"].([]any)[0].(map[string]any)["context_length"] != float64(32768) {
		t.Fatalf("ps: %v", ps)
	}
	var v map[string]any
	resp, _ = http.Get(srv.URL + "/api/version")
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if v["version"] != "0.34.2" {
		t.Fatalf("version: %v", v)
	}
	resp, _ = http.Post(srv.URL+"/api/generate", "application/json", bytes.NewReader([]byte(`{"model":"gemma4:12b","keep_alive":0}`)))
	if resp.StatusCode != http.StatusOK || len(srv.Unloaded) != 1 {
		t.Fatalf("unload not recorded: %d %v", resp.StatusCode, srv.Unloaded)
	}
}
