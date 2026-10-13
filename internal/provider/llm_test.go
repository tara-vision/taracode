package provider

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
)

func TestOllamaRunsOnTheOpenAIPath(t *testing.T) {
	srv := newFakeServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/v1/models": answer(200, `{"data":[{"id":"gemma4:latest"}]}`),
	})
	p := NewOllamaProvider(srv.URL, "")
	if p.Info().Type != TypeOllama || p.Info().Name != "Ollama" {
		t.Fatalf("info %+v", p.Info())
	}
	if _, err := p.LLM().Show(context.Background(), "gemma4"); !errors.Is(err, llm.ErrNotSupported) {
		t.Fatalf("Show on the OpenAI path: %v", err)
	}
	if names, err := p.DetectModels(context.Background()); err != nil || len(names) != 1 || names[0] != "gemma4:latest" {
		t.Fatalf("DetectModels %v %v", names, err)
	}
	if _, ok := any(p).(ModelManager); ok {
		t.Fatal("Ollama has no model management on the OpenAI path")
	}
}

func TestVLLMProviderLLMIsAdapter(t *testing.T) {
	p := NewVLLMProvider("http://127.0.0.1:1", "")
	if _, err := p.LLM().Show(context.Background(), "m"); err == nil {
		t.Fatal("adapter Show must not be supported")
	}
}
