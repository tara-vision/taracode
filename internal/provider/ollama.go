package provider

import "context"

// OllamaProvider runs an Ollama server on the generic OpenAI-compatible path: taracode has no window control, no
// thinking on or off, no /model and no evals there, and says so once per session (ui's ProviderNotice). taraengine
// is the native backend.
type OllamaProvider struct {
	*BaseProvider
}

var _ Provider = (*OllamaProvider)(nil)

// NewOllamaProvider creates the provider for an Ollama server at host.
func NewOllamaProvider(host, apiKey string) *OllamaProvider {
	base := NewBaseProvider(TypeOllama, host, apiKey)
	base.info.SupportsTools = true
	return &OllamaProvider{BaseProvider: base}
}

// DetectModels lists the models Ollama serves on its OpenAI-compatible API.
func (p *OllamaProvider) DetectModels(ctx context.Context) ([]string, error) {
	return p.DetectModelsOpenAI(ctx)
}
