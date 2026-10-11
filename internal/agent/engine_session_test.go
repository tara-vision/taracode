package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
	"github.com/tara-vision/taracode/internal/ui"
)

// engineOptions is a New configuration on the fake engine with two usable models, one that is not pulled and one
// without tools, with HOME isolated and the output collected in the returned buffer.
func engineOptions(t *testing.T) (Options, *taraenginetest.Server, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 131072},
		{Name: "glm-4.7-flash", Capabilities: []string{"completion", "tools", "thinking"}, ContextLength: 202752},
		{Name: "qwen3.6:35b", Status: "available"},
		{Name: "embed:1b", Capabilities: []string{"embedding"}},
	}
	var out bytes.Buffer
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Spinner, opts.Output = srv.URL, "gemma4:12b", t.TempDir(), false, &out
	opts.Ephemeral = true
	return opts, srv, &out
}

func TestNewLoadsTheModelAtTheResolvedWindow(t *testing.T) {
	opts, srv, _ := engineOptions(t)
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	want := taraenginetest.LoadRequest{Model: "gemma4:12b", Context: 32768}
	if len(srv.Loads) != 1 || srv.Loads[0] != want {
		t.Fatalf("loads %+v, want one at the auto window", srv.Loads)
	}
}

func TestNewStopsWhereTheEngineSaysNo(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*Options, *taraenginetest.Server)
		status int
	}{
		{"a wrong key", func(o *Options, s *taraenginetest.Server) { s.Key, o.APIKey = "right", "wrong" },
			http.StatusUnauthorized},
		{"a failed load", func(_ *Options, s *taraenginetest.Server) {
			s.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway,
				Message: "gemma4:12b exited with exit code 1", Hint: "taraengine doctor shows the last lines of the log"}
		}, http.StatusBadGateway},
	}
	for _, c := range cases {
		opts, srv, _ := engineOptions(t)
		c.setup(&opts, srv)
		_, err := New(opts)
		var refused *llm.ServerError
		if !errors.As(err, &refused) || refused.Status != c.status {
			t.Errorf("%s: %#v", c.name, err)
		}
	}
}

// TestNewFallsBackFromAModelThatIsNotPulled: a configured model the engine lists as available but not pulled is
// not among the pulled models, so New warns and takes the first pulled one, as it does for any missing model.
func TestNewFallsBackFromAModelThatIsNotPulled(t *testing.T) {
	opts, srv, out := engineOptions(t)
	opts.Model = "qwen3.6:35b"
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Configured model 'qwen3.6:35b' not available. Using: gemma4:12b") ||
		srv.Loads[0].Model != "gemma4:12b" {
		t.Fatalf("output %q, loads %+v", out.String(), srv.Loads)
	}
}

func TestNewStopsOnAModelWithoutTools(t *testing.T) {
	opts, srv, _ := engineOptions(t)
	opts.Model = "embed:1b"
	if _, err := New(opts); !errors.Is(err, ErrModelWithoutTools) || len(srv.Loads) != 0 {
		t.Fatalf("a model without tools: %v, loads %+v", err, srv.Loads)
	}
}

// TestNewAndSwitchModelStartAnImportTheEngineHasNotLoaded: the engine lists no capabilities for an import until it
// has loaded it, so the session takes them as unknown: no tools gate, no warning, thinking left as configured and the
// window from the native context; the load then teaches the engine the rest.
func TestNewAndSwitchModelStartAnImportTheEngineHasNotLoaded(t *testing.T) {
	opts, srv, out := engineOptions(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "owner/repo:q4", CapabilitiesInferred: true,
		ContextLength: 16384})
	opts.Model = "owner/repo:q4"
	a, err := New(opts)
	if err != nil {
		t.Fatalf("New on an import: %v", err)
	}
	want := taraenginetest.LoadRequest{Model: "owner/repo:q4", Context: 16384}
	if len(srv.Loads) != 1 || srv.Loads[0] != want || strings.Contains(out.String(), ui.IconWarning) ||
		a.SetThink(llm.ThinkHigh) != llm.ThinkHigh {
		t.Fatalf("loads %+v, output %q", srv.Loads, out.String())
	}
	if err := a.SwitchModel("gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchModel("owner/repo:q4"); err != nil || a.GetCurrentModel() != "owner/repo:q4" ||
		srv.Loads[len(srv.Loads)-1] != want {
		t.Fatalf("back to the import: %v, model %s, loads %+v", err, a.GetCurrentModel(), srv.Loads)
	}
}

