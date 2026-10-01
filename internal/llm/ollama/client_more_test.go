package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// rawServer answers every request with status and body.
func rawServer(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, nil)
}

// droppingClient talks to a server that closes every connection without an answer.
func droppingClient(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, nil)
}

var userAsks = []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "hi"}}

func TestNewDefaultsTheHTTPClientAndTrimsTheHost(t *testing.T) {
	c := New("http://gpu-box:11434//", nil)
	if c.http != http.DefaultClient || c.host != "http://gpu-box:11434" {
		t.Fatalf("%+v", c)
	}
}

// TestEndpointsReportTheServersFailures: a refused status carries the plain text the server sent,
// and a 200 with a body that is not JSON is a decode error.
func TestEndpointsReportTheServersFailures(t *testing.T) {
	down := rawServer(t, http.StatusServiceUnavailable, "warming up\n")
	if _, err := down.Models(context.Background()); err == nil || err.Error() != "ollama: /api/tags returned 503: warming up" {
		t.Fatalf("Models: %v", err)
	}
	if _, err := down.Loaded(context.Background()); err == nil || !strings.Contains(err.Error(), "/api/ps returned 503") {
		t.Fatalf("Loaded: %v", err)
	}
	if _, err := down.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "/api/version returned 503") {
		t.Fatalf("Version: %v", err)
	}
	if err := down.Unload(context.Background(), "gemma4:12b"); err == nil || !strings.Contains(err.Error(), "/api/generate returned 503") {
		t.Fatalf("Unload: %v", err)
	}
	garbled := rawServer(t, http.StatusOK, "not json")
	if _, err := garbled.Chat(context.Background(), llm.Request{Messages: userAsks}, nil); err == nil ||
		!strings.HasPrefix(err.Error(), "ollama: decode reply: ") {
		t.Fatalf("Chat: %v", err)
	}
	if _, err := garbled.Show(context.Background(), "m"); err == nil || !strings.HasPrefix(err.Error(), "ollama: decode show: ") {
		t.Fatalf("Show: %v", err)
	}
	if _, err := garbled.Models(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "ollama: decode /api/tags: ") {
		t.Fatalf("Models: %v", err)
	}
}

func TestRequestsThatNeverReachTheServer(t *testing.T) {
	bad := New("http://bad host", nil)
	if _, err := bad.Version(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "ollama: create request: ") {
		t.Fatalf("GET: %v", err)
	}
	if _, err := bad.Show(context.Background(), "m"); err == nil || !strings.HasPrefix(err.Error(), "ollama: create request: ") {
		t.Fatalf("POST: %v", err)
	}
	dropped := droppingClient(t)
	if _, err := dropped.Version(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "ollama: /api/version: ") {
		t.Fatalf("GET dropped: %v", err)
	}
	nan := float32(math.NaN())
	_, err := dropped.Chat(context.Background(), llm.Request{Messages: userAsks, Options: llm.Options{Temperature: &nan}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "ollama: encode request: ") {
		t.Fatalf("an unencodable request: %v", err)
	}
}

// TestUnloadAsksForKeepAliveZero: Unload is a generate request for the model with keep_alive 0,
// which has Ollama drop the model from memory at once.
func TestUnloadAsksForKeepAliveZero(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["path"] = r.URL.Path
		bodies <- body
	}))
	t.Cleanup(srv.Close)
	if err := New(srv.URL, nil).Unload(context.Background(), "gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	if body := <-bodies; body["path"] != "/api/generate" || body["model"] != "gemma4:12b" || body["keep_alive"] != float64(0) {
		t.Fatalf("%v", body)
	}
}

func TestWireCarriesTopPToolArgumentsAndImageURLs(t *testing.T) {
	topP := float32(0.5)
	w := toWire(llm.Request{Options: llm.Options{TopP: &topP}, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, ToolCalls: []openai.ToolCall{{ID: "1",
			Function: openai.FunctionCall{Name: "read_file", Arguments: "not json"}}}},
		{Role: openai.ChatMessageRoleUser, MultiContent: []openai.ChatMessagePart{
			{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &openai.ChatMessageImageURL{URL: "https://example.com/a.png"}}}},
	}}, false)
	if len(w.Options) != 1 || w.Options["top_p"] != 0.5 {
		t.Fatalf("options %v: a zero num_ctx or num_predict is left to the server", w.Options)
	}
	if args := string(w.Messages[0].ToolCalls[0].Function.Arguments); args != "{}" {
		t.Fatalf("arguments that are not JSON go as {}: %s", args)
	}
	if imgs := w.Messages[1].Images; len(imgs) != 1 || imgs[0] != "https://example.com/a.png" {
		t.Fatalf("a URL that is no data URL goes as it is: %v", imgs)
	}
}

const streamLines = "\n" +
	`{"message":{"thinking":"hmm"}}` + "\n\n" +
	`{"message":{"content":"hi"}}` + "\n" +
	`{"message":{"tool_calls":[{"function":{"name":"read_file","arguments":{}}}]}}` + "\n" +
	`{"done":true,"done_reason":"stop"}` + "\n"

// TestReadStreamStopsOnTheCallersError: an error the event callback returns, for any kind of event,
// ends the stream with that error.
func TestReadStreamStopsOnTheCallersError(t *testing.T) {
	stop := errors.New("stop")
	for _, kind := range []llm.EventKind{llm.EventThinking, llm.EventText, llm.EventToolCall, llm.EventUsage} {
		_, err := readStream(strings.NewReader(streamLines), func(e llm.Event) error {
			if e.Kind == kind {
				return stop
			}
			return nil
		})
		if !errors.Is(err, stop) {
			t.Errorf("kind %v: %v", kind, err)
		}
	}
	res, err := readStream(strings.NewReader(streamLines), func(llm.Event) error { return nil })
	if err != nil || res.Content != "hi" || res.Thinking != "hmm" || len(res.ToolCalls) != 1 || res.DoneReason != "stop" {
		t.Fatalf("blank lines are skipped: %+v %v", res, err)
	}
}

func TestReadStreamReportsBrokenStreams(t *testing.T) {
	none := func(llm.Event) error { return nil }
	tests := []struct {
		r    io.Reader
		want string
	}{
		{strings.NewReader("not json\n"), "ollama: decode stream chunk: "},
		{strings.NewReader(`{"error":"model crashed"}` + "\n"), "ollama: model crashed"},
		{io.MultiReader(strings.NewReader(`{"message":{"content":"hi"}}`+"\n"), iotest.ErrReader(errors.New("reset"))),
			"ollama: read stream: reset"},
	}
	for _, tt := range tests {
		if _, err := readStream(tt.r, none); err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%v, want %q", err, tt.want)
		}
	}
}
