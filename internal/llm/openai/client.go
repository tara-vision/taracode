// Package openaiclient adapts a go-openai client to llm.Client for OpenAI-compatible servers
// (vLLM, llama.cpp). Context window and keep-alive are not controllable on this path.
package openaiclient

import (
	"context"
	"errors"
	"fmt"
	"io"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// Client wraps a go-openai client.
type Client struct {
	api *openai.Client
}

// New wraps an already configured go-openai client.
func New(api *openai.Client) *Client { return &Client{api: api} }

var _ llm.Client = (*Client)(nil)

func toRequest(req llm.Request, stream bool) openai.ChatCompletionRequest {
	out := openai.ChatCompletionRequest{Model: req.Model, Messages: req.Messages, Tools: req.Tools, Stream: stream}
	if req.Options.Temperature != nil {
		out.Temperature = *req.Options.Temperature
	}
	if req.Options.TopP != nil {
		out.TopP = *req.Options.TopP
	}
	if req.Options.NumPredict > 0 {
		//nolint:staticcheck // SA1019: vLLM/llama.cpp speak the legacy max_tokens field, not OpenAI's max_completion_tokens.
		out.MaxTokens = req.Options.NumPredict
	}
	switch req.Options.Think {
	case llm.ThinkLow, llm.ThinkMedium, llm.ThinkHigh:
		out.ReasoningEffort = string(req.Options.Think)
	}
	if stream {
		out.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}
	return out
}

// Chat implements llm.Client.
func (c *Client) Chat(ctx context.Context, req llm.Request, onEvent func(llm.Event) error) (*llm.Result, error) {
	if onEvent == nil {
		resp, err := c.api.CreateChatCompletion(ctx, toRequest(req, false))
		if err != nil {
			return nil, fmt.Errorf("openai: %w", err)
		}
		res := &llm.Result{Usage: llm.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}}
		if len(resp.Choices) > 0 {
			res.Content = resp.Choices[0].Message.Content
			res.ToolCalls = resp.Choices[0].Message.ToolCalls
			res.DoneReason = string(resp.Choices[0].FinishReason)
		}
		return res, nil
	}
	stream, err := c.api.CreateChatCompletionStream(ctx, toRequest(req, true))
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	defer func() { _ = stream.Close() }()
	return readStream(stream, onEvent)
}

// readStream folds SSE chunks into events; partial tool calls are merged by index and emitted once.
func readStream(stream *openai.ChatCompletionStream, onEvent func(llm.Event) error) (*llm.Result, error) {
	res := &llm.Result{}
	pending := map[int]*openai.ToolCall{}
	order := []int{}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("openai: stream: %w", err)
		}
		if chunk.Usage != nil {
			res.Usage = llm.Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
		}
		if err := processChoices(chunk.Choices, res, pending, &order, onEvent); err != nil {
			return nil, err
		}
	}
	if err := emitToolCalls(res, pending, order, onEvent); err != nil {
		return nil, err
	}
	u := res.Usage
	if err := onEvent(llm.Event{Kind: llm.EventUsage, Usage: &u}); err != nil {
		return nil, err
	}
	return res, nil
}

// processChoices folds one chunk's choices into res, merging partial tool calls into pending.
func processChoices(
	choices []openai.ChatCompletionStreamChoice,
	res *llm.Result,
	pending map[int]*openai.ToolCall,
	order *[]int,
	onEvent func(llm.Event) error,
) error {
	for _, choice := range choices {
		if choice.Delta.Content != "" {
			res.Content += choice.Delta.Content
			if err := onEvent(llm.Event{Kind: llm.EventText, Text: choice.Delta.Content}); err != nil {
				return err
			}
		}
		mergeToolCalls(choice.Delta.ToolCalls, pending, order)
		if choice.FinishReason != "" {
			res.DoneReason = string(choice.FinishReason)
		}
	}
	return nil
}

// mergeToolCalls folds one delta's tool call fragments into pending, keyed by index.
func mergeToolCalls(deltas []openai.ToolCall, pending map[int]*openai.ToolCall, order *[]int) {
	for _, tc := range deltas {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		cur, ok := pending[idx]
		if !ok {
			cp := tc
			pending[idx] = &cp
			*order = append(*order, idx)
			continue
		}
		cur.Function.Arguments += tc.Function.Arguments
		if tc.ID != "" {
			cur.ID = tc.ID
		}
		if tc.Function.Name != "" {
			cur.Function.Name = tc.Function.Name
		}
	}
}

// emitToolCalls appends the merged tool calls to res in first-seen order and emits one event each.
func emitToolCalls(
	res *llm.Result,
	pending map[int]*openai.ToolCall,
	order []int,
	onEvent func(llm.Event) error,
) error {
	for _, idx := range order {
		call := *pending[idx]
		if call.Type == "" {
			call.Type = openai.ToolTypeFunction
		}
		res.ToolCalls = append(res.ToolCalls, call)
		if err := onEvent(llm.Event{Kind: llm.EventToolCall, ToolCall: &call}); err != nil {
			return err
		}
	}
	return nil
}

// Models implements llm.Client via GET /v1/models.
func (c *Client) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	list, err := c.api.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai: list models: %w", err)
	}
	out := make([]llm.ModelInfo, 0, len(list.Models))
	for _, m := range list.Models {
		out = append(out, llm.ModelInfo{Name: m.ID})
	}
	return out, nil
}

// Show is not available on the OpenAI-compatible API.
func (c *Client) Show(context.Context, string) (*llm.ModelDetails, error) {
	return nil, llm.ErrNotSupported
}

// Loaded is not available on the OpenAI-compatible API.
func (c *Client) Loaded(context.Context) ([]llm.LoadedModel, error) { return nil, llm.ErrNotSupported }

// Version is unknown on the OpenAI-compatible API.
func (c *Client) Version(context.Context) (string, error) { return "", nil }

// Unload is not available on the OpenAI-compatible API.
func (c *Client) Unload(context.Context, string) error { return llm.ErrNotSupported }
