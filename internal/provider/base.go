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

	"github.com/tara-vision/taracode/internal/llm"
	openaiclient "github.com/tara-vision/taracode/internal/llm/openai"
)

const (
	// defaultConnectTimeout bounds a dial to an OpenAI-compatible server.
	defaultConnectTimeout = 10 * time.Second
	// engineConnectTimeout bounds a dial to the engine below the session's 5 s bound on reading a model, so a host that
	// never answers reads as nothing answering there, not as a call that ran out of time. Only the dial is bounded: a
	// load still runs under its 15-minute backstop.
	engineConnectTimeout = 3 * time.Second
)

// dial connects every HTTP client of this package; each client bounds it with a timeout of its own (boundedDial).
// Tests replace it with a dial that never connects.
var dial = (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext

// boundedDial is dial with a deadline of its own. net/http dials apart from the request's context, so a request whose
// time runs out while the dial still waits reports its own deadline, not that nothing answered: only a dial bound
// below the caller's deadline turns a host that never answers into a dial error.
func boundedDial(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return dial(ctx, network, addr)
	}
}

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
		httpClient: newHTTPClient(defaultConnectTimeout),
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

// LLM returns the go-openai adapter; taraengine overrides this with its native client.
func (p *BaseProvider) LLM() llm.Client {
	return openaiclient.New(p.CreateClient())
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

// newHTTPClient creates an HTTP client for LLM API requests whose dials give up after connectTimeout.
// Client-level timeout is disabled (0) to allow long-running streaming responses.
// Timeout should be controlled via context instead.
func newHTTPClient(connectTimeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: 0, // Disabled - use context timeout for streaming
		Transport: &http.Transport{
			DialContext:         boundedDial(connectTimeout),
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}
