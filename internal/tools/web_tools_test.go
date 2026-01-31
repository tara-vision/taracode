package tools

import (
	"strings"
	"testing"
)

func TestWebSearch(t *testing.T) {
	// Test with valid query
	result, err := WebSearch(map[string]interface{}{
		"query":       "golang",
		"num_results": float64(3),
	}, ".")
	if err != nil {
		t.Fatalf("WebSearch failed: %v", err)
	}

	if !strings.Contains(result, "Search results for") {
		t.Errorf("Expected search results header, got: %s", result[:min(len(result), 200)])
	}

	if !strings.Contains(result, "DuckDuckGo") {
		t.Errorf("Expected DuckDuckGo provider mention")
	}

	t.Logf("Result preview: %s", result[:min(len(result), 500)])
}

func TestWebSearchMissingQuery(t *testing.T) {
	_, err := WebSearch(map[string]interface{}{}, ".")
	if err == nil {
		t.Error("Expected error for missing query")
	}
}

func TestWebSearchEmptyQuery(t *testing.T) {
	_, err := WebSearch(map[string]interface{}{
		"query": "",
	}, ".")
	if err == nil {
		t.Error("Expected error for empty query")
	}
}

func TestWebFetch(t *testing.T) {
	// Test with a simple, reliable URL
	result, err := WebFetch(map[string]interface{}{
		"url":        "https://example.com",
		"max_length": float64(5000),
	}, ".")
	if err != nil {
		t.Fatalf("WebFetch failed: %v", err)
	}

	if !strings.Contains(result, "Fetched:") {
		t.Errorf("Expected 'Fetched:' header")
	}

	if !strings.Contains(result, "Title:") {
		t.Errorf("Expected 'Title:' in result")
	}

	if !strings.Contains(result, "Content length:") {
		t.Errorf("Expected 'Content length:' in result")
	}

	t.Logf("Result preview: %s", result[:min(len(result), 500)])
}

func TestWebFetchMissingURL(t *testing.T) {
	_, err := WebFetch(map[string]interface{}{}, ".")
	if err == nil {
		t.Error("Expected error for missing URL")
	}
}

func TestWebFetchInvalidURL(t *testing.T) {
	_, err := WebFetch(map[string]interface{}{
		"url": "not-a-valid-url",
	}, ".")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestWebFetchInvalidScheme(t *testing.T) {
	_, err := WebFetch(map[string]interface{}{
		"url": "ftp://example.com",
	}, ".")
	if err == nil {
		t.Error("Expected error for non-http(s) scheme")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