func TestSwitchModelLoadsTheNewModel(t *testing.T) {
	opts, srv, out := engineOptions(t)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchModel("glm-4.7-flash"); err != nil {
		t.Fatal(err)
	}
	want := taraenginetest.LoadRequest{Model: "glm-4.7-flash", Context: 32768}
	if a.GetCurrentModel() != "glm-4.7-flash" || len(srv.Unloaded) != 1 || srv.Unloaded[0] != "gemma4:12b" ||
		srv.Loads[len(srv.Loads)-1] != want {
		t.Fatalf("model %s, unloaded %v, loads %+v", a.GetCurrentModel(), srv.Unloaded, srv.Loads)
	}
	srv.Failures["POST /api/unload"] = &taraenginetest.Failure{Status: http.StatusInternalServerError, Message: "x"}
	if err := a.SwitchModel("gemma4:12b"); err != nil || !strings.Contains(out.String(), "Could not unload glm-4.7-flash") {
		t.Fatalf("an unload that fails does not stop the switch: %v, %q", err, out.String())
	}
}

func TestSwitchModelKeepsTheOldModelWhenTheNewOneIsRefused(t *testing.T) {
	for _, refused := range []string{"qwen3.6:35b", "embed:1b"} {
		opts, srv, _ := engineOptions(t)
		a, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.SwitchModel(refused); err == nil || a.GetCurrentModel() != "gemma4:12b" || len(srv.Unloaded) != 0 {
			t.Errorf("%s: err %v, model %s, unloaded %v", refused, err, a.GetCurrentModel(), srv.Unloaded)
		}
	}
}

func TestSwitchModelBringsTheOldModelBackWhenTheLoadFails(t *testing.T) {
	opts, srv, out := engineOptions(t)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway, Message: "exit code 1",
		Times: 1}
	if err := a.SwitchModel("glm-4.7-flash"); err == nil || a.GetCurrentModel() != "gemma4:12b" {
		t.Fatalf("err %v, model %s", err, a.GetCurrentModel())
	}
	if last := srv.Loads[len(srv.Loads)-1]; last.Model != "gemma4:12b" || strings.Contains(out.String(), "Could not load") {
		t.Fatalf("the old model comes back: loads %+v, output %q", srv.Loads, out.String())
	}
	srv.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway, Message: "exit code 1"}
	if err := a.SwitchModel("glm-4.7-flash"); err == nil || !strings.Contains(out.String(),
		"Could not load gemma4:12b back") {
		t.Fatalf("err %v, output %q", err, out.String())
	}
}

// thinkingSession is a session at think=high on model, on an engine that serves glm-4.7-flash, with thinking, and
// small:3b, without it, whose window of 16384 is below the compaction budget.
func thinkingSession(t *testing.T, model string) (*Assistant, *taraenginetest.Server, *bytes.Buffer) {
	t.Helper()
	opts, srv, out := engineOptions(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "small:3b",
		Capabilities: []string{"completion", "tools"}, ContextLength: 16384})
	opts.Model, opts.Think = model, "high"
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return a, srv, out
}

// sessionSettings describes what a model switch may change: the model, the think level, the compaction budget and
// the window.
func sessionSettings(a *Assistant) string {
	info := a.GetContextInfo()
	return fmt.Sprintf("model %s, think %q, budget %d, window %d", a.GetCurrentModel(), a.Think(), info.MaxTokens,
		info.ContextWindow)
}

// scriptedShow is an llm.Client that records the models it is asked to describe and fails the first Show of one
// model with err; everything else, loads included, goes to the client underneath.
type scriptedShow struct {
	llm.Client
	model string
	err   error
	shown []string
}

