package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/search"
)

func TestWebFetchRefusesLocalAddressesAndExtractsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>T</title><style>p{}</style><script>x()</script></head><body><h1>Hello</h1><p>World &amp; friends</p></body></html>`))
	}))
	defer srv.Close()
	tool := WebFetchTool()
	if _, err := tool.Run(context.Background(), map[string]any{"url": srv.URL}, ""); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("loopback must be refused: %v", err)
	}
	allowLocalFetch = true
	defer func() { allowLocalFetch = false }()
	out, err := tool.Run(context.Background(), map[string]any{"url": srv.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "World & friends") || strings.Contains(out, "x()") || strings.Contains(out, "p{}") {
		t.Errorf("extracted %q", out)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"url": "ftp://x"}, ""); err == nil {
		t.Error("only http and https")
	}
	inv := tool.Classify(map[string]any{"url": "https://api.example.com/v1"}, "")
	if inv.Classification != policy.Read || len(inv.Targets.Hosts) != 1 || inv.Targets.Hosts[0] != "api.example.com" {
		t.Errorf("%+v", inv)
	}
}

func TestWebSearchWithoutAnOrchestrator(t *testing.T) {
	tool := WebSearchTool(nil)
	if _, err := tool.Run(context.Background(), map[string]any{"query": "x"}, ""); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("%v", err)
	}
	if !tool.External || !WebFetchTool().External {
		t.Error("web tools are external")
	}
}

// TestWebSearchThroughALocalSearXNGServer exercises WebSearchTool's success path end to end through a
// real search.Orchestrator pointed at a local SearXNG-shaped server: the provider header, the numbered
// results, the instant-answer branch and the "No results." branch.
func TestWebSearchThroughALocalSearXNGServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("q") {
		case "answer":
			_, _ = w.Write([]byte(`{"query":"answer","number_of_results":1,"results":[` +
				`{"url":"https://example.com/a","title":"A","content":"snippet a","engine":"e"}],"answers":["42"]}`))
		case "empty":
			_, _ = w.Write([]byte(`{"query":"empty","number_of_results":0,"results":[]}`))
		default:
			_, _ = w.Write([]byte(`{"query":"go","number_of_results":2,"results":[` +
				`{"url":"https://example.com/1","title":"One","content":"first snippet","engine":"e1"},` +
				`{"url":"https://example.com/2","title":"Two","content":"second snippet","engine":"e2"}]}`))
		}
	}))
	defer srv.Close()
	orch := search.NewOrchestrator(search.OrchestratorConfig{
		Primary: "searxng", CustomSearXNGInstance: srv.URL, Timeout: 5 * time.Second, RetryCount: 0,
	})
	tool := WebSearchTool(orch)

	out, err := tool.Run(context.Background(), map[string]any{"query": "go", "max": 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "SearXNG") {
		t.Errorf("missing provider header: %q", out)
	}
	if !strings.Contains(out, "1. One") || !strings.Contains(out, "https://example.com/1") || !strings.Contains(out, "first snippet") {
		t.Errorf("missing first result: %q", out)
	}
	if !strings.Contains(out, "2. Two") || !strings.Contains(out, "https://example.com/2") || !strings.Contains(out, "second snippet") {
		t.Errorf("missing second result: %q", out)
	}

	out, err = tool.Run(context.Background(), map[string]any{"query": "answer"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Answer: 42") {
		t.Errorf("missing instant answer: %q", out)
	}

	out, err = tool.Run(context.Background(), map[string]any{"query": "empty"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No results.") {
		t.Errorf("expected No results.: %q", out)
	}
}
