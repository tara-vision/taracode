package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	duckduckgoAPI  = "https://api.duckduckgo.com/"
	duckduckgoHTML = "https://html.duckduckgo.com/html/"
)

// DuckDuckGo implements the Provider interface using DuckDuckGo search
type DuckDuckGo struct {
	client  *http.Client
	timeout time.Duration
}

// DuckDuckGoOption configures DuckDuckGo provider
type DuckDuckGoOption func(*DuckDuckGo)

// WithTimeout sets the HTTP timeout for DuckDuckGo requests
func WithTimeout(timeout time.Duration) DuckDuckGoOption {
	return func(d *DuckDuckGo) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// NewDuckDuckGo creates a new DuckDuckGo search provider
func NewDuckDuckGo(opts ...DuckDuckGoOption) *DuckDuckGo {
	d := &DuckDuckGo{
		timeout: 15 * time.Second,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// Name returns the provider name
func (d *DuckDuckGo) Name() string {
	return "DuckDuckGo"
}

// IsAvailable checks if DuckDuckGo is reachable
func (d *DuckDuckGo) IsAvailable(ctx context.Context) bool {
	// Quick health check using the API with a simple query
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, "HEAD", duckduckgoAPI, nil)
	if err != nil {
		return false
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Accept 200 and 405 (Method Not Allowed for HEAD) as available
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMethodNotAllowed
}

// duckduckgoResponse represents the Instant Answer API response structure
type duckduckgoResponse struct {
	Abstract       string `json:"Abstract"`
	AbstractSource string `json:"AbstractSource"`
	AbstractURL    string `json:"AbstractURL"`
	AbstractText   string `json:"AbstractText"`
	Answer         string `json:"Answer"`
	AnswerType     string `json:"AnswerType"`
	Definition     string `json:"Definition"`
	DefinitionURL  string `json:"DefinitionURL"`
	Heading        string `json:"Heading"`
	RelatedTopics  []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
		Icon     struct {
			URL string `json:"URL"`
		} `json:"Icon"`
		Result string `json:"Result"`
		Topics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Topics,omitempty"`
		Name string `json:"Name,omitempty"`
	} `json:"RelatedTopics"`
	Results []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"Results"`
}

// Search performs a search using DuckDuckGo
// It tries the HTML search first (for web results), then falls back to Instant Answer API
func (d *DuckDuckGo) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// Try HTML search first (provides actual web results)
	response, err := d.searchHTML(ctx, query, numResults)
	if err == nil && len(response.Results) > 0 {
		return response, nil
	}

	// Fall back to Instant Answer API for definitions/quick answers
	return d.searchInstantAnswer(ctx, query, numResults)
}

// searchHTML scrapes DuckDuckGo HTML search page for web results
func (d *DuckDuckGo) searchHTML(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	// Build form data for POST request
	formData := url.Values{}
	formData.Set("q", query)
	formData.Set("b", "")  // Start from first result
	formData.Set("kl", "") // No region filter

	req, err := http.NewRequestWithContext(ctx, "POST", duckduckgoHTML, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to look like a simple browser
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; taracode/1.0; +https://tara.vision)")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (HTTP 429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	html := string(body)

	// Parse HTML results
	results := d.parseHTMLResults(html, numResults)

	response := &SearchResponse{
		Query:    query,
		Provider: "DuckDuckGo",
		Results:  results,
	}

	return response, nil
}

// parseHTMLResults extracts search results from DuckDuckGo HTML
func (d *DuckDuckGo) parseHTMLResults(html string, numResults int) []SearchResult {
	var results []SearchResult

	// Pattern to match result blocks
	// DuckDuckGo HTML uses class="result__a" for result links
	// and class="result__snippet" for descriptions
	resultPattern := regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]*href="([^"]+)"[^>]*>([^<]+)</a>.*?<a[^>]+class="result__snippet"[^>]*>([^<]*(?:<[^>]+>[^<]*)*)</a>`)

	matches := resultPattern.FindAllStringSubmatch(html, -1)

	for _, match := range matches {
		if len(results) >= numResults {
			break
		}

		if len(match) >= 4 {
			urlStr := match[1]
			title := strings.TrimSpace(match[2])
			snippet := cleanHTML(match[3])

			// Skip ad results and empty titles
			if title == "" || strings.Contains(urlStr, "duckduckgo.com/y.js") {
				continue
			}

			// Extract actual URL from DuckDuckGo redirect
			actualURL := extractActualURL(urlStr)
			if actualURL == "" {
				actualURL = urlStr
			}

			results = append(results, SearchResult{
				Title:   decodeHTMLEntities(title),
				URL:     actualURL,
				Snippet: decodeHTMLEntities(snippet),
			})
		}
	}

	// If first pattern didn't work, try alternative pattern
	if len(results) == 0 {
		// Alternative: match result__url and result__title separately
		linkPattern := regexp.MustCompile(`<a[^>]+class="[^"]*result__url[^"]*"[^>]*href="([^"]+)"`)
		titlePattern := regexp.MustCompile(`<a[^>]+class="[^"]*result__a[^"]*"[^>]*>([^<]+)</a>`)
		snippetPattern := regexp.MustCompile(`<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>([^<]*(?:<[^>]+>[^<]*)*)</a>`)

		links := linkPattern.FindAllStringSubmatch(html, -1)
		titles := titlePattern.FindAllStringSubmatch(html, -1)
		snippets := snippetPattern.FindAllStringSubmatch(html, -1)

		maxLen := len(links)
		if len(titles) < maxLen {
			maxLen = len(titles)
		}

		for i := 0; i < maxLen && len(results) < numResults; i++ {
			urlStr := ""
			if i < len(links) && len(links[i]) > 1 {
				urlStr = extractActualURL(links[i][1])
			}

			title := ""
			if i < len(titles) && len(titles[i]) > 1 {
				title = strings.TrimSpace(titles[i][1])
			}

			snippet := ""
			if i < len(snippets) && len(snippets[i]) > 1 {
				snippet = cleanHTML(snippets[i][1])
			}

			if title != "" && urlStr != "" {
				results = append(results, SearchResult{
					Title:   decodeHTMLEntities(title),
					URL:     urlStr,
					Snippet: decodeHTMLEntities(snippet),
				})
			}
		}
	}

	// Third fallback: simpler regex for any links with titles
	if len(results) == 0 {
		simplePattern := regexp.MustCompile(`<a[^>]+href="(https?://[^"]+)"[^>]*>([^<]{10,100})</a>`)
		matches := simplePattern.FindAllStringSubmatch(html, -1)

		seen := make(map[string]bool)
		for _, match := range matches {
			if len(results) >= numResults {
				break
			}
			if len(match) >= 3 {
				urlStr := match[1]
				title := strings.TrimSpace(match[2])

				// Skip DDG internal links
				if strings.Contains(urlStr, "duckduckgo.com") {
					continue
				}

				// Skip duplicates
				if seen[urlStr] {
					continue
				}
				seen[urlStr] = true

				results = append(results, SearchResult{
					Title:   decodeHTMLEntities(title),
					URL:     urlStr,
					Snippet: "",
				})
			}
		}
	}

	return results
}

// extractActualURL extracts the actual URL from DuckDuckGo redirect URLs
func extractActualURL(ddgURL string) string {
	// DuckDuckGo uses redirect URLs like //duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com
	if strings.Contains(ddgURL, "uddg=") {
		if parsed, err := url.Parse(ddgURL); err == nil {
			if uddg := parsed.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
		// Try to extract manually
		if idx := strings.Index(ddgURL, "uddg="); idx >= 0 {
			encoded := ddgURL[idx+5:]
			if ampIdx := strings.Index(encoded, "&"); ampIdx > 0 {
				encoded = encoded[:ampIdx]
			}
			if decoded, err := url.QueryUnescape(encoded); err == nil {
				return decoded
			}
		}
	}

	// Handle protocol-relative URLs
	if strings.HasPrefix(ddgURL, "//") {
		return "https:" + ddgURL
	}

	return ddgURL
}

// cleanHTML removes HTML tags and cleans up text
func cleanHTML(s string) string {
	// Remove HTML tags
	tagRe := regexp.MustCompile(`<[^>]+>`)
	s = tagRe.ReplaceAllString(s, "")

	// Clean up whitespace
	s = strings.Join(strings.Fields(s), " ")

	return strings.TrimSpace(s)
}

// decodeHTMLEntities decodes common HTML entities
func decodeHTMLEntities(s string) string {
	entities := map[string]string{
		"&amp;":    "&",
		"&lt;":     "<",
		"&gt;":     ">",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&#39;":    "'",
		"&nbsp;":   " ",
		"&ndash;":  "-",
		"&mdash;":  "-",
		"&lsquo;":  "'",
		"&rsquo;":  "'",
		"&ldquo;":  "\"",
		"&rdquo;":  "\"",
		"&hellip;": "...",
		"&copy;":   "(c)",
		"&reg;":    "(R)",
		"&trade;":  "(TM)",
	}

	for entity, replacement := range entities {
		s = strings.ReplaceAll(s, entity, replacement)
	}

	// Handle numeric entities
	numericRe := regexp.MustCompile(`&#(\d+);`)
	s = numericRe.ReplaceAllStringFunc(s, func(match string) string {
		var num int
		fmt.Sscanf(match, "&#%d;", &num)
		if num > 0 && num < 128 {
			return string(rune(num))
		}
		return match
	})

	return s
}

// searchInstantAnswer uses the Instant Answer API for definitions and quick answers
func (d *DuckDuckGo) searchInstantAnswer(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
	// Build URL with parameters
	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("no_html", "1")
	params.Set("skip_disambig", "1")

	req, err := http.NewRequestWithContext(ctx, "GET", duckduckgoAPI+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DuckDuckGo request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited (HTTP 429)")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	var ddgResp duckduckgoResponse
	if err := json.NewDecoder(resp.Body).Decode(&ddgResp); err != nil {
		return nil, fmt.Errorf("failed to parse DuckDuckGo response: %w", err)
	}

	response := &SearchResponse{
		Query:    query,
		Provider: "DuckDuckGo",
		Results:  make([]SearchResult, 0),
	}

	// Add instant answer if available
	if ddgResp.Answer != "" {
		response.InstantAnswer = ddgResp.Answer
	} else if ddgResp.Abstract != "" {
		response.InstantAnswer = ddgResp.Abstract
	} else if ddgResp.Definition != "" {
		response.InstantAnswer = ddgResp.Definition
	}

	// Add abstract as first result if available
	if ddgResp.Abstract != "" && ddgResp.AbstractURL != "" {
		title := ddgResp.Heading
		if title == "" {
			title = ddgResp.AbstractSource
		}
		response.Results = append(response.Results, SearchResult{
			Title:   title,
			URL:     ddgResp.AbstractURL,
			Snippet: ddgResp.Abstract,
			Source:  ddgResp.AbstractSource,
		})
	}

	// Add direct results
	for _, result := range ddgResp.Results {
		if len(response.Results) >= numResults {
			break
		}
		if result.Text != "" && result.FirstURL != "" {
			response.Results = append(response.Results, SearchResult{
				Title:   extractTitle(result.Text),
				URL:     result.FirstURL,
				Snippet: result.Text,
			})
		}
	}

	// Add related topics
	for _, topic := range ddgResp.RelatedTopics {
		if len(response.Results) >= numResults {
			break
		}

		if len(topic.Topics) > 0 {
			for _, nested := range topic.Topics {
				if len(response.Results) >= numResults {
					break
				}
				if nested.Text != "" && nested.FirstURL != "" {
					response.Results = append(response.Results, SearchResult{
						Title:   extractTitle(nested.Text),
						URL:     nested.FirstURL,
						Snippet: nested.Text,
					})
				}
			}
			continue
		}

		if topic.Text != "" && topic.FirstURL != "" {
			response.Results = append(response.Results, SearchResult{
				Title:   extractTitle(topic.Text),
				URL:     topic.FirstURL,
				Snippet: topic.Text,
			})
		}
	}

	return response, nil
}

// extractTitle extracts a title from DuckDuckGo text format
func extractTitle(text string) string {
	if idx := strings.Index(text, " - "); idx > 0 && idx < 100 {
		return strings.TrimSpace(text[:idx])
	}

	if len(text) > 60 {
		if idx := strings.LastIndex(text[:60], " "); idx > 30 {
			return text[:idx] + "..."
		}
		return text[:60] + "..."
	}

	return text
}
