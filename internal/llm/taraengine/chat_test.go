package taraengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func userAsks(text string) llm.Request {
	return llm.Request{Model: "gemma4:12b",
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: text}}}
}

func TestChatStreamsThinkingTextToolsAndTimings(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Turns = []taraenginetest.Turn{{Thinking: "two pods restart", Content: "Checking.", PromptTokens: 46,
		CompletionTokens: 12, PromptEvalDuration: 5267 * time.Microsecond, EvalDuration: 3095 * time.Microsecond,
		ToolCalls: []taraenginetest.ToolCall{{Name: "list_files", Args: map[string]any{"path": "."}}, {Name: "noop"}}}}
	var thinking, text string
	var calls []openai.ToolCall
	var usage *llm.Usage
	res, err := New(srv.URL, "", nil).Chat(context.Background(), userAsks("why?"), func(e llm.Event) error {
		switch e.Kind {
		case llm.EventThinking:
			thinking += e.Text
		case llm.EventText:
			text += e.Text
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
	if thinking != "two pods restart" || text != "Checking." || res.Content != text || res.Thinking != thinking {
		t.Fatalf("thinking %q, text %q, result %+v", thinking, text, res)
	}
	if len(calls) != 2 || calls[0].Function.Arguments != `{"path":"."}` || calls[0].ID != "call_0" ||
		calls[1].Function.Name != "noop" || calls[1].Type != openai.ToolTypeFunction || len(res.ToolCalls) != 2 {
		t.Fatalf("tool calls %+v", calls)
	}
	want := llm.Usage{PromptTokens: 46, CompletionTokens: 12, PromptDuration: 5267 * time.Microsecond,
		EvalDuration: 3095 * time.Microsecond}
	if usage == nil || *usage != want || res.Usage != want || res.DoneReason != "tool_calls" {
		t.Fatalf("usage %+v, result %+v", usage, res)
	}
}

func TestChatMapsTheRequest(t *testing.T) {
	zero, half := float32(0), float32(0.5)
	cases := []struct {
		think        llm.Think
		kwargs       any
		effort       any
		maxTokens    int
		wantMaxToken any
	}{
		{llm.ThinkAuto, nil, nil, 0, nil},
		{llm.ThinkOff, map[string]any{"enable_thinking": false}, nil, 100, float64(100)},
		{llm.ThinkOn, map[string]any{"enable_thinking": true}, nil, 0, nil},
		{llm.ThinkHigh, map[string]any{"enable_thinking": true}, "high", 0, nil},
	}
	for _, c := range cases {
		srv := taraenginetest.New(t)
		srv.Turns = []taraenginetest.Turn{{Content: "ok"}}
		req := userAsks("hi")
		req.Tools = []openai.Tool{{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "noop"}}}
		req.Options = llm.Options{Think: c.think, Temperature: &zero, TopP: &half, NumPredict: c.maxTokens}
		if _, err := New(srv.URL, "", nil).Chat(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
		body := srv.Requests[0].Body
		if srv.Requests[0].Path != "/v1/chat/completions" || body["stream"] != true || body["temperature"] != 0.0 ||
			body["top_p"] != 0.5 || body["max_tokens"] != c.wantMaxToken {
			t.Errorf("%q: %v", c.think, body)
		}
		if kwargs := body["chat_template_kwargs"]; !sameJSON(kwargs, c.kwargs) || body["reasoning_effort"] != c.effort {
			t.Errorf("%q: kwargs %v, effort %v", c.think, kwargs, body["reasoning_effort"])
		}
		if opts, _ := body["stream_options"].(map[string]any); opts["include_usage"] != true || body["tools"] == nil {
			t.Errorf("%q: stream_options %v, tools %v", c.think, body["stream_options"], body["tools"])
		}
		for _, key := range []string{"options", "keep_alive", "num_ctx"} {
			if _, sent := body[key]; sent {
				t.Errorf("%q: %s must not be sent", c.think, key)
			}
		}
	}
}

// sameJSON compares a decoded JSON value with the expected one.
func sameJSON(got, want any) bool {
	g, _ := got.(map[string]any)
	w, _ := want.(map[string]any)
	if g == nil || w == nil {
		return g == nil && w == nil
	}
	return len(g) == len(w) && g["enable_thinking"] == w["enable_thinking"]
}

func TestChatSendsTheKey(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Key = "k"
	srv.Turns = []taraenginetest.Turn{{Content: "ok"}}
	if _, err := New(srv.URL, "k", nil).Chat(context.Background(), userAsks("hi"), nil); err != nil {
		t.Fatal(err)
	}
	if srv.Requests[0].Auth != "Bearer k" {
		t.Fatalf("auth %q", srv.Requests[0].Auth)
	}
}

func TestChatTurnsARefusalIntoAServerError(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Turns = []taraenginetest.Turn{{Status: http.StatusNotFound, Error: `model "gemma4:12b" is not a pulled model`,
		Hint: "taraengine list shows the models the engine knows"}}
	_, err := New(srv.URL, "", nil).Chat(context.Background(), userAsks("hi"), nil)
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusNotFound || refused.Model != "gemma4:12b" ||
		refused.Hint != "taraengine list shows the models the engine knows" {
		t.Fatalf("a refusal: %#v", err)
	}
}

func TestChatReadsARefusalThatIsNotTheEnginesBody(t *testing.T) {
	for body, want := range map[string]string{"upstream gone\n": "upstream gone", "": "Bad Gateway"} {
		bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(body))
		}))
		_, err := New(bare.URL, "", nil).Chat(context.Background(), userAsks("hi"), nil)
		bare.Close()
		var refused *llm.ServerError
		if !errors.As(err, &refused) || refused.Message != want || refused.Status != http.StatusBadGateway {
			t.Errorf("body %q: %#v", body, err)
		}
	}
}

