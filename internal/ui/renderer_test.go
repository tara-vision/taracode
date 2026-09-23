package ui

import (
	"errors"
	"strings"
	"testing"
)

func TestSearchFallbackMessage(t *testing.T) {
	r := NewRenderer()

	tests := []struct {
		name     string
		from     string
		to       string
		reason   error
		contains []string
	}{
		{
			name:   "rate limit error",
			from:   "DuckDuckGo",
			to:     "SearXNG",
			reason: errors.New("HTTP 429: rate limited"),
			contains: []string{
				"DuckDuckGo",
				"SearXNG",
				"rate limited",
				IconArrow,
				IconWarning,
			},
		},
		{
			name:   "timeout error",
			from:   "DuckDuckGo",
			to:     "SearXNG",
			reason: errors.New("request timeout after 10s"),
			contains: []string{
				"DuckDuckGo",
				"SearXNG",
				"timed out",
			},
		},
		{
			name:   "connection error",
			from:   "DuckDuckGo",
			to:     "SearXNG",
			reason: errors.New("connection refused"),
			contains: []string{
				"DuckDuckGo",
				"SearXNG",
				"connection error",
			},
		},
		{
			name:   "nil reason",
			from:   "DuckDuckGo",
			to:     "SearXNG",
			reason: nil,
			contains: []string{
				"DuckDuckGo",
				"SearXNG",
			},
		},
		{
			name:   "long error message truncated",
			from:   "DuckDuckGo",
			to:     "SearXNG",
			reason: errors.New("this is a very long error message that should be truncated to avoid cluttering the display"),
			contains: []string{
				"DuckDuckGo",
				"SearXNG",
				"...",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := r.SearchFallbackMessage(tc.from, tc.to, tc.reason)

			for _, c := range tc.contains {
				if !strings.Contains(result, c) {
					t.Errorf("Expected result to contain %q, got: %s", c, result)
				}
			}

			// Should not contain "nil" for nil reason
			if tc.reason == nil && strings.Contains(result, "nil") {
				t.Errorf("Result should not contain 'nil' for nil reason: %s", result)
			}
		})
	}
}

func TestRendererWarningMessage(t *testing.T) {
	r := NewRenderer()

	result := r.WarningMessage("Test warning")

	if !strings.Contains(result, "Test warning") {
		t.Error("Expected warning message to contain text")
	}

	if !strings.Contains(result, IconWarning) {
		t.Error("Expected warning message to contain warning icon")
	}
}

func TestRendererInfoMessage(t *testing.T) {
	r := NewRenderer()

	result := r.InfoMessage("Test info")

	if !strings.Contains(result, "Test info") {
		t.Error("Expected info message to contain text")
	}

	if !strings.Contains(result, IconInfo) {
		t.Error("Expected info message to contain info icon")
	}
}

func TestRendererSuccessMessage(t *testing.T) {
	r := NewRenderer()

	result := r.SuccessMessage("Test success")

	if !strings.Contains(result, "Test success") {
		t.Error("Expected success message to contain text")
	}

	if !strings.Contains(result, IconSuccess) {
		t.Error("Expected success message to contain success icon")
	}
}

func TestFormatToolStatusCoversEveryBuiltinTool(t *testing.T) {
	r := NewRenderer()
	longCommand := "echo " + strings.Repeat("x", 80)
	cases := []struct {
		tool   string
		params map[string]interface{}
		result string
		want   string
	}{
		{"read_file", map[string]interface{}{"path": "src/main.go"}, "a\nb\nc", "Read main.go (3 lines)"},
		{"search_files", map[string]interface{}{"pattern": "TODO"}, "a.go:1:x\nb.go:2:y", `Searched for "TODO" (2 matches)`},
		{"search_files", map[string]interface{}{"pattern": "nope"}, "No matches found", `Searched for "nope" (no matches)`},
		{"list_files", map[string]interface{}{}, "a\nb/", "Listed current directory (2 items)"},
		{"list_files", map[string]interface{}{"path": "empty"}, "No entries", "Listed empty (empty)"},
		{"write_file", map[string]interface{}{"path": "dir/out.txt"}, "Wrote 3 bytes", "Wrote out.txt"},
		{"edit_file", map[string]interface{}{"path": "dir/in.go"}, "Edited", "Edited in.go"},
		{"shell", map[string]interface{}{"command": longCommand}, "ok", "Executed: " + longCommand[:MaxCommandDisplay-3] + "..."},
		{"git", map[string]interface{}{"args": "log -n 5"}, "", "git log"},
		{"helm", map[string]interface{}{"args": "list -A"}, "", "helm list"},
		{"docker", map[string]interface{}{"args": "ps -a"}, "", "docker ps"},
		{"kubectl", map[string]interface{}{"verb": "get", "resource": "pods", "name": "web"}, "", "kubectl get pods web"},
		{"kubectl", map[string]interface{}{"verb": "apply"}, "", "kubectl apply"},
		{"terraform", map[string]interface{}{"command": "plan"}, "", "terraform plan"},
		{"cloud", map[string]interface{}{"provider": "aws", "args": "s3 ls s3://bucket"}, "", "aws s3 ls"},
		{"scan", map[string]interface{}{"scanner": "trivy", "target": "nginx:1.27"}, "", "trivy scan nginx:1.27"},
		{"scan", map[string]interface{}{"scanner": "gitleaks"}, "", "gitleaks scan current directory"},
		{"web_fetch", map[string]interface{}{"url": "https://example.com/docs/page"}, "", "Fetched example.com"},
		{"get_datetime", map[string]interface{}{}, "", "Checked the date and time"},
		{"github.list_issues", map[string]interface{}{}, "", "github.list_issues completed"},
	}
	for _, tc := range cases {
		got := r.FormatToolStatusWithDuration(tc.tool, tc.params, tc.result, false, 0)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to contain %q", tc.tool, got, tc.want)
		}
	}
	if got := r.FormatToolStatusWithDuration("shell", nil, "boom", true, 2500); !strings.Contains(got, "shell failed [2.5s]") {
		t.Errorf("error line %q", got)
	}
}

func TestSearchStatusCountsResultsOrTheAnswer(t *testing.T) {
	results := "Results for \"go\" (duckduckgo):\n\n1. Go\n   https://go.dev\n   The Go site\n\n2. Tour\n   https://go.dev/tour\n   A tour\n"
	cases := map[string]string{
		results: `Searched "go" (2 results)`,
		"Results for \"go\" (duckduckgo):\n\nAnswer: a language\n": `Searched "go" (found answer)`,
		"Results for \"go\" (duckduckgo):\n\nNo results.\n":        `Searched "go" (no results)`,
	}
	for result, want := range cases {
		if got := searchStatus("go", result); got != want {
			t.Errorf("searchStatus = %q, want %q", got, want)
		}
	}
	if got := urlHost("not a url"); got != "not a url" {
		t.Errorf("urlHost of a non-URL = %q", got)
	}
}
