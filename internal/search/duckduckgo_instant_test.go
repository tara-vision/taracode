package search

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ddgInstant serves an empty HTML page (no results) and the Instant Answer API answer as JSON.
func ddgInstant(t *testing.T, apiStatus int, apiBody string) *fakeWeb {
	t.Helper()
	return newFakeWeb(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "html.duckduckgo.com" {
			_, _ = w.Write([]byte("<html><body>No results.</body></html>"))
			return
		}
		w.WriteHeader(apiStatus)
		_, _ = w.Write([]byte(apiBody))
	})
}

const instantAnswer = `{"Heading":"Kubernetes","Abstract":"An open-source container orchestration system.",
"AbstractSource":"Wikipedia","AbstractURL":"https://en.wikipedia.org/wiki/Kubernetes",
"Results":[{"Text":"Official site - kubernetes.io","FirstURL":"https://kubernetes.io"},{"Text":"","FirstURL":"https://skip"}],
"RelatedTopics":[{"Text":"Docker - A container runtime","FirstURL":"https://duckduckgo.com/Docker"},
{"Name":"Tools","Topics":[{"Text":"Helm - The package manager","FirstURL":"https://duckduckgo.com/Helm"},
{"Text":"kubectl - The command line","FirstURL":"https://duckduckgo.com/kubectl"}]}]}`

// TestDuckDuckGoFallsBackToTheInstantAnswerAPI: an HTML page without results sends the search to
// the Instant Answer API, whose abstract, results and related topics (nested ones too) become results.
func TestDuckDuckGoFallsBackToTheInstantAnswerAPI(t *testing.T) {
	web := ddgInstant(t, http.StatusOK, instantAnswer)
	d := NewDuckDuckGo()
	d.client = web.client()
	resp, err := d.Search(context.Background(), "kubernetes", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []SearchResult{
		{Title: "Kubernetes", URL: "https://en.wikipedia.org/wiki/Kubernetes", Snippet: "An open-source container orchestration system.",
			Source: "Wikipedia"},
		{Title: "Official site", URL: "https://kubernetes.io", Snippet: "Official site - kubernetes.io"},
		{Title: "Docker", URL: "https://duckduckgo.com/Docker", Snippet: "Docker - A container runtime"},
		{Title: "Helm", URL: "https://duckduckgo.com/Helm", Snippet: "Helm - The package manager"},
		{Title: "kubectl", URL: "https://duckduckgo.com/kubectl", Snippet: "kubectl - The command line"},
	}
	if resp.InstantAnswer != "An open-source container orchestration system." || fmt.Sprint(resp.Results) != fmt.Sprint(want) {
		t.Fatalf("response %+v", resp)
	}
	api := web.seen()[1]
	if api.Host != "api.duckduckgo.com" || api.Query.Get("q") != "kubernetes" || api.Query.Get("format") != "json" ||
		api.Query.Get("no_html") != "1" || api.Query.Get("skip_disambig") != "1" {
		t.Fatalf("API request %+v", api)
	}

	for _, limit := range []int{1, 2, 3, 4} {
		resp, err := d.Search(context.Background(), "kubernetes", limit)
		if err != nil || len(resp.Results) != limit {
			t.Errorf("limit %d: %d results, err %v", limit, len(resp.Results), err)
		}
	}
}

func TestDuckDuckGoInstantAnswerPrefersTheAnswer(t *testing.T) {
	tests := []struct {
		body, want string
	}{
		{`{"Answer":"42","Abstract":"abstract","Definition":"definition"}`, "42"},
		{`{"Definition":"A word.","AbstractSource":"Merriam"}`, "A word."},
		{`{"Abstract":"Only an abstract","AbstractSource":"Source","AbstractURL":"https://example.com"}`, "Only an abstract"},
	}
	for _, tt := range tests {
		web := ddgInstant(t, http.StatusAccepted, tt.body)
		d := NewDuckDuckGo()
		d.client = web.client()
		resp, err := d.Search(context.Background(), "q", 5)
		if err != nil || resp.InstantAnswer != tt.want {
			t.Errorf("%s: %+v %v", tt.body, resp, err)
		}
	}
	// An abstract without a heading takes its source as the title.
	web := ddgInstant(t, http.StatusOK, tests[2].body)
	d := NewDuckDuckGo()
	d.client = web.client()
	if resp, err := d.Search(context.Background(), "q", 5); err != nil || len(resp.Results) != 1 || resp.Results[0].Title != "Source" {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestDuckDuckGoSearchErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{"html rate limited, api rate limited", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}, "rate limited (HTTP 429)"},
		{"html and api fail", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}, "DuckDuckGo returned status 503"},
		{"api answers garbage", func(w http.ResponseWriter, r *http.Request) {
			if r.Host == "html.duckduckgo.com" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte("not json"))
		}, "failed to parse DuckDuckGo response"},
	}
	for _, tt := range tests {
		web := newFakeWeb(t, tt.handler)
		d := NewDuckDuckGo()
		d.client = web.client()
		if _, err := d.Search(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
		}
	}

	d := NewDuckDuckGo(WithTimeout(0))
	d.client = brokenClient()
	if _, err := d.Search(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "DuckDuckGo request failed") {
		t.Fatalf("an unreachable API: %v", err)
	}
	if _, err := d.searchHTML(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "DuckDuckGo request failed") {
		t.Fatalf("an unreachable page: %v", err)
	}
}

// truncatedBody declares more bytes than it sends, so reading the body fails.
func truncatedBody(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Length", "1000")
	_, _ = w.Write([]byte("<html>"))
}

func TestDuckDuckGoReportsATruncatedPage(t *testing.T) {
	web := newFakeWeb(t, truncatedBody)
	d := NewDuckDuckGo()
	d.client = web.client()
	if _, err := d.searchHTML(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Fatalf("err = %v", err)
	}
}

func TestDuckDuckGoIsAvailableReadsTheStatus(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusOK, true},
		{http.StatusMethodNotAllowed, true},
		{http.StatusServiceUnavailable, false},
	}
	for _, tt := range tests {
		web := newFakeWeb(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) })
		d := NewDuckDuckGo()
		d.client = web.client()
		if got := d.IsAvailable(context.Background()); got != tt.want {
			t.Errorf("status %d: IsAvailable() = %v, want %v", tt.status, got, tt.want)
		}
		if req := web.seen()[0]; req.Method != http.MethodHead || req.Host != "api.duckduckgo.com" {
			t.Errorf("the health check %+v", req)
		}
	}
	d := NewDuckDuckGo()
	d.client = brokenClient()
	if d.IsAvailable(context.Background()) {
		t.Fatal("an unreachable API is not available")
	}
}
