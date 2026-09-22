package ollama

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/tara-vision/taracode/internal/llm"
)

// readStream consumes NDJSON chunks, forwards events and builds the Result as it goes: each tool
// call is converted through fromWireToolCall exactly once, so the id a streamed EventToolCall
// carries is the same id that ends up in the returned Result.ToolCalls (assemble, used only by the
// non-stream path, would otherwise convert the same wire call a second time and hand out a second,
// different id from the shared counter).
func readStream(r io.Reader, onEvent func(llm.Event) error) (*llm.Result, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	res := &llm.Result{}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return nil, fmt.Errorf("ollama: decode stream chunk: %w", err)
		}
		if chunk.Error != "" {
			return nil, fmt.Errorf("ollama: %s", chunk.Error)
		}
		res.Content += chunk.Message.Content
		res.Thinking += chunk.Message.Thinking
		if chunk.Message.Thinking != "" {
			if err := onEvent(llm.Event{Kind: llm.EventThinking, Text: chunk.Message.Thinking}); err != nil {
				return nil, err
			}
		}
		if chunk.Message.Content != "" {
			if err := onEvent(llm.Event{Kind: llm.EventText, Text: chunk.Message.Content}); err != nil {
				return nil, err
			}
		}
		for _, tc := range chunk.Message.ToolCalls {
			call := fromWireToolCall(tc)
			res.ToolCalls = append(res.ToolCalls, call)
			if err := onEvent(llm.Event{Kind: llm.EventToolCall, ToolCall: &call}); err != nil {
				return nil, err
			}
		}
		if chunk.Done {
			res.DoneReason = chunk.DoneReason
			res.Usage = llm.Usage{PromptTokens: chunk.PromptEvalCount, CompletionTokens: chunk.EvalCount}
			if err := onEvent(llm.Event{Kind: llm.EventUsage, Usage: &res.Usage}); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ollama: read stream: %w", err)
	}
	return res, nil
}

// assemble folds chunks into a Result (works for the single non-stream chunk too).
func assemble(chunks []chatChunk) *llm.Result {
	res := &llm.Result{}
	for _, c := range chunks {
		res.Content += c.Message.Content
		res.Thinking += c.Message.Thinking
		for _, tc := range c.Message.ToolCalls {
			res.ToolCalls = append(res.ToolCalls, fromWireToolCall(tc))
		}
		if c.Done {
			res.DoneReason = c.DoneReason
			res.Usage = llm.Usage{PromptTokens: c.PromptEvalCount, CompletionTokens: c.EvalCount}
		}
	}
	return res
}
