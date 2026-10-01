package ollamatest

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func postChat(t *testing.T, srv *Server, stream bool) *http.Response {
	t.Helper()
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":false}`
	if stream {
		body = strings.Replace(body, `"stream":false`, `"stream":true`, 1)
	}
	resp, err := http.Post(srv.URL+"/api/chat", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestShowFindsAModelByItsNameWithoutATag: like Ollama, the fake reads "gemma4" as "gemma4:latest".
func TestShowFindsAModelByItsNameWithoutATag(t *testing.T) {
	srv := New(t)
	srv.Models = []ModelSpec{{Name: "gemma4:latest", Family: "gemma4", ContextLength: 8192}}
	resp, err := http.Post(srv.URL+"/api/show", "application/json", strings.NewReader(`{"model":"gemma4"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var show map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %v", resp.StatusCode, err)
	}
	if info, _ := show["model_info"].(map[string]any); info["gemma4.context_length"] != float64(8192) {
		t.Fatalf("%v", show)
	}
}

func TestAnErrorTurnAnswersItsStatus(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{Status: http.StatusServiceUnavailable, Error: "loading model"}}
	resp := postChat(t, srv, true)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || strings.TrimSpace(string(body)) != `{"error":"loading model"}` {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestNonStreamReplyCarriesTheToolCalls(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{ToolCalls: []ToolCall{{Name: "list_files", Args: map[string]any{"path": "."}}}}}
	var m map[string]any
	if err := json.NewDecoder(postChat(t, srv, false).Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	calls, _ := m["message"].(map[string]any)["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["function"].(map[string]any)["name"] != "list_files" {
		t.Fatalf("%v", m)
	}
}

// TestAStreamedToolCallOnlyTurnSendsNoTextChunks: a turn with no thinking and no content streams the
// tool call chunk and the final chunk only.
func TestAStreamedToolCallOnlyTurnSendsNoTextChunks(t *testing.T) {
	srv := New(t)
	srv.Turns = []Turn{{ToolCalls: []ToolCall{{Name: "list_files", Args: map[string]any{}}}}}
	sc := bufio.NewScanner(postChat(t, srv, true).Body)
	var lines int
	for sc.Scan() {
		lines++
	}
	if lines != 2 {
		t.Fatalf("%d chunks", lines)
	}
	if got := splitKeepingSpaces(""); got != nil {
		t.Fatalf("%q", got)
	}
}
