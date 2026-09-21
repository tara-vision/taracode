// Package ollamatest is a scripted fake of Ollama's native API for tests: /api/chat (stream and
// non-stream), /api/tags, /api/show, /api/ps, /api/version and /api/generate (unload).
package ollamatest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ToolCall is one scripted tool call.
type ToolCall struct {
	Name string
	Args map[string]any
}

// Turn is one scripted /api/chat reply. Status other than 0 or 200 answers an error body instead.
type Turn struct {
	Content          string
	Thinking         string
	ToolCalls        []ToolCall
	PromptTokens     int
	CompletionTokens int
	DoneReason       string
	Status           int
	Error            string
}

// ModelSpec describes a model for /api/tags and /api/show.
type ModelSpec struct {
	Name          string
	Capabilities  []string
	ContextLength int
	Family        string
	ParameterSize string
	Quantization  string
	Size          int64
}

// LoadedSpec describes a loaded model for /api/ps.
type LoadedSpec struct {
	Name          string
	ContextLength int
	SizeVRAM      int64
}

// RecordedRequest is one request the server saw.
type RecordedRequest struct {
	Path string
	Body map[string]any
}

// Server is the fake. Mutate the exported fields before the code under test calls it.
type Server struct {
	URL      string
	Turns    []Turn
	Models   []ModelSpec
	Loaded   []LoadedSpec
	Version  string
	Requests []RecordedRequest
	Unloaded []string

	mu   sync.Mutex
	next int
	ts   *httptest.Server
}

// New starts a fake server that is closed when the test ends.
func New(t testing.TB) *Server {
	s := &Server{Version: "0.34.2"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", s.chat)
	mux.HandleFunc("/api/tags", s.tags)
	mux.HandleFunc("/api/show", s.show)
	mux.HandleFunc("/api/ps", s.ps)
	mux.HandleFunc("/api/version", s.version)
	mux.HandleFunc("/api/generate", s.generate)
	s.ts = httptest.NewServer(mux)
	s.URL = s.ts.URL
	t.Cleanup(s.ts.Close)
	return s
}

func (s *Server) record(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	s.mu.Lock()
	s.Requests = append(s.Requests, RecordedRequest{Path: r.URL.Path, Body: m})
	s.mu.Unlock()
	return m
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	req := s.record(r)
	s.mu.Lock()
	if s.next >= len(s.Turns) {
		s.mu.Unlock()
		http.Error(w, `{"error":"ollamatest: no scripted turn left"}`, http.StatusInternalServerError)
		return
	}
	turn := s.Turns[s.next]
	s.next++
	s.mu.Unlock()

	if turn.Status != 0 && turn.Status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(turn.Status)
		_, _ = fmt.Fprintf(w, `{"error":%q}`, turn.Error)
		return
	}
	model, _ := req["model"].(string)
	stream, _ := req["stream"].(bool)
	if !stream {
		s.writeJSON(w, s.finalChunk(model, turn, turn.Content, turn.Thinking, true))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	for _, word := range splitKeepingSpaces(turn.Thinking) {
		_ = enc.Encode(chunk(model, map[string]any{"role": "assistant", "content": "", "thinking": word}, false))
	}
	for _, word := range splitKeepingSpaces(turn.Content) {
		_ = enc.Encode(chunk(model, map[string]any{"role": "assistant", "content": word, "thinking": ""}, false))
	}
	if len(turn.ToolCalls) > 0 {
		_ = enc.Encode(chunk(model, map[string]any{
			"role": "assistant", "content": "", "thinking": "", "tool_calls": toolCallsJSON(turn.ToolCalls),
		}, false))
	}
	_ = enc.Encode(s.finalChunk(model, turn, "", "", false))
}

// finalChunk builds the done chunk; withCalls attaches the tool calls (non-stream replies carry
// them on the single message, streamed replies already sent them in their own chunk).
func (s *Server) finalChunk(model string, turn Turn, content, thinking string, withCalls bool) map[string]any {
	msg := map[string]any{"role": "assistant", "content": content, "thinking": thinking}
	if withCalls && len(turn.ToolCalls) > 0 {
		msg["tool_calls"] = toolCallsJSON(turn.ToolCalls)
	}
	c := chunk(model, msg, true)
	reason := turn.DoneReason
	if reason == "" {
		reason = "stop"
	}
	c["done_reason"] = reason
	c["prompt_eval_count"] = turn.PromptTokens
	c["eval_count"] = turn.CompletionTokens
	return c
}

func chunk(model string, msg map[string]any, done bool) map[string]any {
	return map[string]any{"model": model, "created_at": "2026-09-21T00:00:00Z", "message": msg, "done": done}
}

func toolCallsJSON(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		out = append(out, map[string]any{"function": map[string]any{"name": c.Name, "arguments": c.Args}})
	}
	return out
}

// splitKeepingSpaces yields word-sized deltas that concatenate back to the input.
func splitKeepingSpaces(text string) []string {
	if text == "" {
		return nil
	}
	var parts []string
	for _, w := range strings.SplitAfter(text, " ") {
		if w != "" {
			parts = append(parts, w)
		}
	}
	return parts
}

// detailsJSON is the "details" object shared by /api/tags and /api/show entries.
func detailsJSON(m ModelSpec) map[string]any {
	return map[string]any{"family": m.Family, "parameter_size": m.ParameterSize, "quantization_level": m.Quantization}
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	models := make([]map[string]any, 0, len(s.Models))
	for _, m := range s.Models {
		models = append(models, map[string]any{
			"name": m.Name, "model": m.Name, "size": m.Size, "modified_at": "2026-09-21T00:00:00Z",
			"details": detailsJSON(m),
		})
	}
	s.writeJSON(w, map[string]any{"models": models})
}

func (s *Server) show(w http.ResponseWriter, r *http.Request) {
	req := s.record(r)
	name, _ := req["model"].(string)
	for _, m := range s.Models {
		if m.Name == name || m.Name == name+":latest" {
			s.writeJSON(w, map[string]any{
				"capabilities": m.Capabilities,
				"details":      detailsJSON(m),
				"model_info":   map[string]any{m.Family + ".context_length": m.ContextLength},
			})
			return
		}
	}
	http.Error(w, fmt.Sprintf(`{"error":"model '%s' not found"}`, name), http.StatusNotFound)
}

func (s *Server) ps(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	models := make([]map[string]any, 0, len(s.Loaded))
	for _, m := range s.Loaded {
		models = append(models, map[string]any{
			"name": m.Name, "model": m.Name, "context_length": m.ContextLength, "size_vram": m.SizeVRAM, "size": m.SizeVRAM,
		})
	}
	s.writeJSON(w, map[string]any{"models": models})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	s.writeJSON(w, map[string]any{"version": s.Version})
}

func (s *Server) generate(w http.ResponseWriter, r *http.Request) {
	req := s.record(r)
	if name, ok := req["model"].(string); ok {
		s.mu.Lock()
		s.Unloaded = append(s.Unloaded, name)
		s.mu.Unlock()
	}
	s.writeJSON(w, map[string]any{"model": req["model"], "done": true, "done_reason": "unload"})
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
