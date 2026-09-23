package tools

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/search"
)

const (
	webFetchTimeout  = 15 * time.Second
	webFetchMaxBytes = 2 << 20
	webFetchMaxChars = 50000
	webUserAgent     = "taracode/3.0 (+https://tara.vision)"
	defaultSearchN   = 5
	maxSearchN       = 10
)

// WebSearchTool searches the web through the configured provider chain.
func WebSearchTool(orch *search.Orchestrator) *Tool {
	return &Tool{
		Name: "web_search", ReadForm: true, External: true,
		Description: "Search the web (DuckDuckGo, Brave or SearXNG as configured) and return titles, URLs and snippets.",
		Params: []Param{
			{Name: "query", Type: "string", Description: "Search query", Required: true},
			{Name: "max", Type: "integer", Description: "Results to return (default 5, max 10)"},
		},
		Classify: readOnly("web_search"),
		Run: func(ctx context.Context, args map[string]any, _ string) (string, error) {
			query, err := required(args, "query")
			if err != nil {
				return "", err
			}
			if orch == nil {
				return "", fmt.Errorf("web search is not configured")
			}
			n := argInt(args, "max", defaultSearchN)
			if n < 1 || n > maxSearchN {
				n = defaultSearchN
			}
			ctx, cancel := withTimeout(ctx, 30*time.Second)
			defer cancel()
			resp, err := orch.Search(ctx, query, n)
			if err != nil {
				return "", fmt.Errorf("search failed: %w", err)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Results for %q (%s):\n", query, resp.Provider)
			if resp.InstantAnswer != "" {
				fmt.Fprintf(&b, "\nAnswer: %s\n", resp.InstantAnswer)
			}
			for i, r := range resp.Results {
				fmt.Fprintf(&b, "\n%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Snippet)
			}
			if len(resp.Results) == 0 && resp.InstantAnswer == "" {
				b.WriteString("\nNo results.\n")
			}
			return b.String(), nil
		},
	}
}

// WebFetchTool downloads a page over http or https and returns its readable text.
func WebFetchTool() *Tool {
	return &Tool{
		Name: "web_fetch", ReadForm: true, External: true,
		Description: "Fetch a public http(s) URL and return its readable text (HTML tags stripped).",
		Params:      []Param{{Name: "url", Type: "string", Description: "Absolute URL", Required: true}},
		Classify: func(args map[string]any, _ string) policy.Invocation {
			inv := policy.Invocation{Tool: "web_fetch", Classification: policy.Read}
			if u, err := url.Parse(argString(args, "url")); err == nil && u.Hostname() != "" {
				inv.Targets.Hosts = []string{u.Hostname()}
			}
			return inv
		},
		Run: func(ctx context.Context, args map[string]any, _ string) (string, error) {
			raw, err := required(args, "url")
			if err != nil {
				return "", err
			}
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return "", fmt.Errorf("url must be an absolute http or https URL")
			}
			client := &http.Client{Timeout: webFetchTimeout, Transport: safeTransport(webFetchTimeout),
				CheckRedirect: func(_ *http.Request, via []*http.Request) error {
					if len(via) >= 5 {
						return fmt.Errorf("too many redirects")
					}
					return nil
				}}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", webUserAgent)
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8")
			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("fetch failed: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return "", fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBytes))
			if err != nil {
				return "", err
			}
			text := string(body)
			if strings.Contains(resp.Header.Get("Content-Type"), "html") {
				text = htmlToText(text)
			}
			if len(text) > webFetchMaxChars {
				text = text[:webFetchMaxChars] + "\n[truncated]"
			}
			return text, nil
		},
	}
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	reBlocks = regexp.MustCompile(`(?i)</(p|div|br|li|h[1-6]|tr|section|article)>`)
	reTags   = regexp.MustCompile(`(?s)<[^>]+>`)
	reBlank  = regexp.MustCompile(`\n\s*\n+`)
	reSpaces = regexp.MustCompile(`[ \t]+`)
)

// htmlToText strips scripts, styles and tags and decodes entities.
func htmlToText(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reBlocks.ReplaceAllString(s, "\n")
	s = reTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reBlank.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}
