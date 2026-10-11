package agent

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
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

// thinkingSession is a session at think=high on a model with thinking, on an engine that also serves a small model
// without thinking, whose window of 16384 is below the compaction budget.
func thinkingSession(t *testing.T) (*Assistant, *taraenginetest.Server, *bytes.Buffer) {
	t.Helper()
	opts, srv, out := engineOptions(t)
	srv.Models = append(srv.Models, taraenginetest.ModelSpec{Name: "small:3b",
		Capabilities: []string{"completion", "tools"}, ContextLength: 16384})
	opts.Model, opts.Think = "glm-4.7-flash", "high"
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
		a, srv, _ := thinkingSession(t)
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

// TestSwitchModelPutsTheSettingsBackWhenTheOldModelCannotBeRead: when the engine will not describe the old model
// right after the new one failed to load, the settings the new model set are still put back, thinking support
// included.
func TestSwitchModelPutsTheSettingsBackWhenTheOldModelCannotBeRead(t *testing.T) {
	a, srv, out := thinkingSession(t)
	before := sessionSettings(a)
	srv.Failures["POST /api/load"] = &taraenginetest.Failure{Status: http.StatusBadGateway, Message: "exit code 1",
		Times: 1}
	srv.Failures["GET /api/models/glm-4.7-flash"] = &taraenginetest.Failure{Status: http.StatusInternalServerError,
		Message: "x", Times: 1}
	err := a.SwitchModel("small:3b")
	if err == nil || !strings.Contains(out.String(), "Could not load glm-4.7-flash back") {
		t.Fatalf("err %v, output %q", err, out.String())
	}
	if after := sessionSettings(a); after != before || a.SetThink(llm.ThinkHigh) != llm.ThinkHigh {
		t.Fatalf("the session was %s, now %s, thinking supported: %v", before, after, a.thinkingSupported)
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
