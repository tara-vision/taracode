package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tara-vision/taraengine/pkg/client"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// DefaultHost is where taracode looks for an engine when no host is configured.
const DefaultHost = "http://127.0.0.1:8272"

const (
	// healthTimeout bounds the engine probe that runs before anything else.
	healthTimeout = 2 * time.Second
	// probeConnectTimeout bounds a probe's dial below healthTimeout, so a host that never answers fails the engine
	// probe as a dial, which stops detection, before the probe's own time runs out.
	probeConnectTimeout = 1500 * time.Millisecond
	// maxProbeBody bounds how much of a probe's answer is read.
	maxProbeBody = 1 << 16
)

// Detect identifies the server at host. A taraengine answers its open GET /api/health, and that probe runs first;
// then the host name decides, then Ollama's /api/version, then the OpenAI API. Nothing answering at all is an
// *llm.UnreachableError, with the start hint when host is the default host.
func Detect(ctx context.Context, host string) (Type, error) {
	host = strings.TrimSuffix(host, "/")
	engine, err := probeEngine(ctx, host)
	switch {
	case err != nil:
		hint := ""
		if host == DefaultHost {
			hint = taraengine.StartHint
		}
		return TypeUnknown, &llm.UnreachableError{Host: host, Hint: hint, Err: err}
	case engine:
		return TypeTaraengine, nil
	}
	if t := typeFromName(host); t != TypeUnknown {
		return t, nil
	}
	if probeOllama(ctx, host) {
		return TypeOllama, nil
	}
	if probeEndpoint(ctx, host, "/v1/models") {
		return TypeVLLM, nil
	}
	return TypeUnknown, nil
}

// typeFromName reads the server type from a host name that names Ollama, vLLM or llama.cpp.
func typeFromName(host string) Type {
	hostLower := strings.ToLower(host)
	switch {
	case strings.Contains(hostLower, "ollama"):
		return TypeOllama
	case strings.Contains(hostLower, "vllm"):
		return TypeVLLM
	case strings.Contains(hostLower, "llama"):
		return TypeLlamaCpp
	}
	return TypeUnknown
}

// probeEngine asks GET /api/health: a 200 whose JSON body names a backend is a taraengine. The error is set only when
// the dial reached nothing; any answer, or a URL that cannot be asked, is just not an engine.
func probeEngine(ctx context.Context, host string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	var health struct {
		Backend string `json:"backend"`
	}
	status, err := probeJSON(ctx, host+"/api/health", &health)
	if client.IsConnectionError(err) {
		return false, err
	}
	return status == http.StatusOK && health.Backend != "", nil
}

// probeOllama is a 200 from GET /api/version whose JSON has a version string and no runtime object: an engine
// without a key whose health probe timed out answers there too, with its runtime.
func probeOllama(ctx context.Context, host string) bool {
	var version struct {
		Version string          `json:"version"`
		Runtime json.RawMessage `json:"runtime"`
	}
	status, _ := probeJSON(ctx, host+"/api/version", &version)
	return status == http.StatusOK && version.Version != "" && len(version.Runtime) == 0
}

// probeJSON GETs url and decodes a JSON answer into out. It returns the status, 0 when no answer came, and the
// error of a request that failed or an answer that did not decode.
func probeJSON(ctx context.Context, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := probeClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, maxProbeBody)).Decode(out)
}

// probeEndpoint checks whether an endpoint exists: any answer from 200 to 499 but 404 (a 401 or 403 exists and
// needs a key).
func probeEndpoint(ctx context.Context, host, path string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(host, "/")+path, nil)
	if err != nil {
		return false
	}
	resp, err := probeClient().Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != http.StatusNotFound
}

// probeClient is the HTTP client of the detection probes, with short timeouts. Each probe builds its own, so it keeps
// no connection alive: nothing would ever reuse or close an idle one.
func probeClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext:       boundedDial(probeConnectTimeout),
			DisableKeepAlives: true,
		},
	}
}

// ParseVendorConfig parses a vendor string from config into a Type. Returns TypeUnknown if the vendor should be
// auto-detected.
func ParseVendorConfig(vendor string) Type {
	switch strings.ToLower(strings.TrimSpace(vendor)) {
	case "taraengine":
		return TypeTaraengine
	case "vllm":
		return TypeVLLM
	case "ollama":
		return TypeOllama
	case "llama.cpp", "llamacpp", "llama":
		return TypeLlamaCpp
	}
	return TypeUnknown // "", "auto" and anything else are auto-detected
}
