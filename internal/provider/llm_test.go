package provider

import (
	"context"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestOllamaProviderLLMIsNative(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"tools"}, ContextLength: 4096, Family: "gemma4"}}
	p := NewOllamaProvider(srv.URL, "")
	details, err := p.LLM().Show(context.Background(), "gemma4:12b")
	if err != nil || !details.Has("tools") {
		t.Fatalf("native Show failed: %+v %v", details, err)
	}
	if srv.Requests[len(srv.Requests)-1].Path != "/api/show" {
		t.Fatalf("expected the native /api/show, got %s", srv.Requests[len(srv.Requests)-1].Path)
	}
}

func TestVLLMProviderLLMIsAdapter(t *testing.T) {
	p := NewVLLMProvider("http://127.0.0.1:1", "")
	if _, err := p.LLM().Show(context.Background(), "m"); err == nil {
		t.Fatal("adapter Show must not be supported")
	}
}
