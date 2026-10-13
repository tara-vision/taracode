// Package taraengine is taracode's native client for taraengine: chat over the engine's /v1 with streamed thinking,
// tool calls and timings, and the control API under /api through the engine's own pkg/client.
package taraengine

import (
	"context"
	"errors"
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
