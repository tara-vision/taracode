package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadedContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b","model":"qwen3.8:27b","context_length":32000}]}`))
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "")

	got, err := p.LoadedContextLength(context.Background(), "qwen3.8:27b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 32000 {
		t.Fatalf("context length = %d, want 32000", got)
	}

	notLoaded, err := p.LoadedContextLength(context.Background(), "gemma4:12b")
	if err != nil || notLoaded != 0 {
		t.Fatalf("not loaded model: got %d, %v; want 0, nil", notLoaded, err)
	}
}

func TestLoadedContextLengthServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "")
	if _, err := p.LoadedContextLength(context.Background(), "qwen3.8:27b"); err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
}

func TestLoadedContextLengthUntaggedModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.8:27b","model":"qwen3.8:27b","context_length":40000},{"name":"gemma4:latest","model":"gemma4:latest","context_length":8192}]}`))
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "")

	// A model loaded under a non-latest tag must not match an untagged query: "qwen3.8"
	// looks for "qwen3.8:latest", which is not what is loaded here (different tag semantics
	// do not apply).
	got, err := p.LoadedContextLength(context.Background(), "qwen3.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Fatalf("qwen3.8 context length = %d, want 0 (loaded model is tagged :27b, not :latest)", got)
	}

	// Ollama reports an untagged configured model as "<model>:latest"; an untagged query
	// must match that.
	got, err = p.LoadedContextLength(context.Background(), "gemma4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 8192 {
		t.Fatalf("gemma4 context length = %d, want 8192", got)
	}
}
