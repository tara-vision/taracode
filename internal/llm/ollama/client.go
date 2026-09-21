// Package ollama is taracode's native Ollama client: /api/chat with NDJSON streaming plus the
// model endpoints. It deliberately has no dependency on the ollama module.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tara-vision/taracode/internal/llm"
)

// Client talks to one Ollama server.
type Client struct {
	host string
	http *http.Client
}

// New creates a client for host (for example http://localhost:11434).
func New(host string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{host: strings.TrimRight(host, "/"), http: httpClient}
}

var _ llm.Client = (*Client)(nil)

// Chat implements llm.Client.
func (c *Client) Chat(ctx context.Context, req llm.Request, onEvent func(llm.Event) error) (*llm.Result, error) {
	stream := onEvent != nil
	resp, err := c.post(ctx, "/api/chat", toWire(req, stream))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if stream {
		return readStream(resp.Body, onEvent)
	}
	var chunk chatChunk
	if err := json.NewDecoder(resp.Body).Decode(&chunk); err != nil {
		return nil, fmt.Errorf("ollama: decode reply: %w", err)
	}
	return assemble([]chatChunk{chunk}), nil
}

// Models implements llm.Client via GET /api/tags.
func (c *Client) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	var body struct {
		Models []struct {
			Name    string `json:"name"`
			Size    int64  `json:"size"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := c.getJSON(ctx, "/api/tags", &body); err != nil {
		return nil, err
	}
	out := make([]llm.ModelInfo, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, llm.ModelInfo{
			Name:          m.Name,
			Size:          m.Size,
			Family:        m.Details.Family,
			ParameterSize: m.Details.ParameterSize,
			Quantization:  m.Details.Quantization,
		})
	}
	return out, nil
}

// Show implements llm.Client via POST /api/show.
func (c *Client) Show(ctx context.Context, model string) (*llm.ModelDetails, error) {
	resp, err := c.post(ctx, "/api/show", map[string]string{"model": model})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Capabilities []string `json:"capabilities"`
		Details      struct {
			Family        string `json:"family"`
			ParameterSize string `json:"parameter_size"`
		} `json:"details"`
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ollama: decode show: %w", err)
	}
	d := &llm.ModelDetails{
		Capabilities:  body.Capabilities,
		Family:        body.Details.Family,
		ParameterSize: body.Details.ParameterSize,
	}
	for k, v := range body.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if f, ok := v.(float64); ok {
				d.ContextLength = int(f)
			}
		}
	}
	return d, nil
}

// Loaded implements llm.Client via GET /api/ps.
func (c *Client) Loaded(ctx context.Context) ([]llm.LoadedModel, error) {
	var body struct {
		Models []struct {
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			SizeVRAM      int64  `json:"size_vram"`
		} `json:"models"`
	}
	if err := c.getJSON(ctx, "/api/ps", &body); err != nil {
		return nil, err
	}
	out := make([]llm.LoadedModel, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, llm.LoadedModel{Name: m.Name, ContextLength: m.ContextLength, SizeVRAM: m.SizeVRAM})
	}
	return out, nil
}

// Version implements llm.Client via GET /api/version.
func (c *Client) Version(ctx context.Context) (string, error) {
	var body struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(ctx, "/api/version", &body); err != nil {
		return "", err
	}
	return body.Version, nil
}

// Unload implements llm.Client: POST /api/generate with keep_alive 0.
func (c *Client) Unload(ctx context.Context, model string) error {
	resp, err := c.post(ctx, "/api/generate", map[string]any{"model": model, "keep_alive": 0})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) post(ctx context.Context, path string, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ollama: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, statusError(path, resp)
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return fmt.Errorf("ollama: create request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return statusError(path, resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("ollama: decode %s: %w", path, err)
	}
	return nil
}

// statusError turns a non-200 reply into an error carrying the status and Ollama's message.
func statusError(path string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	msg := body.Error
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	return fmt.Errorf("ollama: %s returned %d: %s", path, resp.StatusCode, msg)
}
