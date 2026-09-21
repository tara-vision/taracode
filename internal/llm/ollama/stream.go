package ollama

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/tara-vision/taracode/internal/llm"
)

// readStream consumes NDJSON chunks, forwards events and assembles the result.
func readStream(r io.Reader, onEvent func(llm.Event) error) (*llm.Result, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var chunks []chatChunk
	seq := 0
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
		chunks = append(chunks, chunk)
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
			call := fromWireToolCall(tc, seq)
			seq++
			if err := onEvent(llm.Event{Kind: llm.EventToolCall, ToolCall: &call}); err != nil {
				return nil, err
			}
		}
		if chunk.Done {
			u := llm.Usage{PromptTokens: chunk.PromptEvalCount, CompletionTokens: chunk.EvalCount}
			if err := onEvent(llm.Event{Kind: llm.EventUsage, Usage: &u}); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ollama: read stream: %w", err)
	}
	return assemble(chunks), nil
}

// assemble folds chunks into a Result (works for the single non-stream chunk too).
func assemble(chunks []chatChunk) *llm.Result {
	res := &llm.Result{}
	seq := 0
	for _, c := range chunks {
		res.Content += c.Message.Content
		res.Thinking += c.Message.Thinking
		for _, tc := range c.Message.ToolCalls {
			res.ToolCalls = append(res.ToolCalls, fromWireToolCall(tc, seq))
			seq++
		}
		if c.Done {
			res.DoneReason = c.DoneReason
			res.Usage = llm.Usage{PromptTokens: c.PromptEvalCount, CompletionTokens: c.EvalCount}
		}
	}
	return res
}
