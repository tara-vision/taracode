package provider

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// LlamaCppProvider implements Provider for llama.cpp servers (llama-server)
type LlamaCppProvider struct {
	*BaseProvider
}

// NewLlamaCppProvider creates a new llama.cpp provider
func NewLlamaCppProvider(host, apiKey string) *LlamaCppProvider {
	base := NewBaseProvider(TypeLlamaCpp, host, apiKey)
	// llama.cpp doesn't support tool calling
	base.info.SupportsTools = false
	return &LlamaCppProvider{BaseProvider: base}
}

// Info returns provider metadata
func (p *LlamaCppProvider) Info() *Info {
	return p.BaseProvider.Info()
}

// DetectModels queries available models from the llama.cpp server
// llama.cpp typically serves a single model, but supports /v1/models endpoint
func (p *LlamaCppProvider) DetectModels(ctx context.Context) ([]string, error) {
	return p.DetectModelsOpenAI(ctx)
}

// CreateClient returns an OpenAI-compatible client
func (p *LlamaCppProvider) CreateClient() *openai.Client {
	return p.BaseProvider.CreateClient()
}

// SetModel sets the active model
func (p *LlamaCppProvider) SetModel(model string) {
	p.BaseProvider.SetModel(model)
}
