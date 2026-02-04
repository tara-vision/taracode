package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	braveAPIEndpoint    = "https://api.search.brave.com/res/v1/web/search"
	braveDefaultTimeout = 10 * time.Second
)

// BraveOption configures the Brave search provider
type BraveOption func(*Brave)

// Brave implements the Provider interface for Brave Search API
type Brave struct {
	apiKey     string
	httpClient *http.Client
	timeout    time.Duration
}

// braveResponse represents the Brave API response
type braveResponse struct {
	Query struct {
		Original string `json:"original"`
	} `json:"query"`
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
	Infobox *struct {
		Description string `json:"description"`
		Title       string `json:"title"`
	} `json:"infobox,omitempty"`
}

// NewBrave creates a new Brave search provider
func NewBrave(apiKey string, opts ...BraveOption) *Brave {
	b := &Brave{
		apiKey:  apiKey,
		timeout: braveDefaultTimeout,
	}

	for _, opt := range opts {
		opt(b)
	}

	b.httpClient = &http.Client{
		Timeout: b.timeout,
	}

	return b
}

// WithBraveTimeout sets the timeout for Brave search requests
func WithBraveTimeout(timeout time.Duration) BraveOption {
	return func(b *Brave) {
		b.timeout = timeout
	}
}

// Name returns the provider name
func (b *Brave) Name() string {
	return "Brave"
}

// IsAvailable checks if the Brave API is reachable and API key is valid
func (b *Brave) IsAvailable(ctx context.Context) bool {
	if b.apiKey == "" {
		return false
	}

	// Quick health check with a simple query
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "GET", braveAPIEndpoint+"?q=test&count=1", nil)
	if err != nil {
		return false
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// API key is valid if we get 200 or 429 (rate limited but key is valid)
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusTooManyRequests
}

// Search performs a web search using the Brave Search API
func (b *Brave) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("Brave API key not configured")
	}

	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// Build request URL
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", fmt.Sprintf("%d", numResults))
	params.Set("text_decorations", "false")
	params.Set("safesearch", "off")

	reqURL := braveAPIEndpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (429)")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid API key (401)")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var braveResp braveResponse
	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to SearchResponse
	response := &SearchResponse{
		Query:    query,
		Provider: b.Name(),
		Results:  make([]SearchResult, 0, len(braveResp.Web.Results)),
	}

	// Add infobox as instant answer if available
	if braveResp.Infobox != nil && braveResp.Infobox.Description != "" {
		response.InstantAnswer = braveResp.Infobox.Description
	}

	// Add web results
	for _, r := range braveResp.Web.Results {
		response.Results = append(response.Results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
			Source:  "Brave",
		})
	}

	return response, nil
}