func TestChatRetriesA503Once(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Turns = []taraenginetest.Turn{{Status: http.StatusServiceUnavailable, Error: "the engine backend is not running"},
		{Content: "back"}, {Status: http.StatusServiceUnavailable}, {Status: http.StatusServiceUnavailable,
			Error: "the engine backend is not running", Hint: "wait a moment and retry"}}
	c := New(srv.URL, "", nil)
	waits := noWait(c.engine)
	if res, err := c.Chat(context.Background(), userAsks("hi"), nil); err != nil || res.Content != "back" || *waits != 1 {
		t.Fatalf("one 503 then an answer: %+v %v after %d waits", res, err, *waits)
	}
	_, err := c.Chat(context.Background(), userAsks("hi"), nil)
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusServiceUnavailable || *waits != 2 {
		t.Fatalf("two 503s: %#v after %d waits", err, *waits)
	}
}

func TestChatRetryWaitEndsWithTheContext(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Turns = []taraenginetest.Turn{{Status: http.StatusServiceUnavailable}}
	c := New(srv.URL, "", nil)
	c.engine.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := c.Chat(context.Background(), userAsks("hi"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait: %v", err)
	}
}

func TestChatReportsACutStream(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Turns = []taraenginetest.Turn{{Content: "half an", Cut: true}}
	if _, err := New(srv.URL, "", nil).Chat(context.Background(), userAsks("hi"), nil); !errors.Is(err, llm.ErrStreamCut) {
		t.Fatalf("a cut stream: %v", err)
	}
}

// stallingEngine answers one text delta, then holds the stream open until the client goes away. The request body is
// drained first, so the server sees the disconnect; the 5 s bound only keeps a broken test from hanging.
func stallingEngine(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n")
		_ = http.NewResponseController(w).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChatReportsADeadlineThatEndsTheStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	sawText := false
	_, err := New(stallingEngine(t).URL, "", nil).Chat(ctx, userAsks("hi"), func(e llm.Event) error {
		if e.Kind == llm.EventText {
			sawText = true
		}
		return nil
	})
	if !sawText || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, llm.ErrStreamCut) {
		t.Fatalf("a deadline mid-stream (text first %v): %v", sawText, err)
	}
}

func TestChatReportsACancelThatEndsTheStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := New(stallingEngine(t).URL, "", nil).Chat(ctx, userAsks("hi"), func(e llm.Event) error {
		if e.Kind == llm.EventText {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, llm.ErrStreamCut) {
		t.Fatalf("a cancel mid-stream: %v", err)
	}
}

func TestChatReportsAnUnreachableEngine(t *testing.T) {
	_, err := New("http://127.0.0.1:1", "", nil).Chat(context.Background(), userAsks("hi"), nil)
	var unreachable *llm.UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Hint != StartHint {
		t.Fatalf("a closed port: %#v", err)
	}
}

func TestChatReportsOtherFailures(t *testing.T) {
	if _, err := New("http://bad host", "", nil).Chat(context.Background(), userAsks("hi"), nil); err == nil {
		t.Error("a host that is not a URL: no error")
	}
	both := userAsks("hi")
	both.Messages[0].MultiContent = []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeText, Text: "hi"}}
	if _, err := New("http://127.0.0.1:1", "", nil).Chat(context.Background(), both, nil); err == nil {
		t.Error("a message with both content forms does not encode: no error")
	}
}
