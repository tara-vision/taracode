package provider

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// Type represents the LLM provider type
type Type string

const (
	TypeVLLM     Type = "vllm"
	TypeOllama   Type = "ollama"
	TypeLlamaCpp Type = "llama.cpp"
	TypeUnknown  Type = "unknown"
)

// String returns the string representation of the provider type
func (t Type) String() string {
	return string(t)
}

// DisplayName returns a human-readable name for the provider type
func (t Type) DisplayName() string {
	switch t {
	case TypeVLLM:
		return "vLLM"
	case TypeOllama:
		return "Ollama"
	case TypeLlamaCpp:
		return "llama.cpp"
	default:
		return "Unknown"
	}
}

// Info holds provider metadata
type Info struct {
	Type          Type     // Provider type (vllm, ollama, llama.cpp)
	Name          string   // Display name (e.g., "Ollama")
	Host          string   // Base URL
	Model         string   // Selected model
	Models        []string // Available models
	APIPath       string   // API path prefix (e.g., "/v1")
	SupportsTools bool     // Whether provider supports tool calling
}

// ModelInfo holds detailed information about a model
type ModelInfo struct {
	Name   string // Model name (e.g., "gemma3:27b")
	Size   int64  // Model size in bytes
	Family string // Model family (e.g., "gemma3")
	Params string // Parameter size (e.g., "27B")
}

// FormatSize returns a human-readable size string
func (m ModelInfo) FormatSize() string {
	gb := float64(m.Size) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.1fGB", gb)
}

// Provider interface for LLM operations
type Provider interface {
	// Info returns provider metadata
	Info() *Info

	// DetectModels queries available models from the server
	DetectModels(ctx context.Context) ([]string, error)

	// CreateClient returns an OpenAI-compatible client
	CreateClient() *openai.Client

	// SetModel sets the active model
	SetModel(model string)
}

// ModelManager interface for providers that support model management (Ollama)
type ModelManager interface {
	// ListModels returns detailed info about available models
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// UnloadModel unloads a model from memory
	UnloadModel(ctx context.Context, model string) error
}
