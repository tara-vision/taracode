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
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:7b","model":"qwen2.5:7b","context_length":32000}]}`))
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "")

	got, err := p.LoadedContextLength(context.Background(), "qwen2.5:7b")
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
	if _, err := p.LoadedContextLength(context.Background(), "qwen2.5:7b"); err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
}
