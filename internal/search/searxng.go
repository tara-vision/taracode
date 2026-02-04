package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default public SearXNG instances (privacy-focused, no API key required)
// These are well-maintained public instances with JSON API enabled
var defaultSearXNGInstances = []string{
	"https://search.sapti.me",
	"https://searx.be",
	"https://search.bus-hit.me",
}

// SearXNG implements the Provider interface using SearXNG metasearch
type SearXNG struct {
	client    *http.Client
	timeout   time.Duration
	instances []string
	baseURL   string // Currently active instance
}

// SearXNGOption configures SearXNG provider
type SearXNGOption func(*SearXNG)

// WithSearXNGTimeout sets the HTTP timeout
func WithSearXNGTimeout(timeout time.Duration) SearXNGOption {
	return func(s *SearXNG) {
		s.timeout = timeout
		s.client.Timeout = timeout
	}
}

// WithSearXNGInstance sets a custom SearXNG instance URL
func WithSearXNGInstance(instanceURL string) SearXNGOption {
	return func(s *SearXNG) {
		s.baseURL = strings.TrimSuffix(instanceURL, "/")
		s.instances = []string{s.baseURL}
	}
}

// WithSearXNGInstances sets multiple SearXNG instance URLs
func WithSearXNGInstances(instances []string) SearXNGOption {
	return func(s *SearXNG) {
		s.instances = make([]string, len(instances))
		for i, inst := range instances {
			s.instances[i] = strings.TrimSuffix(inst, "/")
		}
		if len(s.instances) > 0 {
			s.baseURL = s.instances[0]
		}
	}
}

// NewSearXNG creates a new SearXNG search provider
func NewSearXNG(opts ...SearXNGOption) *SearXNG {
	s := &SearXNG{
		timeout:   10 * time.Second,
		instances: defaultSearXNGInstances,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	if len(s.instances) > 0 {
		s.baseURL = s.instances[0]
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Name returns the provider name
func (s *SearXNG) Name() string {
	return "SearXNG"
}

// IsAvailable checks if any SearXNG instance is reachable
func (s *SearXNG) IsAvailable(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Try each instance until one responds
	for _, instance := range s.instances {
		req, err := http.NewRequestWithContext(checkCtx, "HEAD", instance, nil)
		if err != nil {
			continue
		}

		resp, err := s.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMethodNotAllowed {
			s.baseURL = instance // Use this instance
			return true
		}
	}

	return false
}

// searxngResponse represents the SearXNG JSON API response
type searxngResponse struct {
	Query           string `json:"query"`
	NumberOfResults int    `json:"number_of_results"`
	Results         []struct {
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		Content       string   `json:"content"`
		Engine        string   `json:"engine"`
		Engines       []string `json:"engines"`
		Score         float64  `json:"score"`
		Category      string   `json:"category"`
		PrettyURL     string   `json:"pretty_url"`
		PublishedDate string   `json:"publishedDate,omitempty"`
	} `json:"results"`
	Answers   []string `json:"answers"`
	Infoboxes []struct {
		Infobox string `json:"infobox"`
		ID      string `json:"id"`
		Content string `json:"content"`
		URLs    []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"urls"`
	} `json:"infoboxes"`
	Suggestions []string `json:"suggestions"`
}

// Search performs a search using SearXNG
func (s *SearXNG) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	var lastErr error

	// Try each instance until one succeeds
	for _, instance := range s.instances {
		response, err := s.searchInstance(ctx, instance, query, numResults)
		if err == nil {
			s.baseURL = instance // Remember working instance
			return response, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all SearXNG instances failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no SearXNG instances configured")
}

// searchInstance performs a search on a specific SearXNG instance
func (s *SearXNG) searchInstance(ctx context.Context, instance, query string, numResults int) (*SearchResponse, error) {
	// Build search URL with parameters
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("categories", "general")
	params.Set("language", "en")

	searchURL := instance + "/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", "taracode/1.0 (+https://tara.vision)")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SearXNG request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limiting or errors
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (HTTP 429)")
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("access forbidden - instance may block JSON API")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SearXNG returned status %d", resp.StatusCode)
	}

	var sxResp searxngResponse
	if err := json.NewDecoder(resp.Body).Decode(&sxResp); err != nil {
		return nil, fmt.Errorf("failed to parse SearXNG response: %w", err)
	}

	response := &SearchResponse{
		Query:    query,
		Provider: "SearXNG",
		Results:  make([]SearchResult, 0, numResults),
	}

	// Add instant answer from infoboxes or answers
	if len(sxResp.Answers) > 0 {
		response.InstantAnswer = strings.Join(sxResp.Answers, " ")
	} else if len(sxResp.Infoboxes) > 0 {
		response.InstantAnswer = sxResp.Infoboxes[0].Content
	}

	// Add search results
	seen := make(map[string]bool)
	for _, result := range sxResp.Results {
		if len(response.Results) >= numResults {
			break
		}

		// Skip duplicates
		if seen[result.URL] {
			continue
		}
		seen[result.URL] = true

		// Skip empty titles
		if result.Title == "" {
			continue
		}

		response.Results = append(response.Results, SearchResult{
			Title:   result.Title,
			URL:     result.URL,
			Snippet: result.Content,
			Source:  result.Engine,
		})
	}

	return response, nil
}
