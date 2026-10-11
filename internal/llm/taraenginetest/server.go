// Package taraenginetest is a scripted fake of taraengine for tests: the control API under /api, built from the
// engine's own pkg/api types, and /v1/chat/completions streams in llama-server's shape (deltas, a finish reason, a
// last chunk with usage and timings, then [DONE]).
package taraenginetest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tara-vision/taraengine/pkg/api"
)

// The values a new Server reports about itself.
const (
	DefaultVersion  = "0.2.0"
	DefaultRuntime  = "b11514"
	DefaultPlatform = "linux/amd64/cuda12"
)

// ToolCall is one scripted tool call.
type ToolCall struct {
	Name string
	Args map[string]any
}

// Turn is one scripted /v1/chat/completions reply. A Status other than 0 or 200 answers the engine's error body
// instead; Cut ends the stream after the deltas, as a server that died mid-answer does.
type Turn struct {
	Content            string
	Thinking           string
	ToolCalls          []ToolCall
	PromptTokens       int
	CompletionTokens   int
	PromptEvalDuration time.Duration // reported as timings.prompt_ms
	EvalDuration       time.Duration // reported as timings.predicted_ms
	DoneReason         string        // finish_reason; "" = "tool_calls" when the turn calls tools, else "stop"
	Status             int
	Error              string
	Hint               string
	Cut                bool
}

// ModelSpec is one model of the engine's catalog, for /api/models and /api/models/{name}.
type ModelSpec struct {
	Name          string
	Capabilities  []string
	ContextLength int // the native window
	Family        string
	ParameterSize string
	Quantization  string
	Size          int64
	Status        string // "" = "pulled"; "available" is in the catalog but not pulled
	Tier          string
	Default       bool
	Digest        string
	ExitCode      int
	LogTail       string
}

// LoadedSpec is one model /api/loaded lists. Its memory is Size bytes, or SizeVRAM when Size is 0.
type LoadedSpec struct {
	Name          string
	ContextLength int
	Size          int64
	SizeVRAM      int64
	Source        string // "" = "measured"; "estimated"; "none" leaves the memory figure out
}

// LoadRequest is one POST /api/load the fake served.
type LoadRequest struct {
	Model   string
	Context int
	Force   bool
}

// Failure scripts the engine's error answer for one route.
type Failure struct {
	Status  int
	Message string
	Hint    string
	Times   int // calls that fail before the route answers normally; 0 = every call
}

// RecordedRequest is one request the fake saw. Body is nil when the request had none or it was not JSON.
type RecordedRequest struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

// Server is the fake. Set the exported fields before the code under test calls it.
type Server struct {
	URL      string
	Key      string // when set, every route but /api/health needs "Authorization: Bearer <Key>"
	Turns    []Turn
	Models   []ModelSpec
	Loaded   []LoadedSpec
	Version  string
	Runtime  string
	Platform string
	Health   api.Health          // the /api/health answer; an empty Status answers ok with the backend llama.cpp
	Doctor   api.Doctor          // the /api/doctor answer
	Failures map[string]*Failure // keyed by "METHOD /path", for example "POST /api/load"
	Requests []RecordedRequest
	Loads    []LoadRequest
	Unloaded []string

	mu    sync.Mutex
	next  int
	calls int // tool calls streamed so far; the ids run on across the turns
}

