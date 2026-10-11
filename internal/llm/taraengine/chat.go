package taraengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taraengine/pkg/client"

	"github.com/tara-vision/taracode/internal/llm"
)

// maxErrorBody bounds how much of a refusal Chat reads.
const maxErrorBody = 1 << 20

// chatRequest is the body of POST /v1/chat/completions. It is taracode's own type because go-openai's drops a zero
// temperature (omitempty on a float) and the evals run at temperature 0; the pointers here send a 0.
type chatRequest struct {
	Model              string                         `json:"model"`
	Messages           []openai.ChatCompletionMessage `json:"messages"`
	Tools              []openai.Tool                  `json:"tools,omitempty"`
	Stream             bool                           `json:"stream"`
	StreamOptions      streamOptions                  `json:"stream_options"`
	Temperature        *float32                       `json:"temperature,omitempty"`
	TopP               *float32                       `json:"top_p,omitempty"`
	MaxTokens          int                            `json:"max_tokens,omitempty"`
	ReasoningEffort    string                         `json:"reasoning_effort,omitempty"`
	ChatTemplateKwargs map[string]any                 `json:"chat_template_kwargs,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// newChatRequest maps a request onto the engine's /v1. Thinking off and on go through
// chat_template_kwargs.enable_thinking, the levels through reasoning_effort with thinking on, and auto sends neither.
// The window is set at load and keep_alive does not exist on the engine, so neither is sent.
func newChatRequest(req llm.Request) chatRequest {
	out := chatRequest{Model: req.Model, Messages: req.Messages, Tools: req.Tools, Stream: true,
		StreamOptions: streamOptions{IncludeUsage: true}, Temperature: req.Options.Temperature, TopP: req.Options.TopP}
	if req.Options.NumPredict > 0 {
		out.MaxTokens = req.Options.NumPredict
	}
	switch req.Options.Think {
	case llm.ThinkOff:
		out.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	case llm.ThinkOn:
		out.ChatTemplateKwargs = map[string]any{"enable_thinking": true}
	case llm.ThinkLow, llm.ThinkMedium, llm.ThinkHigh:
		out.ChatTemplateKwargs = map[string]any{"enable_thinking": true}
		out.ReasoningEffort = string(req.Options.Think)
	}
	return out
}

// Chat implements llm.Client. It always streams; a nil onEvent drops the events and keeps the assembled result.
// A context that ends the read is reported as the context's error, never as llm.ErrStreamCut.
func (c *Client) Chat(ctx context.Context, req llm.Request, onEvent func(llm.Event) error) (*llm.Result, error) {
	if onEvent == nil {
		onEvent = func(llm.Event) error { return nil }
	}
	body, err := json.Marshal(newChatRequest(req))
	if err != nil {
		return nil, fmt.Errorf("taraengine: encode the request: %w", err)
	}
	resp, err := c.post(ctx, req.Model, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	res, err := readStream(resp.Body, onEvent)
	if err != nil && ctx.Err() != nil {
		// The context ended the read, so its error is the answer; a stream cut is one the context did not cause.
		return nil, fmt.Errorf("taraengine: chat: %w", ctx.Err())
	}
	return res, err
}

// post sends the request, once more after retryDelay when the engine answers 503, and turns a refusal into
// *llm.ServerError and a dial that reached nothing into *llm.UnreachableError.
func (c *Client) post(ctx context.Context, model string, body []byte) (*http.Response, error) {
	resp, err := c.send(ctx, body)
	if err == nil && resp.StatusCode == http.StatusServiceUnavailable {
		_ = resp.Body.Close()
		if werr := c.engine.sleep(ctx, retryDelay); werr != nil {
			return nil, werr
		}
		resp, err = c.send(ctx, body)
	}
	switch {
	case err != nil && client.IsConnectionError(err):
		return nil, &llm.UnreachableError{Host: c.host, Hint: StartHint, Err: err}
	case err != nil:
		return nil, fmt.Errorf("taraengine: chat: %w", err)
	case resp.StatusCode != http.StatusOK:
		defer func() { _ = resp.Body.Close() }()
		return nil, refusal(resp, model)
	}
	return resp, nil
}

// send is one POST of the chat request.
func (c *Client) send(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	return c.http.Do(req)
}

// refusal reads the engine's error body, {"error":{"message","hint"}}, into *llm.ServerError. Another body becomes
// the message, and an empty one the status text.
func refusal(resp *http.Response, model string) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	var body struct {
		Error struct {
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	e := &llm.ServerError{Status: resp.StatusCode, Model: model}
	if json.Unmarshal(data, &body) == nil && body.Error.Message != "" {
		e.Message, e.Hint = body.Error.Message, body.Error.Hint
	} else {
		e.Message = strings.TrimSpace(string(data))
	}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	return e
}
