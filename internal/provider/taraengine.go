package provider

import (
	"context"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// TaraengineProvider is taracode's native backend: the taraengine client for chat, the model list and the model
// switch.
type TaraengineProvider struct {
	*BaseProvider
	client *taraengine.Client
}

var (
	_ Provider     = (*TaraengineProvider)(nil)
	_ ModelManager = (*TaraengineProvider)(nil)
)

// NewTaraengineProvider creates the provider for the engine at host.
func NewTaraengineProvider(host, apiKey string) *TaraengineProvider {
	base := NewBaseProvider(TypeTaraengine, host, apiKey)
	base.info.SupportsTools = true
	return &TaraengineProvider{BaseProvider: base, client: taraengine.New(base.info.Host, apiKey, base.httpClient)}
}

// LLM returns the engine client.
func (p *TaraengineProvider) LLM() llm.Client { return p.client }

// DetectModels lists the pulled models.
func (p *TaraengineProvider) DetectModels(ctx context.Context) ([]string, error) {
	infos, err := p.client.Models(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for _, m := range infos {
		names = append(names, m.Name)
	}
	p.info.Models = names
	return names, nil
}

// ListModels returns the pulled models for the model picker.
func (p *TaraengineProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	infos, err := p.client.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(infos))
	for _, m := range infos {
		out = append(out, ModelInfo{Name: m.Name, Size: m.Size, Family: m.Family, Params: m.ParameterSize})
	}
	return out, nil
}

// UnloadModel asks the engine to unload a model.
func (p *TaraengineProvider) UnloadModel(ctx context.Context, model string) error {
	return p.client.Unload(ctx, model)
}
