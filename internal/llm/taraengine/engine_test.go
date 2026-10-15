package taraengine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tara-vision/taraengine/pkg/api"

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

// TestEngineKeepsTheEnginesTextOffTheTerminalControls: the message and hint of a refusal on the control API reach
// the terminal, so the control characters in them are dropped.
func TestEngineKeepsTheEnginesTextOffTheTerminalControls(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Failures["GET /api/version"] = &taraenginetest.Failure{Status: http.StatusInternalServerError,
		Message: "broken\x1b[2J catalog", Hint: "\x1b]0;title\x07taraengine doctor"}
	_, err := New(srv.URL, "", nil).Engine().Version(context.Background())
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Message != "broken[2J catalog" || refused.Hint != "]0;titletaraengine doctor" {
		t.Fatalf("a refusal with escape sequences: %#v", err)
	}
}

// TestEngineCutsALongRefusalMessage: pkg/client passes on up to a mebibyte of a refusal as its message, a whole proxy
// page when the body is not the engine's JSON. The message reaches the terminal, so it is cut to the length of a /v1
// refusal, at a rune boundary; a message that fits is unchanged.
func TestEngineCutsALongRefusalMessage(t *testing.T) {
	long := "x" + strings.Repeat("é", 499) + "y" // 1000 bytes; the cut at 300 falls inside the 150th two-byte rune
	atTheBound := strings.Repeat("a", maxPlainRefusal)
	for name, c := range map[string]struct{ message, want string }{
		"a long message":         {long, long[:maxPlainRefusal-1]},
		"a message at the bound": {atTheBound, atTheBound},
		"a short message":        {"the catalog is broken", "the catalog is broken"},
	} {
		srv := taraenginetest.New(t)
		srv.Failures["GET /api/version"] = &taraenginetest.Failure{Status: http.StatusInternalServerError,
			Message: c.message, Hint: "taraengine doctor shows the log"}
		_, err := New(srv.URL, "", nil).Engine().Version(context.Background())
		var refused *llm.ServerError
		if !errors.As(err, &refused) || refused.Message != c.want || !utf8.ValidString(refused.Message) ||
			refused.Hint != "taraengine doctor shows the log" || refused.Status != http.StatusInternalServerError {
			t.Errorf("%s: %#v, want a message of %d bytes", name, err, len(c.want))
		}
	}
	page := "<html>" + strings.Repeat("proxy error page ", 60) + "</html>"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(proxy.Close)
	_, err := New(proxy.URL, "", nil).Engine().Version(context.Background())
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Message != page[:maxPlainRefusal] || refused.Status != http.StatusBadGateway {
		t.Errorf("a proxy's page: %#v", err)
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

// healthCalls counts the fake's /api/health requests.
func healthCalls(srv *taraenginetest.Server) int {
	n := 0
	for _, r := range srv.Requests {
		if r.Path == "/api/health" {
			n++
		}
	}
	return n
}

// shortWaits lowers the wait bounds for a test and restores them after it.
func shortWaits(t *testing.T, wait, grace time.Duration) {
	t.Helper()
	oldWait, oldGrace := startWait, refusedGrace
	startWait, refusedGrace = wait, grace
	t.Cleanup(func() { startWait, refusedGrace = oldWait, oldGrace })
}

// TestAwaitReadyFollowsAStartingEngineToTheEnd: each step of the start reaches progress once, a step the engine
// repeats is not repeated, the polls wait pollEvery between them, and the wait ends at the first answer that is not
// starting.
func TestAwaitReadyFollowsAStartingEngineToTheEnd(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Starting = []string{"installing runtime b11514 (10%)", "installing runtime b11514 (10%)",
		"installing runtime b11514 (65%)", "starting llama-server\x1b[2J"}
	eng := New(srv.URL, "", nil).Engine()
	waits := noWait(eng)
	var steps []string
	if err := eng.AwaitReady(context.Background(), func(step string) { steps = append(steps, step) }); err != nil {
		t.Fatal(err)
	}
	want := []string{"installing runtime b11514 (10%)", "installing runtime b11514 (65%)", "starting llama-server[2J"}
	if strings.Join(steps, "|") != strings.Join(want, "|") || *waits != 4 || healthCalls(srv) != 5 {
		t.Fatalf("steps %q, waits %d, health calls %d", steps, *waits, healthCalls(srv))
	}
}

// TestAwaitReadyReturnsAtOnceForAnEngineThatAnswers: an engine that is ok or degraded is not waited for, and progress
// hears nothing.
func TestAwaitReadyReturnsAtOnceForAnEngineThatAnswers(t *testing.T) {
	for _, status := range []string{"ok", "degraded"} {
		srv := taraenginetest.New(t)
		srv.Health = api.Health{Status: status, Backend: "llama.cpp", Detail: "x"}
		eng := New(srv.URL, "", nil).Engine()
		waits := noWait(eng)
		called := false
		if err := eng.AwaitReady(context.Background(), func(string) { called = true }); err != nil || called ||
			*waits != 0 || healthCalls(srv) != 1 {
			t.Fatalf("%s: %v, progress called %v, waits %d, health calls %d", status, err, called, *waits,
				healthCalls(srv))
		}
	}
}

// TestAwaitReadyGivesUpAtTheDeadline: an engine still starting when startWait is over ends the wait with the engine's
// own wording, and the host's password stays off the message.
func TestAwaitReadyGivesUpAtTheDeadline(t *testing.T) {
	shortWaits(t, 0, time.Minute)
	srv := taraenginetest.New(t)
	srv.Starting = []string{"installing runtime b11514 (10%)", "installing runtime b11514 (20%)"}
	host := strings.Replace(srv.URL, "http://", "http://user:secret@", 1)
	eng := New(host, "", nil).Engine()
	noWait(eng)
	err := eng.AwaitReady(context.Background(), func(string) {})
	want := "taraengine did not get ready within 0s at " + strings.Replace(srv.URL, "http://", "http://user:xxxxx@", 1) +
		"; `taraengine service status` shows why"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}

// TestAwaitReadyReportsAnEngineThatNeverAnsweredAtOnce: nothing answering the first health call is the unreachable
// error at once, not a wait: the session and the eval run then stop with it, as they do for any other first call.
func TestAwaitReadyReportsAnEngineThatNeverAnsweredAtOnce(t *testing.T) {
	eng := New("http://127.0.0.1:1", "", nil).Engine()
	waits := noWait(eng)
	err := eng.AwaitReady(context.Background(), func(string) {})
	var unreachable *llm.UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Host != "http://127.0.0.1:1" || *waits != 0 {
		t.Fatalf("err %#v, waits %d", err, *waits)
	}
}

// TestAwaitReadyGivesUpWhenAStartingEngineStopsAnswering: a starting engine whose health calls reach nothing for
// refusedGrace ends the wait with the engine's own wording.
func TestAwaitReadyGivesUpWhenAStartingEngineStopsAnswering(t *testing.T) {
	shortWaits(t, time.Minute, 0)
	starting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"starting","backend":"llama.cpp","detail":"installing runtime b11514 (10%)"}`))
	}))
	t.Cleanup(starting.Close)
	eng := New(starting.URL, "", nil).Engine()
	eng.sleep = func(context.Context, time.Duration) error { starting.Close(); return nil } // gone after the first answer
	err := eng.AwaitReady(context.Background(), func(string) {})
	want := "taraengine stopped answering at " + starting.URL + "; `taraengine service status` shows why"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}

// TestAwaitReadyPassesARefusalOn: a health call the server refuses is that refusal, not a wait.
func TestAwaitReadyPassesARefusalOn(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"host not allowed","hint":"use the listen address"}}`))
	}))
	t.Cleanup(refusing.Close)
	eng := New(refusing.URL, "", nil).Engine()
	noWait(eng)
	err := eng.AwaitReady(context.Background(), func(string) {})
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusForbidden || refused.Hint != "use the listen address" {
		t.Fatalf("err %#v", err)
	}
}

// TestAwaitReadyEndsWithItsContext: a context that ends during the wait ends it with the context's error.
func TestAwaitReadyEndsWithItsContext(t *testing.T) {
	srv := taraenginetest.New(t)
	srv.Starting = []string{"installing runtime b11514 (10%)", "installing runtime b11514 (20%)"}
	eng := New(srv.URL, "", nil).Engine()
	ctx, cancel := context.WithCancel(context.Background())
	eng.sleep = func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	if err := eng.AwaitReady(ctx, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

// TestTheHintsNameTheServiceAndTheInstallers: the hints name what engine v0.3.0 installs, the service, the cask and
// the installer script, so a user who meets them can act on them as written.
func TestTheHintsNameTheServiceAndTheInstallers(t *testing.T) {
	if StartHint != "start taraengine with `taraengine service start`, or `taraengine serve` in a terminal" {
		t.Errorf("StartHint = %q", StartHint)
	}
	if InstallHint != "brew install --cask tara-vision/taracode/taraengine on a Mac, "+
		"or curl -fsSL https://engine.tara.vision/install.sh | bash on Linux" {
		t.Errorf("InstallHint = %q", InstallHint)
	}
}
