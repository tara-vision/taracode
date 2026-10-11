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

// TestAgainstARealEngine runs the client against the engine scripts/engine-ci.sh starts: health, version, the model
// list, a load with a window, a streamed chat with timings, an unload, a not-pulled refusal, and the key on /api and
// /v1. TARACODE_ENGINE_URL, TARACODE_ENGINE_KEY and TARACODE_ENGINE_MODEL name the engine, its key and the imported
// model.
func TestAgainstARealEngine(t *testing.T) {
	host, key, model := os.Getenv("TARACODE_ENGINE_URL"), os.Getenv("TARACODE_ENGINE_KEY"),
		os.Getenv("TARACODE_ENGINE_MODEL")
	if host == "" || key == "" || model == "" {
		t.Fatal("run through scripts/engine-ci.sh: TARACODE_ENGINE_URL, TARACODE_ENGINE_KEY or TARACODE_ENGINE_MODEL " +
			"is unset")
	}
	ctx := context.Background()
	c := New(host, key, nil)
	if h, err := c.Engine().Health(ctx); err != nil || h.Backend != "llama.cpp" {
		t.Fatalf("health %+v %v", h, err)
	}
	if v, err := c.Version(ctx); err != nil || v == "" {
		t.Fatalf("version %q %v", v, err)
	}
	infos, err := c.Models(ctx)
	if err != nil || !slices.ContainsFunc(infos, func(m llm.ModelInfo) bool { return m.Name == model }) {
		t.Fatalf("models %+v %v", infos, err)
	}
	if err := c.Load(ctx, model, 512); err != nil {
		t.Fatalf("load: %v", err)
	}
	loaded, err := c.Loaded(ctx)
	if err != nil || !slices.ContainsFunc(loaded, func(m llm.LoadedModel) bool {
		return m.Name == model && m.ContextLength == 512
	}) {
		t.Fatalf("loaded %+v %v", loaded, err)
	}
	zero := float32(0)
	req := llm.Request{Model: model, Options: llm.Options{Temperature: &zero, NumPredict: 16},
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "Once upon a time"}}}
	res, err := c.Chat(ctx, req, nil)
	if err != nil || res.Usage.CompletionTokens == 0 || res.Usage.EvalDuration <= 0 || res.Usage.PromptDuration <= 0 {
		t.Fatalf("chat %+v %v", res, err)
	}
	if err := c.Unload(ctx, model); err != nil {
		t.Fatalf("unload: %v", err)
	}
	var refused *llm.ServerError
	if _, err := c.Show(ctx, "gemma4:12b"); !errors.As(err, &refused) || refused.Status != http.StatusNotFound {
		t.Fatalf("a catalog model that is not pulled: %#v", err)
	}
	wrong := New(host, "wrong", nil)
	if _, err := wrong.Version(ctx); !errors.As(err, &refused) || refused.Status != http.StatusUnauthorized {
		t.Fatalf("/api with a wrong key: %#v", err)
	}
	if _, err := wrong.Chat(ctx, req, nil); !errors.As(err, &refused) || refused.Status != http.StatusUnauthorized {
		t.Fatalf("/v1 with a wrong key: %#v", err)
	}
}
