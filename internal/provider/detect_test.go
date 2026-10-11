package provider

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func TestDetectFindsTheEngineFirst(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Key = "k" // health needs no key, so a keyed engine is found too
	if got, err := Detect(context.Background(), srv.URL+"/"); err != nil || got != TypeTaraengine {
		t.Fatalf("Detect() = %s, %v", got, err)
	}
}

func TestDetectNeedsTheEnginesHealthAnswer(t *testing.T) {
	type routes = map[string]func(http.ResponseWriter, *http.Request)
	tests := []struct {
		name   string
		routes routes
		want   Type
	}{
		{"a proxy page", routes{"/api/health": answer(200, "<html>welcome</html>"),
			"/v1/models": answer(200, `{"data":[]}`)}, TypeVLLM},
		{"health without a backend", routes{"/api/health": answer(200, `{"status":"ok"}`)}, TypeUnknown},
		{"Ollama", routes{"/api/version": answer(200, `{"version":"0.35.0"}`)}, TypeOllama},
		{"an engine whose health timed out", routes{"/api/version": answer(200,
			`{"version":"0.2.0","runtime":{"build":"b11514"}}`), "/v1/models": answer(401, `{}`)}, TypeVLLM},
		{"a keyed version route", routes{"/api/version": answer(401, `{}`)}, TypeUnknown},
		{"only the OpenAI API", routes{"/v1/models": answer(200, `{"data":[]}`)}, TypeVLLM},
		{"nothing", routes{}, TypeUnknown},
	}
	for _, tt := range tests {
		srv := newFakeServer(t, tt.routes)
		if got, err := Detect(context.Background(), srv.URL); err != nil || got != tt.want {
			t.Errorf("%s: Detect() = %s, %v, want %s", tt.name, got, err, tt.want)
		}
	}
}

// TestDetectTakesTheHostNameAtItsWord: when no engine answers, a host whose name says Ollama, vLLM or llama.cpp is
// that server, with no probe after the engine's. The fake sits behind a path prefix, the way a gateway mounts a server.
func TestDetectTakesTheHostNameAtItsWord(t *testing.T) {
	for prefix, want := range map[string]Type{"/ollama": TypeOllama, "/vllm": TypeVLLM, "/llama": TypeLlamaCpp} {
		srv := newFakeServer(t, nil)
		if got, err := Detect(context.Background(), srv.URL+prefix); err != nil || got != want {
			t.Errorf("Detect(%s) = %s, %v, want %s", prefix, got, err, want)
		}
		if _, paths := srv.seen(); len(paths) != 1 || paths[0] != prefix+"/api/health" {
			t.Errorf("%s: probes %v, want the engine's health probe only", prefix, paths)
		}
	}
}

func TestDetectStopsWhenNothingAnswers(t *testing.T) {
	_, err := Detect(context.Background(), "http://127.0.0.1:1")
	var unreachable *llm.UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Hint != "" || unreachable.Host != "http://127.0.0.1:1" {
		t.Fatalf("a closed port: %#v", err)
	}
	if got, err := Detect(context.Background(), droppingServer(t)); err != nil || got != TypeUnknown {
		t.Fatalf("a server that drops the connection is not nothing: %s, %v", got, err)
	}
}

func TestDetectHintsAtTheDefaultHost(t *testing.T) {
	_, err := Detect(context.Background(), DefaultHost)
	var unreachable *llm.UnreachableError
	if !errors.As(err, &unreachable) {
		t.Skipf("something answers at %s on this machine: %v", DefaultHost, err)
	}
	if unreachable.Hint != taraengine.StartHint {
		t.Fatalf("the default host's hint: %q", unreachable.Hint)
	}
}

func TestTypeFromNameReadsTheHostName(t *testing.T) {
	for host, want := range map[string]Type{
		"http://OLLAMA.internal:11434": TypeOllama, "http://vllm-gpu:8000": TypeVLLM,
		"http://llama-server:8080": TypeLlamaCpp, "http://gpu-box:8000": TypeUnknown,
	} {
		if got := typeFromName(host); got != want {
			t.Errorf("typeFromName(%q) = %s, want %s", host, got, want)
		}
	}
}

func TestProbesNeedAURL(t *testing.T) {
	if probeEndpoint(context.Background(), badHost, "/v1/models") || probeOllama(context.Background(), badHost) {
		t.Fatal("a host that is not a URL has no endpoints")
	}
	if engine, err := probeEngine(context.Background(), badHost); engine || err != nil {
		t.Fatalf("a host that is not a URL is not an engine and not nothing: %v %v", engine, err)
	}
}

func TestParseVendorConfig(t *testing.T) {
	tests := map[string]Type{
		"taraengine": TypeTaraengine, " TaraEngine ": TypeTaraengine,
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
