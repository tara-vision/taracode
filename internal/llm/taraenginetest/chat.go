package taraenginetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// chat answers the next scripted turn.
func (s *Server) chat(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	s.mu.Lock()
	if s.next >= len(s.Turns) {
		s.mu.Unlock()
		writeChatError(w, http.StatusInternalServerError, "taraenginetest: no scripted turn left", "")
		return
	}
	turn := s.Turns[s.next]
	s.next++
	s.mu.Unlock()
	if turn.Status != 0 && turn.Status != http.StatusOK {
		if turn.Status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "2")
		}
		writeChatError(w, turn.Status, turn.Error, turn.Hint)
		return
	}
	model, _ := body["model"].(string)
	stream(w, model, turn)
}

// writeChatError answers the engine proxy's error body on /v1.
func writeChatError(w http.ResponseWriter, status int, message, hint string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message, "code": status, "hint": hint}})
}

// stream writes a turn the way llama-server streams through the engine: a role chunk, the reasoning and content
// deltas word by word, each tool call in two fragments, the finish reason, a last chunk with no choices that
// carries usage and timings, then [DONE].
func stream(w http.ResponseWriter, model string, turn Turn) {
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(v any) {
		data, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	delta := func(d map[string]any, finish any) map[string]any {
		return map[string]any{"object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0, "delta": d, "finish_reason": finish}}}
	}
	send(delta(map[string]any{"role": "assistant", "content": ""}, nil))
	for _, word := range words(turn.Thinking) {
		send(delta(map[string]any{"reasoning_content": word}, nil))
	}
	for _, word := range words(turn.Content) {
		send(delta(map[string]any{"content": word}, nil))
	}
	for i, call := range turn.ToolCalls {
		args := []byte("{}")
		if call.Args != nil {
			args, _ = json.Marshal(call.Args)
		}
		half := len(args) / 2
		send(delta(map[string]any{"tool_calls": []map[string]any{{"index": i, "id": fmt.Sprintf("call_%d", i),
			"type": "function", "function": map[string]any{"name": call.Name, "arguments": string(args[:half])}}}}, nil))
		send(delta(map[string]any{"tool_calls": []map[string]any{{"index": i,
			"function": map[string]any{"arguments": string(args[half:])}}}}, nil))
	}
	if turn.Cut {
		return
	}
	send(delta(map[string]any{}, finishReason(turn)))
	send(map[string]any{"object": "chat.completion.chunk", "model": model, "choices": []any{},
		"usage": map[string]int{"prompt_tokens": turn.PromptTokens, "completion_tokens": turn.CompletionTokens,
			"total_tokens": turn.PromptTokens + turn.CompletionTokens},
		"timings": map[string]any{"prompt_n": turn.PromptTokens, "prompt_ms": millis(turn.PromptEvalDuration),
			"predicted_n": turn.CompletionTokens, "predicted_ms": millis(turn.EvalDuration)}})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func finishReason(turn Turn) string {
	switch {
	case turn.DoneReason != "":
		return turn.DoneReason
	case len(turn.ToolCalls) > 0:
		return "tool_calls"
	}
	return "stop"
}

// words yields word-sized deltas that concatenate back to the input.
func words(text string) []string {
	var out []string
	for _, w := range strings.SplitAfter(text, " ") {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
