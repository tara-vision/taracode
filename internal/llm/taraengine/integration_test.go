//go:build engine

package taraengine

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// TestAgainstARealEngine runs the client against the engine scripts/engine-ci.sh starts, one step after another:
// health without a key (what detection asks), the version, doctor (the machine's tier and the recommendations), the
// model list, an import's capabilities before its first load, a load with a window, a streamed chat with timings, an
// unload, a not-pulled refusal, and the key on /api and /v1. TARACODE_ENGINE_URL, TARACODE_ENGINE_KEY and
// TARACODE_ENGINE_MODEL name the engine, its key and the imported model.
func TestAgainstARealEngine(t *testing.T) {
	e := realEngine{host: os.Getenv("TARACODE_ENGINE_URL"), key: os.Getenv("TARACODE_ENGINE_KEY"),
		model: os.Getenv("TARACODE_ENGINE_MODEL")}
	if e.host == "" || e.key == "" || e.model == "" {
		t.Fatal("run through scripts/engine-ci.sh: TARACODE_ENGINE_URL, TARACODE_ENGINE_KEY or TARACODE_ENGINE_MODEL " +
			"is unset")
	}
	e.c = New(e.host, e.key, nil)
	for _, step := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"health without a key", e.healthWithoutAKey},
		{"version", e.version},
		{"doctor", e.doctor},
		{"models", e.models},
		{"import capabilities unknown before the load", e.importCapabilitiesUnknown},
		{"load at a window", e.loadAtAWindow},
		{"chat with timings", e.chatWithTimings},
		{"unload", e.unload},
		{"not pulled", e.notPulled},
		{"a wrong key", e.wrongKey},
	} {
		if !t.Run(step.name, step.run) {
			return // each step needs the ones before it
		}
	}
}

// realEngine is the engine the steps run against: its address, its key, the imported model and a keyed client.
type realEngine struct {
	host, key, model string
	c                *Client
}

// healthWithoutAKey: detection asks /api/health without a key, so a keyed engine must answer it.
func (e realEngine) healthWithoutAKey(t *testing.T) {
	if h, err := New(e.host, "", nil).Engine().Health(context.Background()); err != nil || h.Backend != "llama.cpp" {
		t.Fatalf("health without a key %+v %v", h, err)
	}
}

func (e realEngine) version(t *testing.T) {
	if v, err := e.c.Version(context.Background()); err != nil || v == "" {
		t.Fatalf("version %q %v", v, err)
	}
}

// doctor: taracode doctor and the first-run advice read the machine's tier and the recommendation per tier.
func (e realEngine) doctor(t *testing.T) {
	d, err := e.c.Engine().Doctor(context.Background())
	if err != nil || d.Machine.Tier == "" || len(d.Recommendation) == 0 {
		t.Fatalf("doctor %+v %v", d, err)
	}
}

func (e realEngine) models(t *testing.T) {
	infos, err := e.c.Models(context.Background())
	if err != nil || !slices.ContainsFunc(infos, func(m llm.ModelInfo) bool { return m.Name == e.model }) {
		t.Fatalf("models %+v %v", infos, err)
	}
}

// importCapabilitiesUnknown: the engine infers an import's capabilities at its load, so before the first load Show
// reports them unknown and the session does not refuse the model as one without tools.
func (e realEngine) importCapabilitiesUnknown(t *testing.T) {
	if d, err := e.c.Show(context.Background(), e.model); err != nil || !d.CapabilitiesUnknown {
		t.Fatalf("show %+v %v", d, err)
	}
}

func (e realEngine) loadAtAWindow(t *testing.T) {
	ctx := context.Background()
	if err := e.c.Load(ctx, e.model, 512); err != nil {
		t.Fatalf("load: %v", err)
	}
	loaded, err := e.c.Loaded(ctx)
	if err != nil || !slices.ContainsFunc(loaded, func(m llm.LoadedModel) bool {
		return m.Name == e.model && m.ContextLength == 512
	}) {
		t.Fatalf("loaded %+v %v", loaded, err)
	}
}

func (e realEngine) chatWithTimings(t *testing.T) {
	res, err := e.c.Chat(context.Background(), e.request(), nil)
	if err != nil || res.Usage.CompletionTokens == 0 || res.Usage.EvalDuration <= 0 || res.Usage.PromptDuration <= 0 {
		t.Fatalf("chat %+v %v", res, err)
	}
}

func (e realEngine) unload(t *testing.T) {
	if err := e.c.Unload(context.Background(), e.model); err != nil {
		t.Fatalf("unload: %v", err)
	}
}

func (e realEngine) notPulled(t *testing.T) {
	var refused *llm.ServerError
	if _, err := e.c.Show(context.Background(), "gemma4:12b"); !errors.As(err, &refused) ||
		refused.Status != http.StatusNotFound {
		t.Fatalf("a catalog model that is not pulled: %#v", err)
	}
}

func (e realEngine) wrongKey(t *testing.T) {
	wrong := New(e.host, "wrong", nil)
	var refused *llm.ServerError
	if _, err := wrong.Version(context.Background()); !errors.As(err, &refused) ||
		refused.Status != http.StatusUnauthorized {
		t.Fatalf("/api with a wrong key: %#v", err)
	}
	if _, err := wrong.Chat(context.Background(), e.request(), nil); !errors.As(err, &refused) ||
		refused.Status != http.StatusUnauthorized {
		t.Fatalf("/v1 with a wrong key: %#v", err)
	}
}

// request is a short completion at temperature 0.
func (e realEngine) request() llm.Request {
	zero := float32(0)
	return llm.Request{Model: e.model, Options: llm.Options{Temperature: &zero, NumPredict: 16},
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "Once upon a time"}}}
}
