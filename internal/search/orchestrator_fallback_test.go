package search

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedProvider answers each Search with the next error of errs (nil = a result), then succeeds.
type scriptedProvider struct {
	name      string
	errs      []error
	calls     int
	available bool
}

func (p *scriptedProvider) Name() string                     { return p.name }
func (p *scriptedProvider) IsAvailable(context.Context) bool { return p.available }

func (p *scriptedProvider) Search(_ context.Context, query string, _ int) (*SearchResponse, error) {
	p.calls++
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &SearchResponse{Query: query, Provider: p.name}, nil
}

// orchestratorWith builds an orchestrator on the two providers (fallback may be nil).
func orchestratorWith(primary, fallback Provider, retries int, onSwitch func(from, to string, reason error)) *Orchestrator {
	o := &Orchestrator{config: OrchestratorConfig{Timeout: 10 * time.Second, RetryCount: retries, OnProviderSwitch: onSwitch},
		primary: primary}
	if fallback != nil {
		o.fallback = fallback
	}
	return o
}

func TestNewOrchestratorChoosesTheProviders(t *testing.T) {
	brave := NewOrchestrator(OrchestratorConfig{Primary: "BRAVE", Fallback: "duckduckgo", BraveAPIKey: "key",
		CustomSearXNGInstance: "https://searx.example"})
	if brave.Name() != "Orchestrator(Brave->DuckDuckGo)" {
		t.Fatalf("%s", brave.Name())
	}
	if sx, ok := brave.providers["searxng"].(*SearXNG); !ok || sx.baseURL != "https://searx.example" {
		t.Fatalf("the custom SearXNG instance: %+v", brave.providers["searxng"])
	}
	tests := []struct {
		cfg  OrchestratorConfig
		want string
	}{
		{OrchestratorConfig{Primary: "brave", Fallback: "searxng"}, "Orchestrator(DuckDuckGo->SearXNG)"}, // no key, no Brave
		{OrchestratorConfig{Primary: "searxng", Fallback: "searxng"}, "Orchestrator(SearXNG->none)"},
		{OrchestratorConfig{Primary: "duckduckgo", Fallback: "altavista"}, "Orchestrator(DuckDuckGo->none)"},
	}
	for _, tt := range tests {
		if got := NewOrchestrator(tt.cfg).Name(); got != tt.want {
			t.Errorf("%+v: %s, want %s", tt.cfg, got, tt.want)
		}
	}
}

func TestOrchestratorSearchUsesThePrimary(t *testing.T) {
	primary, fallback := &scriptedProvider{name: "P"}, &scriptedProvider{name: "F"}
	resp, err := orchestratorWith(primary, fallback, 0, nil).Search(context.Background(), "q", 3)
	if err != nil || resp.Provider != "P" || resp.Fallback || fallback.calls != 0 {
		t.Fatalf("%+v %v, fallback calls %d", resp, err, fallback.calls)
	}
}

