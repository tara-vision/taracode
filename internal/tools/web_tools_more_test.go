package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/search"
)

// localFetch lets web_fetch reach 127.0.0.1 for the rest of the test.
func localFetch(t *testing.T) {
	t.Helper()
	allowLocalFetch = true
	t.Cleanup(func() { allowLocalFetch = false })
}

func searxngServer(t *testing.T, handler http.HandlerFunc) *search.Orchestrator {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return search.NewOrchestrator(search.OrchestratorConfig{
		Primary: "searxng", CustomSearXNGInstance: srv.URL, Timeout: 5 * time.Second, RetryCount: 0,
	})
}

func TestWebSearchArgumentsAndFailures(t *testing.T) {
	orch := searxngServer(t, func(w http.ResponseWriter, r *http.Request) {
		asked := r.URL.Query().Get("q")
		if asked == "broken" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var results []string
		for i := 1; i <= 8; i++ {
			results = append(results, fmt.Sprintf(`{"url":"https://example.com/%d","title":"R%d","content":"c","engine":"e"}`, i, i))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"query":%q,"number_of_results":8,"results":[%s]}`, asked, strings.Join(results, ","))
	})
	tool := WebSearchTool(orch)
	if _, err := tool.Run(context.Background(), map[string]any{}, ""); err == nil || err.Error() != "query is required" {
		t.Fatalf("no query: %v", err)
	}
	out, err := tool.Run(context.Background(), map[string]any{"query": "many", "max": 50.0}, "")
	if err != nil || !strings.Contains(out, "5. R5") || strings.Contains(out, "6. R6") {
		t.Fatalf("a max out of range gives 5 results: %q %v", out, err)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"query": "broken"}, ""); err == nil ||
		!strings.HasPrefix(err.Error(), "search failed: ") {
		t.Fatalf("a failed search: %v", err)
	}
}

func TestWebFetchFailures(t *testing.T) {
	localFetch(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/missing", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/short", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("only ten b"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tool := WebFetchTool()
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "url is required"},
		{map[string]any{"url": srv.URL + "/missing"}, "HTTP 404"},
		{map[string]any{"url": srv.URL + "/loop"}, "too many redirects"},
		{map[string]any{"url": srv.URL + "/short"}, "unexpected EOF"},
	}
	for _, tt := range tests {
		if _, err := tool.Run(context.Background(), tt.args, ""); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestWebFetchFollowsARedirectAndCutsLongText(t *testing.T) {
	localFetch(t)
	long := strings.Repeat("a", webFetchMaxChars+10)
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/new", http.StatusMovedPermanently) })
	mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(long))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	out, err := WebFetchTool().Run(context.Background(), map[string]any{"url": srv.URL + "/old"}, "")
	if err != nil || out != long[:webFetchMaxChars]+"\n[truncated]" {
		t.Fatalf("%d chars, %v", len(out), err)
	}
}
