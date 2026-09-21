// Package llm is the transport boundary between the agent loop and a model server.
// Messages and tool schemas reuse go-openai structs as a neutral data model; everything
// taracode controls about a request lives in Options.
package llm

import (
	"context"
	"encoding/json"
	"errors"

	openai "github.com/sashabaranov/go-openai"
)

// ErrNotSupported is returned by backends that cannot answer a call (for example Show on vLLM).
var ErrNotSupported = errors.New("llm: not supported by this backend")

// Think selects the model's reasoning mode. ThinkAuto sends nothing and keeps the model default.
type Think string

// The Think values a Request can carry; ThinkAuto sends nothing and keeps the model default.
const (
	ThinkAuto   Think = ""
	ThinkOff    Think = "off"
	ThinkOn     Think = "on"
	ThinkLow    Think = "low"
	ThinkMedium Think = "medium"
	ThinkHigh   Think = "high"
)

// ParseThink accepts the config and /think spellings.
func ParseThink(s string) (Think, bool) {
	switch s {
	case "", "auto":
		return ThinkAuto, true
	case "off", "false", "no":
		return ThinkOff, true
	case "on", "true", "yes":
		return ThinkOn, true
	case "low", "medium", "high":
		return Think(s), true
	}
	return ThinkAuto, false
}

// Options carries everything taracode controls about a request.
type Options struct {
	NumCtx      int             // context window; 0 = server default
	KeepAlive   string          // "" = server default, "-1" = keep loaded
	Think       Think           // reasoning mode
	Format      json.RawMessage // JSON schema for structured output, nil = free text
	Temperature *float32
	TopP        *float32
	NumPredict  int // max tokens per reply; 0 = model default
}

// Request is one chat completion.
type Request struct {
	Model    string
	Messages []openai.ChatCompletionMessage
	Tools    []openai.Tool
	Options  Options
}

// EventKind tags streamed events.
type EventKind int

// The EventKind values a streamed Event can carry.
const (
	EventText     EventKind = iota // visible answer text delta
	EventThinking                  // reasoning text delta, shown dimmed, never stored
	EventToolCall                  // one complete tool call
	EventUsage                     // token usage, sent once at the end
)

// Event is one streamed increment.
type Event struct {
	Kind     EventKind
	Text     string
	ToolCall *openai.ToolCall
	Usage    *Usage
}

// Usage is the token accounting of one request.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Result is the assembled reply of one request.
type Result struct {
	Content    string
	Thinking   string
	ToolCalls  []openai.ToolCall
	Usage      Usage
	DoneReason string
}

// ModelInfo is one entry of the server's model list.
type ModelInfo struct {
	Name          string
	Size          int64
	Family        string
	ParameterSize string
	Quantization  string
}

// ModelDetails is what the server knows about one model.
type ModelDetails struct {
	Capabilities  []string // "completion", "tools", "thinking", "vision"
	ContextLength int      // native maximum, 0 = unknown
	Family        string
	ParameterSize string
}

// Has reports whether the model advertises a capability.
func (d *ModelDetails) Has(capability string) bool {
	if d == nil {
		return false
	}
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// LoadedModel is one entry of the server's loaded-model list.
type LoadedModel struct {
	Name          string
	ContextLength int
	SizeVRAM      int64
}

// Client is implemented by every backend.
type Client interface {
	// Chat runs one completion. When onEvent is non-nil the reply is streamed through it and the
	// assembled Result is returned as well; when nil the backend may skip streaming.
	Chat(ctx context.Context, req Request, onEvent func(Event) error) (*Result, error)
	Models(ctx context.Context) ([]ModelInfo, error)
	Show(ctx context.Context, model string) (*ModelDetails, error)
	Loaded(ctx context.Context) ([]LoadedModel, error)
	Version(ctx context.Context) (string, error)
	Unload(ctx context.Context, model string) error
}
