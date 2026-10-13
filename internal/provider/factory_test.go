package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

func TestNewAndNewWithTypeNeedAHost(t *testing.T) {
	if _, err := New(context.Background(), "", "ollama", ""); err == nil || err.Error() != "host is required" {
		t.Fatalf("New: %v", err)
	}
	if _, err := NewWithType(TypeOllama, "", ""); err == nil || err.Error() != "host is required" {
		t.Fatalf("NewWithType: %v", err)
	}
}

// TestNewBuildsTheVendorsProvider: a vendor in the configuration decides the type without any
// request (the host does not exist).
func TestNewBuildsTheVendorsProvider(t *testing.T) {
	const host = "http://gpu-box.invalid:1"
	tests := []struct {
		vendor string
		want   Type
	}{
		{"ollama", TypeOllama}, {"llama.cpp", TypeLlamaCpp}, {"vllm", TypeVLLM}, {"taraengine", TypeTaraengine},
	}
	for _, tt := range tests {
		p, err := New(context.Background(), host, tt.vendor, "k")
		if err != nil || p.Info().Type != tt.want {
			t.Errorf("New(%q) = %v, %v", tt.vendor, p, err)
		}
	}
	if _, err := New(context.Background(), "http://my-ollama.invalid", "auto", ""); err == nil {
		t.Fatal("a host that does not resolve: no error")
	}
	srv := taraenginetest.New(t)
	if p, err := New(context.Background(), srv.URL, "auto", ""); err != nil || p.Info().Type != TypeTaraengine {
		t.Fatalf("auto-detecting the engine: %v %v", p, err)
	}
}

func TestNewFallsBackToTheOpenAIAPI(t *testing.T) {
	srv := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){})
	p, err := New(context.Background(), srv.URL, "", "")
	if err != nil || p.Info().Type != TypeVLLM {
		t.Fatalf("an unrecognised server is used through the OpenAI API: %v, %v", p, err)
	}
	if _, paths := srv.seen(); len(paths) != 3 || paths[0] != "/api/health" || paths[1] != "/api/version" ||
		paths[2] != "/v1/models" {
		t.Fatalf("probes %v", paths)
	}
}

func TestNewWithTypeBuildsThatType(t *testing.T) {
	tests := map[Type]Type{TypeTaraengine: TypeTaraengine, TypeOllama: TypeOllama, TypeLlamaCpp: TypeLlamaCpp,
		TypeVLLM: TypeVLLM, TypeUnknown: TypeVLLM}
	for typ, want := range tests {
		p, err := NewWithType(typ, "http://h.invalid", "")
		if err != nil || p.Info().Type != want {
			t.Errorf("NewWithType(%s) = %v, %v", typ, p, err)
		}
	}
}
