package search

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewBraveAppliesTheTimeout(t *testing.T) {
	b := NewBrave("key", WithBraveTimeout(3*time.Second))
	if b.Name() != "Brave" || b.timeout != 3*time.Second || b.httpClient.Timeout != 3*time.Second {
		t.Fatalf("%+v", b)
	}
	if NewBrave("key").httpClient.Timeout != braveDefaultTimeout {
		t.Fatal("the default timeout")
	}
}

func TestBraveSearchReturnsTheWebResults(t *testing.T) {
	web := newFakeWeb(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"query":{"original":"helm rollback"},
			"web":{"results":[{"title":"Helm rollback","url":"https://helm.sh/docs/rollback","description":"Roll back a release"},
			{"title":"History","url":"https://helm.sh/docs/history","description":"Release history"}]},
			"infobox":{"title":"Helm","description":"The package manager for Kubernetes"}}`))
	})
	b := NewBrave("secret-key")
	b.httpClient = web.client()
	resp, err := b.Search(context.Background(), "helm rollback", 2)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Query != "helm rollback" || resp.Provider != "Brave" || resp.InstantAnswer != "The package manager for Kubernetes" ||
		len(resp.Results) != 2 || resp.Results[0] != (SearchResult{Title: "Helm rollback", URL: "https://helm.sh/docs/rollback",
		Snippet: "Roll back a release", Source: "Brave"}) {
		t.Fatalf("response %+v", resp)
	}
	req := web.seen()[0]
	if req.Host != "api.search.brave.com" || req.Path != "/res/v1/web/search" || req.Query.Get("q") != "helm rollback" ||
		req.Query.Get("count") != "2" || req.Query.Get("safesearch") != "off" || req.Query.Get("text_decorations") != "false" ||
		req.Header.Get("X-Subscription-Token") != "secret-key" || req.Header.Get("Accept") != "application/json" {
		t.Fatalf("request %+v", req)
	}
}

func TestBraveSearchErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"rate limited", http.StatusTooManyRequests, "", "rate limited (429)"},
		{"bad key", http.StatusUnauthorized, "", "invalid API key (401)"},
		{"server error", http.StatusBadGateway, "upstream down", "API error 502: upstream down"},
		{"not json", http.StatusOK, "<html>", "failed to parse response"},
	}
	for _, tt := range tests {
		web := newFakeWeb(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(tt.body))
		})
		b := NewBrave("key")
		b.httpClient = web.client()
		if _, err := b.Search(context.Background(), "q", 3); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
		}
	}

	b := NewBrave("key")
	b.httpClient = brokenClient()
	if _, err := b.Search(context.Background(), "q", 3); err == nil || !strings.Contains(err.Error(), "request failed: ") {
		t.Errorf("a failed request: %v", err)
	}
	if _, err := NewBrave("").Search(context.Background(), "q", 3); err == nil || err.Error() != "Brave API key not configured" {
		t.Errorf("no key: %v", err)
	}
	if _, err := NewBrave("key").Search(context.Background(), "", 3); err == nil || err.Error() != "query cannot be empty" {
		t.Errorf("no query: %v", err)
	}
}

func TestBraveIsAvailable(t *testing.T) {
	if NewBrave("").IsAvailable(context.Background()) {
		t.Fatal("no key is never available")
	}
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusOK, true},
		{http.StatusTooManyRequests, true},
		{http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		web := newFakeWeb(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) })
		b := NewBrave("key")
		b.httpClient = web.client()
		if got := b.IsAvailable(context.Background()); got != tt.want {
			t.Errorf("status %d: IsAvailable() = %v, want %v", tt.status, got, tt.want)
		}
		if req := web.seen()[0]; req.Query.Get("q") != "test" || req.Header.Get("X-Subscription-Token") != "key" {
			t.Errorf("the health check request %+v", req)
		}
	}
	b := NewBrave("key")
	b.httpClient = brokenClient()
	if b.IsAvailable(context.Background()) {
		t.Fatal("an unreachable API is not available")
	}
}
