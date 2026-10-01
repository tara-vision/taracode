package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeServer is an OpenAI-compatible or Ollama-like server that answers each path from routes; a
// path it does not know is a 404. It remembers the Authorization header of every request.
type fakeServer struct {
	URL   string
	mu    sync.Mutex
	auth  []string
	paths []string
}

func newFakeServer(t *testing.T, routes map[string]func(http.ResponseWriter, *http.Request)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		if route, ok := routes[r.URL.Path]; ok {
			route(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

func (f *fakeServer) seen() (auth, paths []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...), append([]string(nil), f.paths...)
}

func answer(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// droppingServer accepts each connection and closes it without an answer, so every request fails.
func droppingServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// badHost cannot be parsed as a URL, so no request is ever made to it.
const badHost = "http://bad host"

const twoModels = `{"data":[{"id":"qwen3.5:9b"},{"id":"gemma4:12b"}]}`

func TestDetectModelsOpenAIListsTheServedModels(t *testing.T) {
	srv := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){"/v1/models": answer(200, twoModels)})
	p := NewVLLMProvider(srv.URL+"/", "secret")
	models, err := p.DetectModels(context.Background())
	if err != nil || strings.Join(models, ",") != "qwen3.5:9b,gemma4:12b" {
		t.Fatalf("models %v, err %v", models, err)
	}
	if strings.Join(p.Info().Models, ",") != "qwen3.5:9b,gemma4:12b" {
		t.Fatalf("the provider keeps the list: %v", p.Info().Models)
	}
	if auth, _ := srv.seen(); len(auth) != 1 || auth[0] != "Bearer secret" {
		t.Fatalf("authorization %q", auth)
	}

	keyless := NewLlamaCppProvider(srv.URL, "")
	if _, err := keyless.DetectModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth, _ := srv.seen(); auth[1] != "" {
		t.Fatalf("no key, no authorization header: %q", auth[1])
	}
}

func TestDetectModelsOpenAIReportsFailures(t *testing.T) {
	broken := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/models": answer(200, `{"data": [`),
	})
	down := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/models": answer(503, `{}`),
	})
	tests := []struct {
		host, want string
	}{
		{badHost, "create request: "},
		{droppingServer(t), "request failed: "},
		{down.URL, "unexpected status: 503"},
		{broken.URL, "decode response: "},
	}
	for _, tt := range tests {
		p := NewVLLMProvider(tt.host, "")
		models, err := p.DetectModelsOpenAI(context.Background())
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) || models != nil {
			t.Errorf("%s: models %v, err %v; want %q", tt.host, models, err, tt.want)
		}
	}
}

// TestCreateClientTalksToTheV1API: the go-openai client of every provider type sends the key to
// the host's /v1 API.
func TestCreateClientTalksToTheV1API(t *testing.T) {
	srv := newFakeServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/models": answer(200, `{"object":"list","data":[{"id":"m1","object":"model"}]}`),
	})
	for _, p := range []Provider{NewVLLMProvider(srv.URL, "k"), NewLlamaCppProvider(srv.URL, "k"), NewOllamaProvider(srv.URL, "k")} {
		list, err := p.CreateClient().ListModels(context.Background())
		if err != nil || len(list.Models) != 1 || list.Models[0].ID != "m1" {
			t.Fatalf("%s: %+v %v", p.Info().Name, list, err)
		}
	}
	auth, paths := srv.seen()
	for i := range paths {
		if paths[i] != "/v1/models" || auth[i] != "Bearer k" {
			t.Fatalf("request %d: %s %q", i, paths[i], auth[i])
		}
	}
}

func TestEveryProviderDescribesItself(t *testing.T) {
	tests := []struct {
		p     Provider
		typ   Type
		name  string
		tools bool
	}{
		{NewVLLMProvider("http://h:8000/", ""), TypeVLLM, "vLLM", true},
		{NewLlamaCppProvider("http://h:8000/", ""), TypeLlamaCpp, "llama.cpp", false},
		{NewOllamaProvider("http://h:8000/", ""), TypeOllama, "Ollama", false},
	}
	for _, tt := range tests {
		tt.p.SetModel("m:1b")
		info := tt.p.Info()
		if info.Type != tt.typ || info.Name != tt.name || info.SupportsTools != tt.tools || info.Host != "http://h:8000" ||
			info.APIPath != "/v1" || info.Model != "m:1b" {
			t.Errorf("%+v", info)
		}
	}
}
