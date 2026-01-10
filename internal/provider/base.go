package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

const (
	defaultConnectTimeout = 10 * time.Second
)

// BaseProvider contains common provider functionality
type BaseProvider struct {
	info       *Info
	httpClient *http.Client
	apiKey     string
}

// NewBaseProvider creates a base provider with common setup
func NewBaseProvider(providerType Type, host, apiKey string) *BaseProvider {
	host = strings.TrimSuffix(host, "/")

	return &BaseProvider{
		info: &Info{
			Type:    providerType,
			Name:    providerType.DisplayName(),
			Host:    host,
			APIPath: "/v1",
		},
		httpClient: newHTTPClient(),
		apiKey:     apiKey,
	}
}

// Info returns provider metadata
func (p *BaseProvider) Info() *Info {
	return p.info
}

// SetModel sets the active model
func (p *BaseProvider) SetModel(model string) {
	p.info.Model = model
}

// CreateClient returns an OpenAI-compatible client
func (p *BaseProvider) CreateClient() *openai.Client {
	config := openai.DefaultConfig(p.apiKey)
	config.BaseURL = p.info.Host + p.info.APIPath
	config.HTTPClient = p.httpClient
	return openai.NewClientWithConfig(config)
}

// DetectModelsOpenAI queries the /v1/models endpoint (OpenAI-compatible)
func (p *BaseProvider) DetectModelsOpenAI(ctx context.Context) ([]string, error) {
	url := p.info.Host + "/v1/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	models := make([]string, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, m.ID)
	}

	p.info.Models = models
	return models, nil
}

// newHTTPClient creates an HTTP client for LLM API requests.
// Client-level timeout is disabled (0) to allow long-running streaming responses.
// Timeout should be controlled via context instead.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0, // Disabled - use context timeout for streaming
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   defaultConnectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}
