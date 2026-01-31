package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// OrchestratorConfig configures the search orchestrator
type OrchestratorConfig struct {
	// Primary provider name (e.g., "brave", "duckduckgo", "searxng")
	Primary string

	// Fallback provider name (optional)
	Fallback string

	// Timeout per search request
	Timeout time.Duration

	// RetryCount is the number of retries before falling back
	RetryCount int

	// OnProviderSwitch is called when switching to fallback provider
	OnProviderSwitch func(from, to string, reason error)

	// CustomSearXNGInstance allows configuring a custom SearXNG instance
	CustomSearXNGInstance string

	// BraveAPIKey is the API key for Brave Search (if configured, Brave becomes available)
	BraveAPIKey string
}

// DefaultConfig returns the default orchestrator configuration
func DefaultConfig() OrchestratorConfig {
	return OrchestratorConfig{
		Primary:    "duckduckgo",
		Fallback:   "searxng",
		Timeout:    10 * time.Second,
		RetryCount: 1,
	}
}

// Orchestrator manages multiple search providers with fallback support
type Orchestrator struct {
	config    OrchestratorConfig
	primary   Provider
	fallback  Provider
	providers map[string]Provider
}

// NewOrchestrator creates a new search orchestrator
func NewOrchestrator(config OrchestratorConfig) *Orchestrator {
	o := &Orchestrator{
		config:    config,
		providers: make(map[string]Provider),
	}

	// Initialize providers
	o.initializeProviders()

	return o
}

// initializeProviders sets up the configured providers
func (o *Orchestrator) initializeProviders() {
	// Create DuckDuckGo provider
	ddg := NewDuckDuckGo(WithTimeout(o.config.Timeout))
	o.providers["duckduckgo"] = ddg

	// Create SearXNG provider
	var sxng *SearXNG
	if o.config.CustomSearXNGInstance != "" {
		sxng = NewSearXNG(
			WithSearXNGTimeout(o.config.Timeout),
			WithSearXNGInstance(o.config.CustomSearXNGInstance),
		)
	} else {
		sxng = NewSearXNG(WithSearXNGTimeout(o.config.Timeout))
	}
	o.providers["searxng"] = sxng

	// Create Brave provider if API key is configured
	if o.config.BraveAPIKey != "" {
		brave := NewBrave(o.config.BraveAPIKey, WithBraveTimeout(o.config.Timeout))
		o.providers["brave"] = brave
	}

	// Set primary and fallback
	primaryName := strings.ToLower(o.config.Primary)
	if p, ok := o.providers[primaryName]; ok {
		o.primary = p
	} else {
		o.primary = ddg // Default to DuckDuckGo
	}

	fallbackName := strings.ToLower(o.config.Fallback)
	if f, ok := o.providers[fallbackName]; ok && fallbackName != primaryName {
		o.fallback = f
	}
}

// Search performs a search with automatic fallback on failure
func (o *Orchestrator) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// Create timeout context if not already set
	searchCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		searchCtx, cancel = context.WithTimeout(ctx, o.config.Timeout)
		defer cancel()
	}

	// Try primary provider
	response, err := o.tryProvider(searchCtx, o.primary, query, numResults)
	if err == nil {
		return response, nil
	}

	primaryErr := err

	// If no fallback, return the error
	if o.fallback == nil {
		return nil, &SearchError{
			Provider: o.primary.Name(),
			Err:      primaryErr,
			Retried:  o.config.RetryCount > 0,
		}
	}

	// Notify about provider switch
	if o.config.OnProviderSwitch != nil {
		o.config.OnProviderSwitch(o.primary.Name(), o.fallback.Name(), primaryErr)
	}

	// Try fallback provider
	response, err = o.tryProvider(searchCtx, o.fallback, query, numResults)
	if err == nil {
		response.Fallback = true
		return response, nil
	}

	// Both providers failed
	return nil, fmt.Errorf("all search providers failed: primary (%s): %v, fallback (%s): %v",
		o.primary.Name(), primaryErr, o.fallback.Name(), err)
}

// tryProvider attempts to search with a provider, with retries
func (o *Orchestrator) tryProvider(ctx context.Context, provider Provider, query string, numResults int) (*SearchResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= o.config.RetryCount; attempt++ {
		// Check context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check if rate limited - immediate fallback
		if lastErr != nil && isRateLimitError(lastErr) {
			break
		}

		// Small delay between retries (except first attempt)
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}

		response, err := provider.Search(ctx, query, numResults)
		if err == nil {
			return response, nil
		}

		lastErr = err

		// Don't retry on certain errors
		if isNonRetryableError(err) {
			break
		}
	}

	return nil, lastErr
}

// isRateLimitError checks if the error indicates rate limiting
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests")
}

// isNonRetryableError checks if the error should not be retried
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Rate limits - fallback immediately
	if isRateLimitError(err) {
		return true
	}

	// Empty query - programmer error
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "query cannot be empty") {
		return true
	}

	// Context cancelled - user cancelled
	if errors.Is(err, context.Canceled) {
		return true
	}

	return false
}

// Name returns the orchestrator's active provider name
func (o *Orchestrator) Name() string {
	return fmt.Sprintf("Orchestrator(%s->%s)", o.primary.Name(), o.fallbackName())
}

// fallbackName returns the fallback provider name or "none"
func (o *Orchestrator) fallbackName() string {
	if o.fallback != nil {
		return o.fallback.Name()
	}
	return "none"
}

// PrimaryProvider returns the primary provider
func (o *Orchestrator) PrimaryProvider() Provider {
	return o.primary
}

// FallbackProvider returns the fallback provider (may be nil)
func (o *Orchestrator) FallbackProvider() Provider {
	return o.fallback
}

// IsAvailable checks if any provider is available
func (o *Orchestrator) IsAvailable(ctx context.Context) bool {
	if o.primary != nil && o.primary.IsAvailable(ctx) {
		return true
	}
	if o.fallback != nil && o.fallback.IsAvailable(ctx) {
		return true
	}
	return false
}
