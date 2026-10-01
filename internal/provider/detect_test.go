package provider

import (
	"context"
	"net/http"
	"testing"
)

// TestDetectReadsTheHostName: a host name that names the server type decides it without a probe
// (these hosts do not exist; a probe would fail and give TypeUnknown).
func TestDetectReadsTheHostName(t *testing.T) {
	tests := []struct {
		host string
		want Type
	}{
		{"http://OLLAMA.internal:11434/", TypeOllama},
		{"http://vllm-gpu.invalid:8000", TypeVLLM},
		{"http://llama-server.invalid:8080", TypeLlamaCpp},
	}
	for _, tt := range tests {
		if got := Detect(context.Background(), tt.host); got != tt.want {
			t.Errorf("Detect(%q) = %s, want %s", tt.host, got, tt.want)
		}
	}
}

// TestDetectProbesTheServer: /api/tags marks Ollama, then /v1/models marks an OpenAI-compatible
// server; an endpoint that answers 401 or 403 exists, one that answers 404 or 5xx does not.
func TestDetectProbesTheServer(t *testing.T) {
	type routes = map[string]func(http.ResponseWriter, *http.Request)
	tests := []struct {
		name   string
		routes routes
		want   Type
	}{
		{"tags answer", routes{"/api/tags": answer(200, `{"models":[]}`)}, TypeOllama},
		{"tags need a key", routes{"/api/tags": answer(401, `{}`)}, TypeOllama},
		{"only the OpenAI API", routes{"/v1/models": answer(200, `{"data":[]}`)}, TypeVLLM},
		{"tags fail, models need a key", routes{"/api/tags": answer(500, `{}`), "/v1/models": answer(403, `{}`)}, TypeVLLM},
		{"nothing", routes{}, TypeUnknown},
	}
	for _, tt := range tests {
		srv := newFakeServer(t, tt.routes)
		if got := Detect(context.Background(), srv.URL+"/"); got != tt.want {
			t.Errorf("%s: Detect() = %s, want %s", tt.name, got, tt.want)
		}
	}
	if got := Detect(context.Background(), droppingServer(t)); got != TypeUnknown {
		t.Errorf("a server that answers nothing: %s", got)
	}
}

func TestProbeEndpointNeedsAURL(t *testing.T) {
	if probeEndpoint(context.Background(), badHost, "/api/tags") {
		t.Fatal("a host that is not a URL has no endpoints")
	}
}

func TestParseVendorConfig(t *testing.T) {
	tests := map[string]Type{
		"vllm": TypeVLLM, " VLLM ": TypeVLLM, "ollama": TypeOllama, "Ollama": TypeOllama,
		"llama.cpp": TypeLlamaCpp, "llamacpp": TypeLlamaCpp, "llama": TypeLlamaCpp,
		"": TypeUnknown, "auto": TypeUnknown, "openai": TypeUnknown,
	}
	for vendor, want := range tests {
		if got := ParseVendorConfig(vendor); got != want {
			t.Errorf("ParseVendorConfig(%q) = %s, want %s", vendor, got, want)
		}
	}
}
