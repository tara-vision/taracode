package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearXNGOptions(t *testing.T) {
	s := NewSearXNG(WithSearXNGTimeout(4*time.Second), WithSearXNGInstance("https://searx.example/"))
	if s.Name() != "SearXNG" || s.timeout != 4*time.Second || s.client.Timeout != 4*time.Second ||
		s.baseURL != "https://searx.example" || strings.Join(s.instances, ",") != "https://searx.example" {
		t.Fatalf("%+v", s)
	}
	many := NewSearXNG(WithSearXNGInstances([]string{"https://a.example/", "https://b.example"}))
	if many.baseURL != "https://a.example" || strings.Join(many.instances, ",") != "https://a.example,https://b.example" {
		t.Fatalf("%+v", many)
	}
	none := NewSearXNG(WithSearXNGInstances(nil))
	if len(none.instances) != 0 || none.baseURL != defaultSearXNGInstances[0] {
		t.Fatalf("an empty list keeps the default base: %+v", none)
	}
}

// searxServer is a SearXNG instance answering status and body to /search.
func searxServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("categories") != "general" ||
			r.URL.Query().Get("language") != "en" || r.Header.Get("Accept") != "application/json" {
			http.Error(w, "unexpected request "+r.URL.String(), http.StatusTeapot)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const searxAnswer = `{"query":"q","answers":["first","second"],"infoboxes":[{"content":"box"}],"results":[
{"url":"https://a.example","title":"A","content":"about a","engine":"bing"},
{"url":"https://a.example","title":"A again","content":"duplicate"},
{"url":"https://b.example","title":"","content":"no title"},
{"url":"https://c.example","title":"C","content":"about c","engine":"brave"},
{"url":"https://d.example","title":"D","content":"about d"}]}`

func TestSearXNGSearchMovesToAWorkingInstance(t *testing.T) {
	down := searxServer(t, http.StatusInternalServerError, "")
	up := searxServer(t, http.StatusOK, searxAnswer)
	s := NewSearXNG(WithSearXNGInstances([]string{down.URL, up.URL}))
	resp, err := s.Search(context.Background(), "q", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []SearchResult{
		{Title: "A", URL: "https://a.example", Snippet: "about a", Source: "bing"},
		{Title: "C", URL: "https://c.example", Snippet: "about c", Source: "brave"},
	}
	if resp.Provider != "SearXNG" || resp.InstantAnswer != "first second" || fmt.Sprint(resp.Results) != fmt.Sprint(want) {
		t.Fatalf("response %+v: duplicates and untitled results skipped, two kept", resp)
	}
	if s.baseURL != up.URL {
		t.Fatalf("the working instance is remembered: %s", s.baseURL)
	}
}

func TestSearXNGSearchTakesTheInfoboxWithoutAnswers(t *testing.T) {
	srv := searxServer(t, http.StatusOK, `{"infoboxes":[{"content":"From the infobox"}],"results":[]}`)
	resp, err := NewSearXNG(WithSearXNGInstance(srv.URL)).Search(context.Background(), "q", 5)
	if err != nil || resp.InstantAnswer != "From the infobox" || len(resp.Results) != 0 {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestSearXNGSearchErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"rate limited", http.StatusTooManyRequests, "", "rate limited (HTTP 429)"},
		{"json blocked", http.StatusForbidden, "", "access forbidden - instance may block JSON API"},
		{"server error", http.StatusBadGateway, "", "SearXNG returned status 502"},
		{"not json", http.StatusOK, "<html>", "failed to parse SearXNG response"},
	}
	for _, tt := range tests {
		srv := searxServer(t, tt.status, tt.body)
		_, err := NewSearXNG(WithSearXNGInstance(srv.URL)).Search(context.Background(), "q", 5)
		if err == nil || !strings.Contains(err.Error(), "all SearXNG instances failed: ") || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.wantErr)
		}
	}

	broken := NewSearXNG(WithSearXNGInstance("https://searx.example"))
	broken.client = brokenClient()
	if _, err := broken.Search(context.Background(), "q", 5); err == nil || !strings.Contains(err.Error(), "SearXNG request failed") {
		t.Errorf("an unreachable instance: %v", err)
	}
	if _, err := NewSearXNG(WithSearXNGInstance("http://bad\x7fhost")).Search(context.Background(), "q", 5); err == nil ||
		!strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("an instance URL that does not parse: %v", err)
	}
	if _, err := NewSearXNG(WithSearXNGInstances(nil)).Search(context.Background(), "q", 5); err == nil ||
		err.Error() != "no SearXNG instances configured" {
		t.Errorf("no instances: %v", err)
	}
	if _, err := NewSearXNG().Search(context.Background(), "", 5); err == nil || err.Error() != "query cannot be empty" {
		t.Errorf("no query: %v", err)
	}
}

func TestSearXNGIsAvailableFindsAnInstanceThatAnswers(t *testing.T) {
	status := func(code int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		t.Cleanup(srv.Close)
		return srv
	}
	down, notAllowed, ok := status(http.StatusServiceUnavailable), status(http.StatusMethodNotAllowed), status(http.StatusOK)

	s := NewSearXNG(WithSearXNGInstances([]string{"http://bad\x7fhost", down.URL, notAllowed.URL, ok.URL}))
	if !s.IsAvailable(context.Background()) || s.baseURL != notAllowed.URL {
		t.Fatalf("the first instance that answers 200 or 405 is used: %s", s.baseURL)
	}
	s = NewSearXNG(WithSearXNGInstances([]string{down.URL, ok.URL}))
	if !s.IsAvailable(context.Background()) || s.baseURL != ok.URL {
		t.Fatalf("base %s", s.baseURL)
	}
	unreachable := NewSearXNG(WithSearXNGInstances([]string{down.URL, "https://searx.example"}))
	unreachable.client = brokenClient()
	if unreachable.IsAvailable(context.Background()) {
		t.Fatal("no instance answers")
	}
}
