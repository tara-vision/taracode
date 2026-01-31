package search

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDuckDuckGoSearch(t *testing.T) {
	ddg := NewDuckDuckGo()

	// Test provider name
	if ddg.Name() != "DuckDuckGo" {
		t.Errorf("Expected name 'DuckDuckGo', got '%s'", ddg.Name())
	}

	// Test with multiple queries - DuckDuckGo Instant Answers works best with factual queries
	queries := []string{
		"golang",             // Should return Go language info
		"python",             // Programming language
		"what is kubernetes", // Common tech term
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			resp, err := ddg.Search(ctx, query, 5)
			if err != nil {
				t.Fatalf("DuckDuckGo search failed for '%s': %v", query, err)
			}

			if resp.Query != query {
				t.Errorf("Query mismatch: expected '%s', got '%s'", query, resp.Query)
			}

			if resp.Provider != "DuckDuckGo" {
				t.Errorf("Provider mismatch: expected 'DuckDuckGo', got '%s'", resp.Provider)
			}

			// Log what we got
			if resp.InstantAnswer != "" {
				t.Logf("Instant answer: %s", truncate(resp.InstantAnswer, 150))
			}
			t.Logf("Results: %d", len(resp.Results))
			for i, r := range resp.Results {
				if i >= 3 {
					break
				}
				t.Logf("  %d. %s", i+1, r.Title)
			}
		})
	}
}

func TestDuckDuckGoEmptyQuery(t *testing.T) {
	ddg := NewDuckDuckGo()
	ctx := context.Background()

	_, err := ddg.Search(ctx, "", 5)
	if err == nil {
		t.Error("Expected error for empty query")
	}
}

func TestDuckDuckGoIsAvailable(t *testing.T) {
	ddg := NewDuckDuckGo()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should return true if DuckDuckGo is reachable
	available := ddg.IsAvailable(ctx)
	t.Logf("DuckDuckGo available: %v", available)
	// Don't fail the test if not available (network issues)
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Go - A programming language", "Go"},
		{"Short title", "Short title"},
		{"This is a very long text that should be truncated at some point to fit the display requirements", "This is a very long text that should be truncated..."},
	}

	for _, tc := range tests {
		result := extractTitle(tc.input)
		if !strings.HasPrefix(result, strings.Split(tc.expected, "...")[0]) {
			t.Errorf("extractTitle(%q) = %q, want prefix %q", tc.input, result, tc.expected)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
