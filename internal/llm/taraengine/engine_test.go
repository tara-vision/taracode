package taraengine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// noWait replaces the retry wait and counts the waits.
func noWait(e *Engine) *int {
	waits := 0
	e.sleep = func(context.Context, time.Duration) error { waits++; return nil }
	return &waits
}

func TestEngineReadsTheControlAPI(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{{Name: "gemma4:12b", ContextLength: 131072}}
	srv.Loaded = []taraenginetest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768, Size: 9_000_000_000}}
	eng := New(srv.URL, "", nil).Engine()
	ctx := context.Background()
	h, err := eng.Health(ctx)
	if err != nil || h.Backend != "llama.cpp" {
		t.Fatalf("health %+v %v", h, err)
	}
	v, err := eng.Version(ctx)
	if err != nil || v.Version != taraenginetest.DefaultVersion || v.Runtime.Build != taraenginetest.DefaultRuntime {
		t.Fatalf("version %+v %v", v, err)
	}
	if list, err := eng.Models(ctx); err != nil || len(list) != 1 {
		t.Fatalf("models %+v %v", list, err)
	}
	if d, err := eng.Model(ctx, "gemma4:12b"); err != nil || d.NativeContext != 131072 {
		t.Fatalf("model %+v %v", d, err)
	}
	if l, err := eng.Loaded(ctx); err != nil || l[0].Memory.Bytes != 9_000_000_000 {
		t.Fatalf("loaded %+v %v", l, err)
	}
	if d, err := eng.Doctor(ctx); err != nil || d.Machine.Tier != "32" {
		t.Fatalf("doctor %+v %v", d, err)
	}
}

func TestEngineLoadsAndUnloads(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{{Name: "gemma4:12b", ContextLength: 131072}}
	eng := New(srv.URL, "", nil).Engine()
	if m, err := eng.Load(context.Background(), "gemma4:12b", 32768); err != nil || m.Context != 32768 {
		t.Fatalf("load %+v %v", m, err)
	}
	if err := eng.Unload(context.Background(), "gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	want := taraenginetest.LoadRequest{Model: "gemma4:12b", Context: 32768}
	if len(srv.Loads) != 1 || srv.Loads[0] != want || len(srv.Unloaded) != 1 {
		t.Fatalf("loads %+v, unloaded %v", srv.Loads, srv.Unloaded)
	}
}

func TestEngineTurnsRefusalsIntoServerErrors(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Key = "right"
	eng := New(srv.URL, "wrong", nil).Engine()
	_, err := eng.Version(context.Background())
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusUnauthorized ||
		refused.Hint != "send Authorization: Bearer <api_key>" {
		t.Fatalf("a wrong key: %#v", err)
	}
	srv.Key = ""
	_, err = eng.Model(context.Background(), "nope:1b")
	if !errors.As(err, &refused) || refused.Status != http.StatusNotFound || refused.Model != "nope:1b" {
		t.Fatalf("an unknown model: %#v", err)
	}
}

func TestEngineReportsANoAnswerAsUnreachable(t *testing.T) {
	_, err := New("http://127.0.0.1:1/", "", nil).Engine().Health(context.Background())
	var unreachable *llm.UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Host != "http://127.0.0.1:1" || unreachable.Hint != StartHint {
		t.Fatalf("a closed port: %#v", err)
	}
}

func TestEnginePassesOtherErrorsThrough(t *testing.T) {
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(garbage.Close)
	_, err := New(garbage.URL, "", nil).Engine().Version(context.Background())
	var refused *llm.ServerError
	var unreachable *llm.UnreachableError
	if err == nil || errors.As(err, &refused) || errors.As(err, &unreachable) {
		t.Fatalf("an answer that does not decode: %#v", err)
	}
}

func TestEngineRetriesA503Once(t *testing.T) {
	srv := taraenginetest.New(t)
	busy := func(times int) {
		srv.Failures["GET /api/doctor"] = &taraenginetest.Failure{Status: http.StatusServiceUnavailable,
			Message: "the engine backend is not running", Hint: "wait a moment and retry", Times: times}
	}
	eng := New(srv.URL, "", nil).Engine()
	waits := noWait(eng)
	busy(1)
	if _, err := eng.Doctor(context.Background()); err != nil || *waits != 1 {
		t.Fatalf("one 503 then an answer: %v after %d waits", err, *waits)
	}
	busy(2)
	_, err := eng.Doctor(context.Background())
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusServiceUnavailable || *waits != 2 {
		t.Fatalf("two 503s: %#v after %d waits", err, *waits)
	}
}

func TestEngineRetryStopsWhenTheContextEnds(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Failures["GET /api/loaded"] = &taraenginetest.Failure{Status: http.StatusServiceUnavailable}
	ctx, cancel := context.WithCancel(context.Background())
	eng := New(srv.URL, "", nil).Engine()
	eng.sleep = func(ctx context.Context, d time.Duration) error { cancel(); return sleepCtx(ctx, d) }
	if _, err := eng.Loaded(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled wait: %v", err)
	}
	if len(srv.Requests) != 1 {
		t.Fatalf("no retry after a cancelled wait: %d requests", len(srv.Requests))
	}
}

func TestSleepCtxWaitsOrStops(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context: %v", err)
	}
}
