package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/ui"
)

func TestToolsListsEveryTool(t *testing.T) {
	r, _ := projectREPL(t)
	r.mcp = newMCPManager(t, fakeMCPServer("fake", false))
	_ = captureStdoutForTest(t, func() { r.dispatch("/mcp connect fake") })
	out := captureStdoutForTest(t, func() { r.dispatch("/tools") })
	registry := r.asst.ToolRegistry()
	names := registry.Names()
	want := []string{
		"Tools (* = has a read form, available in investigate mode):",
		fmt.Sprintf("%d tools; %d exposed in investigate mode", len(names), registry.Available(r.asst.Mode())),
	}
	truncated := false
	for _, name := range names {
		tool, _ := registry.Get(name)
		marker := " "
		if tool.ReadForm {
			marker = "*"
		}
		line := fmt.Sprintf("  %s %-20s %s", marker, name, ui.TruncateString(tool.Description, toolDescriptionWidth))
		if strings.HasPrefix(name, "fake.") {
			line += " [MCP: fake]"
		}
		want = append(want, line+"\n")
		truncated = truncated || len(tool.Description) > toolDescriptionWidth
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("/tools lacks %q:\n%s", w, out)
		}
	}
	if !truncated || !strings.Contains(out, "  * fake.list_issues") || !strings.Contains(out, "    write_file") {
		t.Fatalf("expected a cut description, the read marker on a read tool and none on write_file:\n%s", out)
	}
}

// TestToolConfigFallsBackToTheSecondSearchProvider: DuckDuckGo answers 429, so the search moves to
// the configured SearXNG instance and says so. The timeout setting does not parse; the default it
// falls back to is long enough for the fallback to answer (a zero timeout would fail the search).
func TestToolConfigFallsBackToTheSecondSearchProvider(t *testing.T) {
	resetConfig(t)
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"results":[{"url":"https://kubernetes.io/docs","title":"Rollouts","content":"How rollouts work"}]}`)
	}))
	t.Cleanup(searx.Close)
	fakeInternet(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "slow down", http.StatusTooManyRequests) })
	viper.Set("search.primary", "duckduckgo")
	viper.Set("search.fallback", "searxng")
	viper.Set("search.searxng_instance", searx.URL)
	viper.Set("search.retry_count", 0)
	viper.Set("search.timeout", "soon")
	cfg := toolConfig(ui.NewRenderer())
	if cfg.Stream != os.Stdout {
		t.Fatalf("live command output goes to stdout by default: %v", cfg.Stream)
	}
	out := captureStdoutForTest(t, func() {
		resp, err := cfg.Search.Search(context.Background(), "kubectl rollout", 3)
		if err != nil || !resp.Fallback || len(resp.Results) != 1 || resp.Results[0].Title != "Rollouts" {
			t.Errorf("search %+v err=%v", resp, err)
		}
	})
	if !strings.Contains(out, "Search: DuckDuckGo "+ui.IconArrow+" SearXNG (rate limited)") {
		t.Fatalf("the fallback notice: %q", out)
	}
}

func TestToolConfigPrefersBraveWithAKey(t *testing.T) {
	resetConfig(t)
	viper.Set("search.primary", "")
	viper.Set("search.brave_api_key", "a-key")
	viper.Set("no_stream_commands", true)
	cfg := toolConfig(ui.NewRenderer())
	if got := cfg.Search.Name(); got != "Orchestrator(Brave->DuckDuckGo)" {
		t.Fatalf("orchestrator %s", got)
	}
	if cfg.Stream != nil {
		t.Fatalf("no_stream_commands turns the live output off: %v", cfg.Stream)
	}
}
