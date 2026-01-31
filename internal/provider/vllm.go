package provider

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// VLLMProvider implements Provider for vLLM servers
type VLLMProvider struct {
	*BaseProvider
}

// NewVLLMProvider creates a new vLLM provider
func NewVLLMProvider(host, apiKey string) *VLLMProvider {
	base := NewBaseProvider(TypeVLLM, host, apiKey)
	base.info.SupportsTools = true // vLLM supports tool calling
	return &VLLMProvider{BaseProvider: base}
}

// Info returns provider metadata
func (p *VLLMProvider) Info() *Info {
	return p.BaseProvider.Info()
}

// DetectModels queries available models from the vLLM server
func (p *VLLMProvider) DetectModels(ctx context.Context) ([]string, error) {
	return p.DetectModelsOpenAI(ctx)
}

// CreateClient returns an OpenAI-compatible client
func (p *VLLMProvider) CreateClient() *openai.Client {
	return p.BaseProvider.CreateClient()
}

// SetModel sets the active model
func (p *VLLMProvider) SetModel(model string) {
	p.BaseProvider.SetModel(model)
}
