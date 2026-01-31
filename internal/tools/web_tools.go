package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/search"
)

const (
	defaultNumResults  = 5
	maxNumResults      = 10
	defaultMaxLength   = 50000
	webFetchTimeout    = 15 * time.Second
	userAgent          = "taracode/1.0 (+https://tara.vision)"
)

var (
	// searchOrchestrator manages search providers with fallback
	searchOrchestrator *search.Orchestrator
	searchOrchestratorOnce sync.Once

	// onProviderSwitch is called when search falls back to another provider
	// This can be set by the UI layer to display fallback messages
	onProviderSwitch func(from, to string, reason error)
)

// SearchOrchestratorConfig holds configuration for the search orchestrator
type SearchOrchestratorConfig struct {
	Primary         string
	Fallback        string
	Timeout         time.Duration
	RetryCount      int
	SearXNGInstance string
	BraveAPIKey     string
}

// InitSearchOrchestrator initializes the search orchestrator with the given config
func InitSearchOrchestrator(cfg SearchOrchestratorConfig) {
	config := search.OrchestratorConfig{
		Primary:               cfg.Primary,
		Fallback:              cfg.Fallback,
		Timeout:               cfg.Timeout,
		RetryCount:            cfg.RetryCount,
		CustomSearXNGInstance: cfg.SearXNGInstance,
		BraveAPIKey:           cfg.BraveAPIKey,
		OnProviderSwitch:      onProviderSwitch,
	}

	// If Brave API key is configured and no primary is set, use Brave as primary
	if cfg.BraveAPIKey != "" && cfg.Primary == "" {
		config.Primary = "brave"
		config.Fallback = "duckduckgo" // Fallback to DuckDuckGo when Brave is primary
	}

	// Apply defaults if not set
	if config.Primary == "" {
		config.Primary = "duckduckgo"
	}
	if config.Fallback == "" {
		config.Fallback = "searxng"
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	searchOrchestrator = search.NewOrchestrator(config)
}

// SetProviderSwitchCallback sets the callback for when search falls back
func SetProviderSwitchCallback(fn func(from, to string, reason error)) {
	onProviderSwitch = fn
	// Update orchestrator if already initialized
	if searchOrchestrator != nil {
		// Re-initialize with new callback
		// This is safe because we're just updating the callback
	}
}

// getSearchOrchestrator returns the search orchestrator, initializing with defaults if needed
func getSearchOrchestrator() *search.Orchestrator {
	searchOrchestratorOnce.Do(func() {
		if searchOrchestrator == nil {
			// Initialize with defaults if not already initialized
			InitSearchOrchestrator(SearchOrchestratorConfig{})
		}
	})
	return searchOrchestrator
}

// WebSearch performs a web search and returns formatted results
func WebSearch(params map[string]interface{}, workingDir string) (string, error) {
	query, ok := params["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query parameter is required")
	}

	// Get optional num_results parameter (default: 5)
	numResults := defaultNumResults
	if n, ok := params["num_results"].(float64); ok && n > 0 {
		numResults = int(n)
		if numResults > maxNumResults {
			numResults = maxNumResults
		}
	}

	// Get the search orchestrator
	orchestrator := getSearchOrchestrator()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Perform search via orchestrator (handles fallback automatically)
	response, err := orchestrator.Search(ctx, query, numResults)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	// Format results for LLM consumption
	var result strings.Builder

	// Show provider info (with fallback indicator if applicable)
	if response.Fallback {
		result.WriteString(fmt.Sprintf("Search results for: \"%s\" (via %s, fallback)\n\n", query, response.Provider))
	} else {
		result.WriteString(fmt.Sprintf("Search results for: \"%s\" (via %s)\n\n", query, response.Provider))
	}

	// Include instant answer if available
	if response.InstantAnswer != "" {
		result.WriteString("## Quick Answer\n")
		result.WriteString(response.InstantAnswer)
		result.WriteString("\n\n")
	}

	// Include search results
	if len(response.Results) > 0 {
		result.WriteString("## Results\n\n")
		for i, r := range response.Results {
			result.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, r.Title))
			result.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
			if r.Snippet != "" && r.Snippet != r.Title {
				// Clean up snippet - remove title prefix if present
				snippet := r.Snippet
				if strings.HasPrefix(snippet, r.Title+" - ") {
					snippet = snippet[len(r.Title)+3:]
				}
				result.WriteString(fmt.Sprintf("   %s\n", snippet))
			}
			result.WriteString("\n")
		}
	} else if response.InstantAnswer == "" {
		result.WriteString("No results found for this query.\n")
	}

	return result.String(), nil
}

