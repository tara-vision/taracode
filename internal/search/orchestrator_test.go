package search

import (
	"context"
	"testing"
	"time"
)

func TestOrchestratorDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Primary != "duckduckgo" {
		t.Errorf("Expected primary 'duckduckgo', got '%s'", config.Primary)
	}

	if config.Fallback != "searxng" {
		t.Errorf("Expected fallback 'searxng', got '%s'", config.Fallback)
	}

	if config.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", config.Timeout)
	}

	if config.RetryCount != 1 {
		t.Errorf("Expected retry count 1, got %d", config.RetryCount)
	}
}

func TestOrchestratorCreation(t *testing.T) {
	config := DefaultConfig()
	orch := NewOrchestrator(config)

	if orch.PrimaryProvider() == nil {
		t.Error("Expected primary provider to be set")
	}

	if orch.FallbackProvider() == nil {
		t.Error("Expected fallback provider to be set")
	}

	if orch.PrimaryProvider().Name() != "DuckDuckGo" {
		t.Errorf("Expected primary 'DuckDuckGo', got '%s'", orch.PrimaryProvider().Name())
	}

	if orch.FallbackProvider().Name() != "SearXNG" {
		t.Errorf("Expected fallback 'SearXNG', got '%s'", orch.FallbackProvider().Name())
	}
}

func TestOrchestratorSearch(t *testing.T) {
	config := DefaultConfig()
	config.Timeout = 30 * time.Second
	orch := NewOrchestrator(config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := orch.Search(ctx, "golang", 3)
	if err != nil {
		t.Fatalf("Orchestrator search failed: %v", err)
	}

	if resp.Query != "golang" {
		t.Errorf("Query mismatch: expected 'golang', got '%s'", resp.Query)
	}

	t.Logf("Provider: %s (fallback: %v)", resp.Provider, resp.Fallback)
	t.Logf("Results: %d", len(resp.Results))
}

func TestOrchestratorEmptyQuery(t *testing.T) {
	config := DefaultConfig()
	orch := NewOrchestrator(config)

	ctx := context.Background()
	_, err := orch.Search(ctx, "", 5)
	if err == nil {
		t.Error("Expected error for empty query")
	}
}

func TestOrchestratorIsAvailable(t *testing.T) {
	config := DefaultConfig()
	orch := NewOrchestrator(config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	available := orch.IsAvailable(ctx)
	t.Logf("Orchestrator available: %v", available)
	// At least one provider should be available in most cases
}

func TestOrchestratorProviderSwitchCallback(t *testing.T) {
	callbackCalled := false
	var switchedFrom, switchedTo string

	config := DefaultConfig()
	config.OnProviderSwitch = func(from, to string, reason error) {
		callbackCalled = true
		switchedFrom = from
		switchedTo = to
		t.Logf("Provider switch: %s -> %s (reason: %v)", from, to, reason)
	}

	orch := NewOrchestrator(config)

	// The callback would only be called on actual fallback
	// This test just verifies the callback is properly set
	if orch.config.OnProviderSwitch == nil {
		t.Error("Expected OnProviderSwitch callback to be set")
	}

	_ = callbackCalled
	_ = switchedFrom
	_ = switchedTo
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{&SearchError{Err: nil}, false},
	}

	for _, tc := range tests {
		result := isRateLimitError(tc.err)
		if result != tc.expected {
			t.Errorf("isRateLimitError(%v) = %v, want %v", tc.err, result, tc.expected)
		}
	}
}
