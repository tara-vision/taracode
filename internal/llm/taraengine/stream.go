package taraengine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// The bounds of one stream line: llama-server sends a tool call's arguments in a single delta.
const (
	lineBuffer = 64 << 10
	maxLine    = 8 << 20
)

// streamChunk is one event of the engine's /v1 stream: the deltas, then a finish reason, then a last chunk with no
// choices that carries the usage and llama-server's timings (captured live on b11514).
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string            `json:"content"`
			ReasoningContent string            `json:"reasoning_content"`
			ToolCalls        []openai.ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Timings *struct {
		PromptMS    float64 `json:"prompt_ms"`
		PredictedMS float64 `json:"predicted_ms"`
	} `json:"timings"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// readStream reads the SSE stream to its [DONE], forwarding events as they come and assembling the result. A stream
// that ends before [DONE] is llm.ErrStreamCut.
func readStream(r io.Reader, onEvent func(llm.Event) error) (*llm.Result, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, lineBuffer), maxLine)
	f := &folder{res: &llm.Result{}, pending: map[int]*openai.ToolCall{}, onEvent: onEvent}
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue // the blank line between events, or a comment
		}
		if data == "[DONE]" {
			return f.finish()
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("taraengine: decode a stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("taraengine: %s", chunk.Error.Message)
		}
		if err := f.add(chunk); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", llm.ErrStreamCut, err)
	}
	return nil, llm.ErrStreamCut
}

// folder assembles the result of one stream.
type folder struct {
	res     *llm.Result
	pending map[int]*openai.ToolCall
	order   []int
	onEvent func(llm.Event) error
}

// add folds one chunk in: thinking and text are forwarded as they come, tool-call fragments are merged by index, and
// the usage and timings are kept for the end.
func (f *folder) add(chunk streamChunk) error {
	for _, choice := range chunk.Choices {
		if t := choice.Delta.ReasoningContent; t != "" {
			f.res.Thinking += t
			if err := f.onEvent(llm.Event{Kind: llm.EventThinking, Text: t}); err != nil {
				return err
			}
		}
		if t := choice.Delta.Content; t != "" {
			f.res.Content += t
			if err := f.onEvent(llm.Event{Kind: llm.EventText, Text: t}); err != nil {
				return err
			}
		}
		f.merge(choice.Delta.ToolCalls)
		if choice.FinishReason != "" {
			f.res.DoneReason = choice.FinishReason
		}
	}
	if u := chunk.Usage; u != nil {
		f.res.Usage.PromptTokens, f.res.Usage.CompletionTokens = u.PromptTokens, u.CompletionTokens
	}
	if t := chunk.Timings; t != nil {
		f.res.Usage.PromptDuration, f.res.Usage.EvalDuration = millis(t.PromptMS), millis(t.PredictedMS)
	}
	return nil
}

// merge folds tool-call fragments into pending by index: the first fragment of an index starts the call, later ones
// append to its arguments.
func (f *folder) merge(deltas []openai.ToolCall) {
	for _, tc := range deltas {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		cur, ok := f.pending[idx]
		if !ok {
			call := tc
			f.pending[idx] = &call
			f.order = append(f.order, idx)
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

// finish emits the merged tool calls in the order they began, then the usage, and returns the result. A call without
// an id gets one, since the tool's answer must name the call it answers; the stream's index is dropped.
func (f *folder) finish() (*llm.Result, error) {
	for _, idx := range f.order {
		call := *f.pending[idx]
		call.Index = nil
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", idx)
		}
		if call.Type == "" {
			call.Type = openai.ToolTypeFunction
		}
		f.res.ToolCalls = append(f.res.ToolCalls, call)
		if err := f.onEvent(llm.Event{Kind: llm.EventToolCall, ToolCall: &call}); err != nil {
			return nil, err
		}
	}
	usage := f.res.Usage
	if err := f.onEvent(llm.Event{Kind: llm.EventUsage, Usage: &usage}); err != nil {
		return nil, err
	}
	return f.res, nil
}

// millis is a llama-server timing, in milliseconds, as a duration, rounded to the nanosecond.
func millis(ms float64) time.Duration {
	return time.Duration(math.Round(ms * float64(time.Millisecond)))
}