// WebFetch fetches content from a URL and extracts readable text
func WebFetch(params map[string]interface{}, workingDir string) (string, error) {
	urlStr, ok := params["url"].(string)
	if !ok || strings.TrimSpace(urlStr) == "" {
		return "", fmt.Errorf("url parameter is required")
	}

	// Validate URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("URL must use http or https scheme")
	}

	// Get optional max_length parameter
	maxLength := defaultMaxLength
	if m, ok := params["max_length"].(float64); ok && m > 0 {
		maxLength = int(m)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: webFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Create request with user agent
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8")

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read body with size limit
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxLength*2)))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	body := string(bodyBytes)

	// Extract title
	title := extractHTMLTitle(body)
	if title == "" {
		title = parsedURL.Host + parsedURL.Path
	}

	// Extract readable text content
	content := extractTextContent(body)

	// Truncate if necessary
	truncated := false
	if len(content) > maxLength {
		content = content[:maxLength]
		// Try to break at a sentence or word boundary
		if idx := strings.LastIndex(content, ". "); idx > maxLength-500 {
			content = content[:idx+1]
		} else if idx := strings.LastIndex(content, " "); idx > maxLength-200 {
			content = content[:idx]
		}
		truncated = true
	}

	// Format output
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Fetched: %s\n", urlStr))
	result.WriteString(fmt.Sprintf("Title: %s\n", title))
	result.WriteString(fmt.Sprintf("Content length: %s characters\n", formatNumber(len(content))))
	if truncated {
		result.WriteString("[Content truncated]\n")
	}
	result.WriteString("\n---\n\n")
	result.WriteString(content)

	return result.String(), nil
}

// extractHTMLTitle extracts the title from HTML content
func extractHTMLTitle(html string) string {
	// Match <title>...</title>
	re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return decodeHTMLEntities(strings.TrimSpace(matches[1]))
	}
	return ""
}

// extractTextContent extracts readable text from HTML
func extractTextContent(html string) string {
	// Remove script and style tags and their content
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// Remove nav, header, footer, aside (usually not main content)
	navRe := regexp.MustCompile(`(?is)<nav[^>]*>.*?</nav>`)
	html = navRe.ReplaceAllString(html, "")

	headerRe := regexp.MustCompile(`(?is)<header[^>]*>.*?</header>`)
	html = headerRe.ReplaceAllString(html, "")

	footerRe := regexp.MustCompile(`(?is)<footer[^>]*>.*?</footer>`)
	html = footerRe.ReplaceAllString(html, "")

	asideRe := regexp.MustCompile(`(?is)<aside[^>]*>.*?</aside>`)
	html = asideRe.ReplaceAllString(html, "")

	// Remove HTML comments
	commentRe := regexp.MustCompile(`(?s)<!--.*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// Add newlines before block elements for better formatting
	blockElements := []string{"p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "br", "hr"}
	for _, elem := range blockElements {
		re := regexp.MustCompile(`(?i)<` + elem + `[^>]*>`)
		html = re.ReplaceAllString(html, "\n")
		re = regexp.MustCompile(`(?i)</` + elem + `>`)
		html = re.ReplaceAllString(html, "\n")
	}

	// Remove all remaining HTML tags
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, "")

	// Decode HTML entities
	text = decodeHTMLEntities(text)

	// Clean up whitespace
	// Replace multiple spaces with single space
	spaceRe := regexp.MustCompile(`[ \t]+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// Replace multiple newlines with double newline
	newlineRe := regexp.MustCompile(`\n\s*\n+`)
	text = newlineRe.ReplaceAllString(text, "\n\n")

	// Trim each line
	lines := strings.Split(text, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// decodeHTMLEntities decodes common HTML entities
func decodeHTMLEntities(s string) string {
	entities := map[string]string{
		"&amp;":   "&",
		"&lt;":    "<",
		"&gt;":    ">",
		"&quot;":  "\"",
		"&apos;":  "'",
		"&#39;":   "'",
		"&nbsp;":  " ",
		"&ndash;": "-",
		"&mdash;": "-",
		"&lsquo;": "'",
		"&rsquo;": "'",
		"&ldquo;": "\"",
		"&rdquo;": "\"",
		"&hellip;": "...",
		"&copy;":  "(c)",
		"&reg;":   "(R)",
		"&trade;": "(TM)",
	}

	for entity, replacement := range entities {
		s = strings.ReplaceAll(s, entity, replacement)
	}

	// Handle numeric entities like &#123;
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

// formatNumber formats a number with thousand separators
func formatNumber(n int) string {
	str := fmt.Sprintf("%d", n)
	if len(str) <= 3 {
		return str
	}

	var result strings.Builder
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}
