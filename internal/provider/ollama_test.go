package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func gemmaServer(t *testing.T) *ollamatest.Server {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Family: "gemma4", ParameterSize: "12B", Size: 3 << 30},
		{Name: "qwen3.5:9b", Family: "qwen3", ParameterSize: "9B", Size: 1 << 30},
	}
	return srv
}

// TestOllamaDetectModelsFallsBackToTags: Ollama's own server has no /v1/models listing here, so the
// models come from /api/tags.
func TestOllamaDetectModelsFallsBackToTags(t *testing.T) {
	srv := gemmaServer(t)
	p := NewOllamaProvider(srv.URL, "")
	models, err := p.DetectModels(context.Background())
	if err != nil || strings.Join(models, ",") != "gemma4:12b,qwen3.5:9b" {
		t.Fatalf("models %v, err %v", models, err)
	}
	if strings.Join(p.Info().Models, ",") != "gemma4:12b,qwen3.5:9b" {
		t.Fatalf("the provider keeps the list: %v", p.Info().Models)
	}
}

func TestOllamaDetectModelsPrefersTheOpenAIList(t *testing.T) {
	srv := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/models": answer(200, twoModels),
		"/api/tags":  answer(200, `{"models":[{"name":"never:1b"}]}`),
	})
	models, err := NewOllamaProvider(srv.URL, "").DetectModels(context.Background())
	if err != nil || strings.Join(models, ",") != "qwen3.5:9b,gemma4:12b" {
		t.Fatalf("models %v, err %v", models, err)
	}
	if _, paths := srv.seen(); len(paths) != 1 {
		t.Fatalf("a non-empty OpenAI list ends the search: %v", paths)
	}

	empty := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/models": answer(200, `{"data":[]}`),
		"/api/tags":  answer(200, `{"models":[{"name":"tagged:1b"}]}`),
	})
	if models, err := NewOllamaProvider(empty.URL, "").DetectModels(context.Background()); err != nil ||
		len(models) != 1 || models[0] != "tagged:1b" {
		t.Fatalf("an empty OpenAI list falls back to the tags: %v %v", models, err)
	}
}

// failingHosts are a host that is not a URL, a server that drops every connection, one that
// answers 500 and one that answers broken JSON, each with the error it must give.
func failingHosts(t *testing.T, path string) []struct{ host, want string } {
	t.Helper()
	down := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){path: answer(500, `{}`)})
	broken := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){path: answer(200, `{"models": [`)})
	return []struct{ host, want string }{
		{badHost, "create request: "},
		{droppingServer(t), "request failed: "},
		{down.URL, "unexpected status: 500"},
		{broken.URL, "decode response: "},
	}
}

func TestOllamaModelListsReportFailures(t *testing.T) {
	for _, tt := range failingHosts(t, "/api/tags") {
		p := NewOllamaProvider(tt.host, "")
		if models, err := p.DetectModels(context.Background()); err == nil || !strings.HasPrefix(err.Error(), tt.want) ||
			models != nil {
			t.Errorf("DetectModels %s: %v %v; want %q", tt.host, models, err, tt.want)
		}
		if models, err := p.ListModels(context.Background()); err == nil || !strings.HasPrefix(err.Error(), tt.want) ||
			models != nil {
			t.Errorf("ListModels %s: %v %v; want %q", tt.host, models, err, tt.want)
		}
	}
}

func TestOllamaListModelsDescribesEachModel(t *testing.T) {
	srv := gemmaServer(t)
	models, err := NewOllamaProvider(srv.URL, "").ListModels(context.Background())
	if err != nil || len(models) != 2 {
		t.Fatalf("models %v, err %v", models, err)
	}
	want := ModelInfo{Name: "gemma4:12b", Size: 3 << 30, Family: "gemma4", Params: "12B"}
	if models[0] != want || models[1].Name != "qwen3.5:9b" || models[1].Params != "9B" {
		t.Fatalf("%+v", models)
	}
}

func TestOllamaUnloadModel(t *testing.T) {
	srv := gemmaServer(t)
	if err := NewOllamaProvider(srv.URL, "").UnloadModel(context.Background(), "gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	if len(srv.Unloaded) != 1 || srv.Unloaded[0] != "gemma4:12b" {
		t.Fatalf("unloaded %v", srv.Unloaded)
	}
	body := srv.Requests[len(srv.Requests)-1].Body
	if keep, ok := body["keep_alive"].(float64); !ok || keep != 0 {
		t.Fatalf("an unload asks for keep_alive 0: %v", body)
	}

	for _, tt := range failingHosts(t, "/api/generate") {
		if tt.want == "decode response: " {
			continue // the answer to an unload is not read
		}
		err := NewOllamaProvider(tt.host, "").UnloadModel(context.Background(), "gemma4:12b")
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: %v; want %q", tt.host, err, tt.want)
		}
	}
}