// New starts a fake engine that is closed when the test ends.
func New(t testing.TB) *Server {
	s := &Server{Version: DefaultVersion, Runtime: DefaultRuntime, Platform: DefaultPlatform,
		Doctor: defaultDoctor(), Failures: map[string]*Failure{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("GET /api/version", s.guard(s.version))
	mux.HandleFunc("GET /api/models", s.guard(s.listModels))
	mux.HandleFunc("GET /api/models/{name...}", s.guard(s.showModel))
	mux.HandleFunc("GET /api/loaded", s.guard(s.loaded))
	mux.HandleFunc("POST /api/load", s.guard(s.load))
	mux.HandleFunc("POST /api/unload", s.guard(s.unload))
	mux.HandleFunc("GET /api/doctor", s.guard(s.doctor))
	mux.HandleFunc("POST /v1/chat/completions", s.guard(s.chat))
	ts := httptest.NewServer(mux)
	s.URL = ts.URL
	t.Cleanup(ts.Close)
	return s
}

// defaultDoctor is an RTX 5090 machine in the 32 GB tier with the runtime installed.
func defaultDoctor() api.Doctor {
	return api.Doctor{
		Machine: api.Machine{OS: "linux", Arch: "amd64", GPU: "NVIDIA GeForce RTX 5090", GPUMemoryBytes: 32 << 30,
			RAMBytes: 48 << 30, Tier: "32", DriverVersion: "570.211.01"},
		Runtime:        api.RuntimeStatus{Installed: true, Verified: true, Build: DefaultRuntime},
		Recommendation: map[string]string{"16": "gemma4:12b", "32": "glm-4.7-flash", "48": "qwen3.6:35b"},
	}
}

// handler is a route behind guard; body is the request's JSON, nil when there is none.
type handler func(w http.ResponseWriter, r *http.Request, body map[string]any)

// guard records the request, enforces the key and answers a scripted failure before the route's own handler.
func (s *Server) guard(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := s.record(r)
		if s.Key != "" && r.Header.Get("Authorization") != "Bearer "+s.Key {
			writeError(w, http.StatusUnauthorized, "missing or wrong API key", "send Authorization: Bearer <api_key>")
			return
		}
		if f := s.failure(r.Method + " " + r.URL.Path); f != nil {
			writeError(w, f.Status, f.Message, f.Hint)
			return
		}
		next(w, r, body)
	}
}

// record appends the request to Requests and returns its JSON body.
func (s *Server) record(r *http.Request) map[string]any {
	data, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(data, &body)
	s.mu.Lock()
	s.Requests = append(s.Requests, RecordedRequest{Method: r.Method, Path: r.URL.Path,
		Auth: r.Header.Get("Authorization"), Body: body})
	s.mu.Unlock()
	return body
}

// failure is the scripted failure for key while it has calls left, counting this call against it.
func (s *Server) failure(key string) *Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.Failures[key]
	if f != nil && f.Times > 0 {
		f.Times--
		if f.Times == 0 {
			delete(s.Failures, key)
		}
	}
	return f
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.record(r)
	h := s.Health
	if h.Status == "" {
		h = api.Health{Status: "ok", Backend: "llama.cpp"}
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	writeJSON(w, http.StatusOK, api.Version{Version: s.Version,
		Runtime: api.RuntimeInfo{Build: s.Runtime, Backend: "llama.cpp"}, Platform: s.Platform})
}

func (s *Server) listModels(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	out := make([]api.Model, 0, len(s.Models))
	for _, m := range s.Models {
		out = append(out, m.apiModel())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) showModel(w http.ResponseWriter, r *http.Request, _ map[string]any) {
	name := r.PathValue("name")
	for _, m := range s.Models {
		if m.Name == name {
			writeJSON(w, http.StatusOK, api.ModelDetail{Model: m.apiModel(), Digest: m.Digest,
				Preset: map[string]string{}, LogTail: m.LogTail})
			return
		}
	}
	writeError(w, http.StatusNotFound, "unknown model "+name, "taraengine list shows the catalog")
}

// apiModel is the spec as /api/models shows it; the engine's window for the model is its native one.
func (m ModelSpec) apiModel() api.Model {
	status, caps := m.Status, m.Capabilities
	if status == "" {
		status = "pulled"
	}
	if caps == nil {
		caps = []string{}
	}
	return api.Model{Name: m.Name, Status: status, Size: m.Size, Family: m.Family, Params: m.ParameterSize,
		Quantization: m.Quantization, Context: m.ContextLength, NativeContext: m.ContextLength, Capabilities: caps,
		Tier: m.Tier, Default: m.Default, ExitCode: m.ExitCode}
}

func (s *Server) loaded(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	out := make([]api.Loaded, 0, len(s.Loaded))
	for _, l := range s.Loaded {
		entry := api.Loaded{Name: l.Name, Context: l.ContextLength}
		if l.Source != "none" {
			bytes, source := l.Size, l.Source
			if bytes == 0 {
				bytes = l.SizeVRAM
			}
			if source == "" {
				source = "measured"
			}
			entry.Memory = &api.Memory{Bytes: bytes, Source: source}
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

// load records the request and answers as the engine does: a pulled model loads at the window asked for, a faulty
// one only with force, and a name the engine does not serve is a 404 with the engine's hint.
func (s *Server) load(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	req := LoadRequest{}
	req.Model, _ = body["model"].(string)
	if c, ok := body["context"].(float64); ok {
		req.Context = int(c)
	}
	req.Force, _ = body["force"].(bool)
	s.mu.Lock()
	s.Loads = append(s.Loads, req)
	s.mu.Unlock()
	for _, m := range s.Models {
		switch {
		case m.Name != req.Model || m.Status == "available":
			continue
		case m.Status == "faulty" && !req.Force:
			writeError(w, http.StatusConflict, "model is faulty: "+req.Model, "load it with force to try again")
		default:
			loaded := m.apiModel()
			loaded.Status, loaded.Context = "loaded", req.Context
			writeJSON(w, http.StatusOK, loaded)
		}
		return
	}
	writeError(w, http.StatusNotFound, "unknown model: "+req.Model, "taraengine list shows the models the engine knows")
}

func (s *Server) unload(w http.ResponseWriter, _ *http.Request, body map[string]any) {
	name, _ := body["model"].(string)
	s.mu.Lock()
	s.Unloaded = append(s.Unloaded, name)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) doctor(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
	writeJSON(w, http.StatusOK, s.Doctor)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers the control API's error body.
func writeError(w http.ResponseWriter, status int, message, hint string) {
	writeJSON(w, status, api.Error{Error: api.ErrorBody{Message: message, Hint: hint}})
}