func TestOrchestratorSearchFallsBack(t *testing.T) {
	primary := &scriptedProvider{name: "P", errs: []error{errors.New("HTTP 429 too many requests")}}
	fallback := &scriptedProvider{name: "F"}
	var switched []string
	onSwitch := func(from, to string, reason error) { switched = append(switched, from+"->"+to+": "+reason.Error()) }
	resp, err := orchestratorWith(primary, fallback, 3, onSwitch).Search(context.Background(), "q", 3)
	if err != nil || resp.Provider != "F" || !resp.Fallback {
		t.Fatalf("%+v %v", resp, err)
	}
	if primary.calls != 1 || strings.Join(switched, "|") != "P->F: HTTP 429 too many requests" {
		t.Fatalf("a rate limit falls back at once: %d calls, switches %v", primary.calls, switched)
	}

	// Without a callback the fallback still answers.
	primary = &scriptedProvider{name: "P", errs: []error{errors.New("rate limit")}}
	if resp, err := orchestratorWith(primary, &scriptedProvider{name: "F"}, 0, nil).Search(context.Background(), "q", 3); err != nil ||
		!resp.Fallback {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestOrchestratorSearchFailures(t *testing.T) {
	primary := &scriptedProvider{name: "P", errs: []error{errors.New("down")}}
	_, err := orchestratorWith(primary, nil, 0, nil).Search(context.Background(), "q", 3)
	var searchErr *SearchError
	if !errors.As(err, &searchErr) || searchErr.Provider != "P" || searchErr.Retried || err.Error() != "P failed: down" ||
		errors.Unwrap(err) == nil || errors.Unwrap(err).Error() != "down" {
		t.Fatalf("no fallback: %v", err)
	}

	primary = &scriptedProvider{name: "P", errs: []error{errors.New("down"), errors.New("still down")}}
	_, err = orchestratorWith(primary, nil, 1, nil).Search(context.Background(), "q", 3)
	if !errors.As(err, &searchErr) || !searchErr.Retried || err.Error() != "P failed after retry: still down" || primary.calls != 2 {
		t.Fatalf("no fallback, a retry that failed too: %v, %d calls", err, primary.calls)
	}

	primary = &scriptedProvider{name: "P", errs: []error{errors.New("down"), errors.New("still down")}}
	fallback := &scriptedProvider{name: "F", errs: []error{errors.New("query cannot be empty")}}
	_, err = orchestratorWith(primary, fallback, 1, nil).Search(context.Background(), "q", 3)
	if err == nil || err.Error() != "all search providers failed: primary (P): still down, fallback (F): query cannot be empty" {
		t.Fatalf("both fail: %v", err)
	}
	if primary.calls != 2 || fallback.calls != 1 {
		t.Fatalf("one retry for the primary, none for a non-retryable error: %d %d", primary.calls, fallback.calls)
	}

	if _, err := orchestratorWith(primary, nil, 0, nil).Search(context.Background(), "", 3); err == nil ||
		err.Error() != "query cannot be empty" {
		t.Fatalf("no query: %v", err)
	}
}

func TestOrchestratorRetriesUntilTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	primary := &scriptedProvider{name: "P"}
	if _, err := orchestratorWith(primary, nil, 0, nil).Search(ctx, "q", 3); !errors.Is(err, context.Canceled) || primary.calls != 0 {
		t.Fatalf("a cancelled context searches nothing: %v, %d calls", err, primary.calls)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	primary = &scriptedProvider{name: "P", errs: []error{errors.New("flaky"), errors.New("flaky")}}
	_, err := orchestratorWith(primary, nil, 5, nil).Search(ctx, "q", 3)
	if !errors.Is(err, context.DeadlineExceeded) || primary.calls != 1 {
		t.Fatalf("the retry delay outlives the deadline: %v, %d calls", err, primary.calls)
	}

	primary = &scriptedProvider{name: "P", errs: []error{context.Canceled}}
	if _, err := orchestratorWith(primary, nil, 5, nil).Search(context.Background(), "q", 3); primary.calls != 1 ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled search is not retried: %v, %d calls", err, primary.calls)
	}
}

func TestIsNonRetryableError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("rate limited (HTTP 429)"), true},
		{errors.New("query cannot be empty"), true},
		{context.Canceled, true},
		{errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		if got := isNonRetryableError(tt.err); got != tt.want {
			t.Errorf("isNonRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestOrchestratorIsAvailableAsksBothProviders(t *testing.T) {
	tests := []struct {
		primary, fallback, want bool
		withFallback            bool
	}{
		{true, false, true, true},
		{false, true, true, true},
		{false, false, false, true},
		{false, false, false, false},
	}
	for _, tt := range tests {
		var fallback Provider
		if tt.withFallback {
			fallback = &scriptedProvider{name: "F", available: tt.fallback}
		}
		o := orchestratorWith(&scriptedProvider{name: "P", available: tt.primary}, fallback, 0, nil)
		if got := o.IsAvailable(context.Background()); got != tt.want {
			t.Errorf("%+v: IsAvailable() = %v", tt, got)
		}
	}
}

func TestSearchErrorMessages(t *testing.T) {
	if got := (&SearchError{Provider: "P"}).Error(); got != "P failed: unknown error" {
		t.Errorf("%q", got)
	}
	if got := (&SearchError{Provider: "P", Err: errors.New("down"), Retried: true}).Error(); got != "P failed after retry: down" {
		t.Errorf("%q", got)
	}
}