var _ llm.Loader = (*scriptedShow)(nil)

func (c *scriptedShow) Show(ctx context.Context, model string) (*llm.ModelDetails, error) {
	c.shown = append(c.shown, model)
	if err := c.err; err != nil && model == c.model {
		c.err = nil
		return nil, err
	}
	return c.Client.Show(ctx, model)
}

// Load passes the load on, as the engine client underneath does.
func (c *scriptedShow) Load(ctx context.Context, model string, window int) error {
	return c.Client.(llm.Loader).Load(ctx, model, window)
}

// TestSwitchModelThatStopsLeavesTheSessionAsItWas: reading a model may lower the think level and the compaction
// budget, so a switch that stops, in its load or before it, leaves the model, the think level, the compaction budget
// and the window as they were.
func TestSwitchModelThatStopsLeavesTheSessionAsItWas(t *testing.T) {
	cases := []struct {
		name, model string
		loadFails   bool
	}{
		{"a load that fails", "small:3b", true},
		{"a model that is not pulled", "qwen3.6:35b", false},
		{"a model without tools", "embed:1b", false},
	}
	for _, c := range cases {
		a, srv, _ := thinkingSession(t, "glm-4.7-flash")
		before := sessionSettings(a)
		if c.loadFails {
			srv.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway,
				Message: "exit code 1", Times: 1}
		}
		if err := a.SwitchModel(c.model); err == nil || sessionSettings(a) != before {
			t.Errorf("%s: err %v, the session was %s, now %s", c.name, err, before, sessionSettings(a))
		}
	}
}

// TestSwitchModelPutsTheSettingsBackWhenTheOldModelCannotBeRead: the old model is not read again once the new one
// failed to load. The session was on small:3b, a window of 16384 and no thinking; whatever a second Show of it
// would answer, the session keeps those settings and the old model is loaded back at its own window.
func TestSwitchModelPutsTheSettingsBackWhenTheOldModelCannotBeRead(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"an error the server explains", &llm.ServerError{Status: http.StatusInternalServerError, Message: "x",
			Model: "small:3b"}},
		{"an error nothing explains", errors.New("context deadline exceeded")},
	}
	for _, c := range cases {
		a, srv, out := thinkingSession(t, "small:3b")
		show := &scriptedShow{Client: a.llm, model: "small:3b", err: c.err}
		a.llm = show
		before := sessionSettings(a)
		srv.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway,
			Message: "exit code 1", Times: 1}
		err := a.SwitchModel("glm-4.7-flash")
		var refused *llm.ServerError
		if !errors.As(err, &refused) || refused.Status != http.StatusBadGateway {
			t.Errorf("%s: the switch returns the load's error: %v", c.name, err)
		}
		want := taraenginetest.LoadRequest{Model: "small:3b", Context: 16384}
		if len(srv.Loads) != 2 || srv.Loads[1] != want || strings.Contains(out.String(), "Could not load") {
			t.Errorf("%s: the old model is loaded back at its window: loads %+v, output %q", c.name, srv.Loads,
				out.String())
		}
		if after := sessionSettings(a); after != before || a.SetThink(llm.ThinkHigh) != llm.ThinkAuto {
			t.Errorf("%s: the session was %s, now %s, thinking supported: %v", c.name, before, after,
				a.thinkingSupported)
		}
		if slices.Contains(show.shown, "small:3b") {
			t.Errorf("%s: the old model is not read again, models read: %v", c.name, show.shown)
		}
	}
}

func TestServerContextAdvicePointsAtContextWindow(t *testing.T) {
	for _, c := range []struct{ serverCtx, max int }{{4096, 32768}, {16384, 32768}} {
		msg, warn := ServerContextAdvice(c.serverCtx, 300, 5800, c.max, 0.75)
		if !warn || strings.Contains(msg, "Ollama") || strings.Contains(msg, "OLLAMA") || !strings.Contains(msg,
			"context.window") {
			t.Errorf("server window %d: %q", c.serverCtx, msg)
		}
	}
}
