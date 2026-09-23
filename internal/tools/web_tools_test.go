package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
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
