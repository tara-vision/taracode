package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sashabaranov/go-openai"
)

// OllamaProvider implements Provider for Ollama servers
type OllamaProvider struct {
	*BaseProvider
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(host, apiKey string) *OllamaProvider {
	base := NewBaseProvider(TypeOllama, host, apiKey)
	// Ollama has limited tool calling support (depends on model)
	base.info.SupportsTools = false
	return &OllamaProvider{BaseProvider: base}
}

// Info returns provider metadata
func (p *OllamaProvider) Info() *Info {
	return p.BaseProvider.Info()
}

// DetectModels queries available models from the Ollama server
// Tries OpenAI-compatible endpoint first, falls back to native /api/tags
func (p *OllamaProvider) DetectModels(ctx context.Context) ([]string, error) {
	// Try OpenAI-compatible endpoint first
	models, err := p.DetectModelsOpenAI(ctx)
	if err == nil && len(models) > 0 {
		return models, nil
	}

	// Fall back to native Ollama API
	return p.detectModelsNative(ctx)
}

// detectModelsNative queries the Ollama-specific /api/tags endpoint
func (p *OllamaProvider) detectModelsNative(ctx context.Context) ([]string, error) {
	url := p.info.Host + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Ollama /api/tags response format
	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	models := make([]string, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		models = append(models, m.Name)
	}

	p.info.Models = models
	return models, nil
}

// CreateClient returns an OpenAI-compatible client
func (p *OllamaProvider) CreateClient() *openai.Client {
	return p.BaseProvider.CreateClient()
}

// SetModel sets the active model
func (p *OllamaProvider) SetModel(model string) {
	p.BaseProvider.SetModel(model)
}
