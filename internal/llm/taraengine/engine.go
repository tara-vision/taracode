// Package taraengine is taracode's native client for taraengine: chat over the engine's /v1 with streamed thinking,
// tool calls and timings, and the control API under /api through the engine's own pkg/client.
package taraengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tara-vision/taraengine/pkg/api"
	"github.com/tara-vision/taraengine/pkg/client"

	"github.com/tara-vision/taracode/internal/llm"
)

const (
	// StartHint tells the user how to start an engine nothing answers at.
	StartHint = "start taraengine with `taraengine serve`"
	// InstallHint tells the user how to install the engine.
	InstallHint = "brew install --cask tara-vision/taracode/taraengine, or see https://github.com/tara-vision/taraengine"
	// MinVersion is the oldest engine this taracode supports; doctor warns below it and never refuses.
	MinVersion = "0.2.0"
	// retryDelay is the wait before the one retry after a 503: the engine is restarting its server, and its /v1
	// proxy asks for two seconds (Retry-After: 2).
	retryDelay = 2 * time.Second
	// loadBackstop bounds a load above the engine's default load_timeout of 600 s, so the engine's own 504, with its
	// hint, normally comes first.
	loadBackstop = 15 * time.Minute
)

// How long AwaitReady waits for an engine that is starting, how often it asks, and how long it bears health calls
// that nothing answers before it gives up: the engine's own commands use the same three bounds. They are variables
// only so that a test can wait less.
var (
	startWait    = 10 * time.Minute
	pollEvery    = time.Second
	refusedGrace = 20 * time.Second
)

// Engine is the engine's control API with taracode's error handling: a refusal becomes *llm.ServerError with the
// engine's hint, a dial that reached nothing becomes *llm.UnreachableError, and a 503 is retried once.
type Engine struct {
	api   *client.Client
	host  string
	sleep func(ctx context.Context, d time.Duration) error // the retry wait; tests replace it
}

func newEngine(host, key string, httpClient *http.Client) *Engine {
	c := client.New(host, key)
	c.HTTP = httpClient
	return &Engine{api: c, host: strings.TrimRight(host, "/"), sleep: sleepCtx}
}

// sleepCtx waits for d, or less when ctx ends first, and then returns ctx's error.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// AwaitReady returns once the engine is no longer starting: at once for an engine that answers ok or degraded, and
// for one that is starting after polling its health every pollEvery, with each new detail of the start handed to
// progress once. It gives up after startWait; after refusedGrace in which a starting engine answered no health call
// at all; at once, with *llm.UnreachableError, when nothing answers the first call; with the typed error when a
// health call is refused; and with the context's error when ctx ends. The detail reaches the terminal, so its control
// characters are dropped.
func (e *Engine) AwaitReady(ctx context.Context, progress func(step string)) error {
	deadline := time.Now().Add(startWait)
	answered := time.Now()
	starting := false
	last := ""
	for {
		h, err := e.Health(ctx)
		var unreachable *llm.UnreachableError
		switch {
		case err == nil && h.Status != api.HealthStarting:
			return nil
		case err == nil:
			answered, starting = time.Now(), true
			if h.Detail != last {
				last = h.Detail
				progress(llm.Printable(h.Detail))
			}
		case !errors.As(err, &unreachable) || !starting:
			return err
		case time.Since(answered) > refusedGrace:
			return fmt.Errorf("taraengine stopped answering at %s; `taraengine service status` shows why",
				llm.RedactedHost(e.host))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("taraengine did not get ready within %s at %s; `taraengine service status` shows why",
				startWait, llm.RedactedHost(e.host))
		}
		if err := e.sleep(ctx, pollEvery); err != nil {
			return err
		}
	}
}

// Health is GET /api/health, the one route the engine answers without a key.
func (e *Engine) Health(ctx context.Context) (api.Health, error) {
	return retry(ctx, e, "", func() (api.Health, error) { return e.api.Health(ctx) })
}

// Version is GET /api/version.
func (e *Engine) Version(ctx context.Context) (api.Version, error) {
	return retry(ctx, e, "", func() (api.Version, error) { return e.api.Version(ctx) })
}

// Models is GET /api/models: the whole catalog, pulled or not.
func (e *Engine) Models(ctx context.Context) ([]api.Model, error) {
	return retry(ctx, e, "", func() ([]api.Model, error) { return e.api.Models(ctx) })
}

// Model is GET /api/models/{name}.
func (e *Engine) Model(ctx context.Context, name string) (api.ModelDetail, error) {
	return retry(ctx, e, name, func() (api.ModelDetail, error) { return e.api.Model(ctx, name) })
}

// Loaded is GET /api/loaded.
func (e *Engine) Loaded(ctx context.Context) ([]api.Loaded, error) {
	return retry(ctx, e, "", func() ([]api.Loaded, error) { return e.api.Loaded(ctx) })
}

// Load is POST /api/load at window, bounded by loadBackstop.
func (e *Engine) Load(ctx context.Context, name string, window int) (api.Model, error) {
	ctx, cancel := context.WithTimeout(ctx, loadBackstop)
	defer cancel()
	return retry(ctx, e, name, func() (api.Model, error) { return e.api.Load(ctx, name, window, false) })
}

// Unload is POST /api/unload.
func (e *Engine) Unload(ctx context.Context, name string) error {
	_, err := retry(ctx, e, name, func() (struct{}, error) { return struct{}{}, e.api.Unload(ctx, name) })
	return err
}

// Doctor is GET /api/doctor: the machine, its tier, the runtime and the recommendation per tier.
func (e *Engine) Doctor(ctx context.Context) (api.Doctor, error) {
	return retry(ctx, e, "", func() (api.Doctor, error) { return e.api.Doctor(ctx) })
}

// retry runs f, runs it once more after retryDelay when the engine answered 503, and converts the error. A context
// that ends during the wait ends the call with its error.
func retry[T any](ctx context.Context, e *Engine, model string, f func() (T, error)) (T, error) {
	v, err := f()
	var refused *client.Error
	if errors.As(err, &refused) && refused.Status == http.StatusServiceUnavailable {
		if werr := e.sleep(ctx, retryDelay); werr != nil {
			return v, werr
		}
		v, err = f()
	}
	return v, e.convert(err, model)
}

// convert turns a pkg/client error into taracode's: a refusal into *llm.ServerError, with the control characters of
// its text dropped and its message cut to the length of a /v1 refusal's, and a dial that reached nothing into
// *llm.UnreachableError. Anything else passes through, and nil stays nil.
func (e *Engine) convert(err error, model string) error {
	var refused *client.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &refused):
		return &llm.ServerError{Status: refused.Status, Message: cutRefusal(llm.Printable(refused.Message)),
			Hint: llm.Printable(refused.Hint), Model: model}
	case client.IsConnectionError(err):
		return &llm.UnreachableError{Host: e.host, Hint: StartHint, Err: err}
	}
	return err
}
