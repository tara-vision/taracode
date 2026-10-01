package search

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ddgResultsPage is an HTML results page in DuckDuckGo's markup with one result: a redirect link
// with entities in its title and tags in its snippet.
const ddgResultsPage = `<div class="result">
<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fkubernetes.io%2Fdocs%2Fpods&amp;rut=1">Pods &amp; containers</a>
<a class="result__snippet" href="#">Pods are the <b>smallest</b>   deployable units &#39;here&#39;</a>
</div>`

func TestDuckDuckGoParsesTheHTMLResults(t *testing.T) {
	web := newFakeWeb(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(ddgResultsPage)) })
	d := NewDuckDuckGo()
	d.client = web.client()
	resp, err := d.Search(context.Background(), "kubernetes pods", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []SearchResult{
		{Title: "Pods & containers", URL: "https://kubernetes.io/docs/pods", Snippet: "Pods are the smallest deployable units 'here'"},
	}
	if resp.Provider != "DuckDuckGo" || resp.Query != "kubernetes pods" || fmt.Sprint(resp.Results) != fmt.Sprint(want) {
		t.Fatalf("response %+v", resp)
	}
	req := web.seen()[0]
	if req.Method != http.MethodPost || req.Host != "html.duckduckgo.com" || req.Path != "/html/" ||
		req.Form.Get("q") != "kubernetes pods" || !strings.Contains(req.Header.Get("User-Agent"), "taracode") {
		t.Fatalf("request %+v", req)
	}
	if got := d.parseHTMLResults(ddgResultsPage, 0); len(got) != 0 {
		t.Fatalf("zero results asked for: %+v", got)
	}
}

// TestParseHTMLResultsSkipsAnAdvert: a page whose only result is an ad yields nothing.
func TestParseHTMLResultsSkipsAnAdvert(t *testing.T) {
	ad := `<a rel="nofollow" class="result__a" href="https://duckduckgo.com/y.js?ad_provider=x">Buy the best boots</a>
<a class="result__snippet" href="#">An advert</a>`
	if got := NewDuckDuckGo().parseHTMLResults(ad, 5); len(got) != 0 {
		t.Fatalf("results %+v", got)
	}
}

// TestParseHTMLResultsFallsBackToLooserPatterns covers the two fallbacks: separate url, title and
// snippet anchors when the combined pattern finds nothing (paired by position), then any external
// link with a title.
func TestParseHTMLResultsFallsBackToLooserPatterns(t *testing.T) {
	d := NewDuckDuckGo()
	separate := `<a href="//x" class="result__a">Helm docs</a>
<a class="result__url" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fhelm.sh%2Fdocs">helm.sh/docs</a>
<a href="//y" class="result__a">Second title</a>
<a class="result__url" href="//example.com/second">example.com/second</a>
<a class="result__snippet" href="#">The <b>Helm</b> documentation</a>`
	got := d.parseHTMLResults(separate, 5)
	want := []SearchResult{
		{Title: "Helm docs", URL: "https://helm.sh/docs", Snippet: "The Helm documentation"},
		{Title: "Second title", URL: "https://example.com/second"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("separate anchors: %+v", got)
	}
	if got := d.parseHTMLResults(separate, 1); len(got) != 1 {
		t.Fatalf("the limit applies to the fallback: %+v", got)
	}

	generic := `<a href="https://duckduckgo.com/settings">DuckDuckGo settings</a>
<a href="https://terraform.io/docs/state">Terraform state docs</a>
<a href="https://terraform.io/docs/state">Terraform state docs</a>
<a href="https://kubernetes.io/blog/news">Kubernetes blog news</a>
<a href="https://short.example">tiny</a>`
	got = d.parseHTMLResults(generic, 5)
	want = []SearchResult{
		{Title: "Terraform state docs", URL: "https://terraform.io/docs/state"},
		{Title: "Kubernetes blog news", URL: "https://kubernetes.io/blog/news"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("generic links: %+v", got)
	}
	if got := d.parseHTMLResults(generic, 1); len(got) != 1 {
		t.Fatalf("the limit applies to the generic links: %+v", got)
	}
	if got := d.parseHTMLResults("<p>nothing</p>", 5); len(got) != 0 {
		t.Fatalf("a page without links: %+v", got)
	}
}

func TestExtractActualURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&rut=x", "https://example.com/a"},
		{"://bad?uddg=https%3A%2F%2Fexample.com%2Fp&rut=1", "https://example.com/p"},
		{"://bad?uddg=%zz", "://bad?uddg=%zz"},
		{"//example.com/b", "https://example.com/b"},
		{"https://example.com/c", "https://example.com/c"},
	}
	for _, tt := range tests {
		if got := extractActualURL(tt.in); got != tt.want {
			t.Errorf("extractActualURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecodeHTMLEntitiesAndCleanHTML(t *testing.T) {
	in := "a &amp; b &lt;c&gt; &quot;d&quot; &#39;e&#39; &copy; &hellip; &#65; &#8212;"
	if got := decodeHTMLEntities(in); got != `a & b <c> "d" 'e' (c) ... A &#8212;` {
		t.Fatalf("decodeHTMLEntities() = %q", got)
	}
	if got := cleanHTML("  <b>bold</b>\n\t text <i>here</i> "); got != "bold text here" {
		t.Fatalf("cleanHTML() = %q", got)
	}
}

func TestExtractTitleShortensLongText(t *testing.T) {
	tests := []struct {
		text, want string
	}{
		{"Go - A programming language", "Go"},
		{"Short title", "Short title"},
		{"Kubernetes is an open source system for automating deployment of apps", "Kubernetes is an open source system for automating..."},
		{strings.Repeat("x", 70), strings.Repeat("x", 60) + "..."},
	}
	for _, tt := range tests {
		if got := extractTitle(tt.text); got != tt.want {
			t.Errorf("extractTitle(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// TestParseHTMLResultsKeepsAnEmptyRedirect: a redirect with no target keeps the link as it is.
func TestParseHTMLResultsKeepsAnEmptyRedirect(t *testing.T) {
	page := `<a class="result__a" href="//duckduckgo.com/l/?uddg=">Lost target</a><a class="result__snippet" href="#">s</a>`
	got := NewDuckDuckGo().parseHTMLResults(page, 5)
	if len(got) != 1 || got[0].URL != "//duckduckgo.com/l/?uddg=" {
		t.Fatalf("%+v", got)
	}
}

// TestParseHTMLResultsPairsNoMoreThanTheTitles: separate anchors with more urls than titles.
func TestParseHTMLResultsPairsNoMoreThanTheTitles(t *testing.T) {
	page := `<a href="//x" class="result__a">Only title</a>
<a class="result__url" href="https://one.example">one</a>
<a class="result__url" href="https://two.example">two</a>`
	got := NewDuckDuckGo().parseHTMLResults(page, 5)
	if len(got) != 1 || got[0].Title != "Only title" || got[0].URL != "https://one.example" {
		t.Fatalf("%+v", got)
	}
}
