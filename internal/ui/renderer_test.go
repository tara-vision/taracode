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
