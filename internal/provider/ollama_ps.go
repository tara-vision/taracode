package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ContextReporter is implemented by providers that can report the context window
// a loaded model is actually running with.
type ContextReporter interface {
	// LoadedContextLength returns the context window for a loaded model, or 0 when
	// the model is not loaded.
	LoadedContextLength(ctx context.Context, model string) (int, error)
}

type psResponse struct {
	Models []struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		ContextLength int    `json:"context_length"`
	} `json:"models"`
}

// LoadedContextLength reads GET /api/ps and returns the context window (num_ctx)
// Ollama is running the given model with. It returns 0 when the model is not loaded.
func (p *OllamaProvider) LoadedContextLength(ctx context.Context, model string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.info.Host+"/api/ps", nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var ps psResponse
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	for _, m := range ps.Models {
		if m.Name == model || m.Model == model {
			return m.ContextLength, nil
		}
	}
	return 0, nil
}
