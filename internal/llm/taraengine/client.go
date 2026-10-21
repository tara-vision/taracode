package taraengine

import (
	"context"
	"net/http"
	"strings"

	"github.com/tara-vision/taracode/internal/llm"
)

// The engine's words for a model's state and a memory figure's origin.
const (
	statusAvailable = "available" // in the catalog, not pulled
	sourceMeasured  = "measured"  // measured on the machine, not estimated
)

// Client is taracode's llm.Client for one engine: chat over /v1 (chat.go), everything else through Engine.
type Client struct {
	engine *Engine
	host   string
	key    string
	http   *http.Client
}

var (
	_ llm.Client = (*Client)(nil)
	_ llm.Loader = (*Client)(nil)
)

// New creates a client for the engine at host (for example http://127.0.0.1:8272), sending key as a Bearer token when
// it is set. A nil httpClient means http.DefaultClient; one client carries both /v1 and /api.
func New(host, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	host = strings.TrimRight(host, "/")
	return &Client{engine: newEngine(host, key, httpClient), host: host, key: key, http: httpClient}
}

// Engine is the control API, for doctor and the evals.
func (c *Client) Engine() *Engine { return c.engine }

// Models implements llm.Client: the pulled models, from one /api/models call.
func (c *Client) Models(ctx context.Context) ([]llm.ModelInfo, error) {
	list, err := c.engine.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]llm.ModelInfo, 0, len(list))
	for _, m := range list {
		if m.Status == statusAvailable {
			continue
		}
		out = append(out, llm.ModelInfo{Name: m.Name, Size: m.Size, Family: m.Family, ParameterSize: m.Params,
			Quantization: m.Quantization})
	}
	return out, nil
}

// Show implements llm.Client through /api/models/{name}. The engine answers a catalog model that is not pulled with
// the status available, which Show turns into the not-pulled refusal. An import's capabilities are inferred while it
// is loaded and listed as none before that, so Show reports them unknown then.
func (c *Client) Show(ctx context.Context, model string) (*llm.ModelDetails, error) {
	d, err := c.engine.Model(ctx, model)
	if err != nil {
		return nil, err
	}
	if d.Status == statusAvailable {
		return nil, &llm.ServerError{Status: http.StatusNotFound, Message: model + " is not pulled", Model: model}
	}
	details := &llm.ModelDetails{Capabilities: d.Capabilities, ContextLength: d.NativeContext, Family: d.Family,
		ParameterSize: d.Params, Quantization: d.Quantization, Digest: d.Digest, Origin: d.Origin,
		CapabilitiesUnknown: d.CapabilitiesInferred && len(d.Capabilities) == 0}
	if s := d.Source; s != nil {
		details.Source = &llm.ModelSource{Repo: s.Repo, File: s.File, Path: s.Path, SHA256: s.SHA256}
	}
	return details, nil
}

// Loaded implements llm.Client through /api/loaded. The engine reports one memory figure per loaded model; every
// preset offloads all layers, so all of it is on the GPU and Size and SizeVRAM both carry it.
func (c *Client) Loaded(ctx context.Context) ([]llm.LoadedModel, error) {
	list, err := c.engine.Loaded(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]llm.LoadedModel, 0, len(list))
	for _, l := range list {
		m := llm.LoadedModel{Name: l.Name, ContextLength: l.Context}
		if l.Memory != nil {
			m.Size, m.SizeVRAM, m.Measured = l.Memory.Bytes, l.Memory.Bytes, l.Memory.Source == sourceMeasured
		}
		out = append(out, m)
	}
	return out, nil
}

// Version implements llm.Client: the engine's own version.
func (c *Client) Version(ctx context.Context) (string, error) {
	v, err := c.engine.Version(ctx)
	return v.Version, err
}

// Unload implements llm.Client.
func (c *Client) Unload(ctx context.Context, model string) error { return c.engine.Unload(ctx, model) }

// Load implements llm.Loader: the engine loads model at window and remembers that window for it.
func (c *Client) Load(ctx context.Context, model string, window int) error {
	_, err := c.engine.Load(ctx, model, window)
	return err
}
