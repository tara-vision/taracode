package provider

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// Detect identifies the provider type from host URL
// It first checks URL patterns, then probes endpoints if needed
func Detect(ctx context.Context, host string) Type {
	// Normalize host
	host = strings.TrimSuffix(host, "/")
	hostLower := strings.ToLower(host)

	// 1. Check URL patterns first (fast path)
	if strings.Contains(hostLower, "ollama") {
		return TypeOllama
	}
	if strings.Contains(hostLower, "vllm") {
		return TypeVLLM
	}
	if strings.Contains(hostLower, "llama") && !strings.Contains(hostLower, "ollama") {
		return TypeLlamaCpp
	}

	// 2. Probe endpoints to detect (slower path)
	// Ollama has a unique /api/tags endpoint
	if probeEndpoint(ctx, host, "/api/tags") {
		return TypeOllama
	}

	// If /v1/models works, assume vLLM-compatible
	if probeEndpoint(ctx, host, "/v1/models") {
		return TypeVLLM
	}

	return TypeUnknown
}

// probeEndpoint checks if an endpoint responds successfully
func probeEndpoint(ctx context.Context, host, path string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 3 * time.Second,
			}).DialContext,
		},
	}

	url := strings.TrimSuffix(host, "/") + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider 2xx and some 4xx as "endpoint exists"
	// 404 means endpoint doesn't exist, but 401/403 means it exists but needs auth
	return resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != 404
}

// ParseVendorConfig parses a vendor string from config into a Type
// Returns TypeUnknown if the vendor should be auto-detected
func ParseVendorConfig(vendor string) Type {
	vendor = strings.ToLower(strings.TrimSpace(vendor))

	switch vendor {
	case "vllm":
		return TypeVLLM
	case "ollama":
		return TypeOllama
	case "llama.cpp", "llamacpp", "llama":
		return TypeLlamaCpp
	case "", "auto":
		return TypeUnknown // Will trigger auto-detection
	default:
		return TypeUnknown
	}
}
