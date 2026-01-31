package search

import "context"

// SearchResult represents a single search result
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source,omitempty"`
}

// SearchResponse holds the complete search response
type SearchResponse struct {
	Query         string         `json:"query"`
	Provider      string         `json:"provider"`
	InstantAnswer string         `json:"instant_answer,omitempty"`
	Results       []SearchResult `json:"results"`
	Fallback      bool           `json:"fallback,omitempty"` // True if this came from a fallback provider
}

// Provider defines the search provider interface
type Provider interface {
	// Search performs a search query and returns results
	// Context is used for timeout and cancellation
	Search(ctx context.Context, query string, numResults int) (*SearchResponse, error)

	// Name returns the provider's display name (e.g., "DuckDuckGo", "SearXNG")
	Name() string

	// IsAvailable performs a quick health check to determine if the provider is reachable
	// This should be fast (sub-second) and not count against rate limits
	IsAvailable(ctx context.Context) bool
}

// SearchError represents a search error with additional context
type SearchError struct {
	Provider string // Provider that failed
	Err      error  // Underlying error
	Retried  bool   // Whether retry was attempted
}

func (e *SearchError) Error() string {
	errMsg := "unknown error"
	if e.Err != nil {
		errMsg = e.Err.Error()
	}
	if e.Retried {
		return e.Provider + " failed after retry: " + errMsg
	}
	return e.Provider + " failed: " + errMsg
}

func (e *SearchError) Unwrap() error {
	return e.Err
}
