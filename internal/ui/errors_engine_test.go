package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/provider"
)

func TestEnhanceErrorPutsTheEngineHintFirst(t *testing.T) {
	cases := []struct {
		err   error
		title string
		lines []string // in the order they must appear
	}{
		{&llm.ServerError{Status: 401, Message: "missing or wrong API key", Hint: "send Authorization: Bearer <api_key>"},
			"Engine Wants a Key", []string{"send Authorization: Bearer <api_key>",
				"Set key in ~/.taracode/config.yaml, or pass --key"}},
		{&llm.ServerError{Status: 404, Message: "gemma4:12b is not pulled", Model: "gemma4:12b"}, "Model Not Pulled",
			[]string{"Pull it: taraengine pull gemma4:12b", "Switch models with: /model"}},
		{&llm.ServerError{Status: 409, Message: "model is faulty: x", Hint: "load it with force to try again", Model: "x"},
			"Model Marked Faulty", []string{"load it with force to try again", "Retry it: taraengine load x --force"}},
		{&llm.ServerError{Status: 502, Message: "x exited", Hint: "taraengine doctor shows the last lines of the log"},
			"Model Failed to Load", []string{"taraengine doctor shows the last lines of the log"}},
		{&llm.ServerError{Status: 503, Message: "the engine backend is not running"}, "Engine Not Ready",
			[]string{"taracode already retried once"}},
		{&llm.ServerError{Status: 504, Message: "load timed out", Hint: "raise load_timeout in the config for a slow disk"},
			"Model Load Timed Out", []string{"raise load_timeout in the config for a slow disk"}},
		{&llm.ServerError{Status: 400, Message: "context 256 is out of bounds"}, "Server Refused the Request",
			[]string{"context 256 is out of bounds", "Run taracode doctor"}},
		{fmt.Errorf("loading x: %w", &llm.ServerError{Status: 404, Message: "unknown model: x"}), "Model Not Pulled",
			[]string{"taraengine pull <model>"}},
		{&llm.UnreachableError{Host: "http://127.0.0.1:8272", Hint: "start taraengine with `taraengine serve`"},
			"Server Not Reachable", []string{"start taraengine with `taraengine serve`", "Run taracode doctor"}},
		{fmt.Errorf("chat: %w", llm.ErrStreamCut), "Answer Cut Off", []string{"Run taracode doctor to see why"}},
	}
	for _, c := range cases {
		out := EnhanceError(c.err)
		if !strings.Contains(out, c.title) {
			t.Errorf("%v: no title %q:\n%s", c.err, c.title, out)
		}
		at := 0
		for _, line := range c.lines {
			i := strings.Index(out[at:], line)
			if i < 0 {
				t.Errorf("%v: lacks %q after offset %d:\n%s", c.err, line, at, out)
				break
			}
			at += i + len(line)
		}
	}
}

func TestStartupErrorExplainsTheServersAnswer(t *testing.T) {
	refused := fmt.Errorf("loading x: %w", &llm.ServerError{Status: 401, Message: "missing or wrong API key"})
	if out := StartupError("http://h", refused); !strings.Contains(out, "Engine Wants a Key") ||
		strings.Contains(out, "Cannot connect") {
		t.Fatalf("a refusal at startup:\n%s", out)
	}
	if out := StartupError("http://h", errors.New("tls: handshake failure")); !strings.Contains(out,
		"Cannot connect to LLM server") || !strings.Contains(out, "curl http://h/api/health") {
		t.Fatalf("anything else:\n%s", out)
	}
}

func TestProviderNoticeIsForOllamaOnly(t *testing.T) {
	r := NewRenderer()
	notice := r.ProviderNotice(&provider.Info{Type: provider.TypeOllama, Name: "Ollama"})
	for _, want := range []string{"no window control", "no thinking on or off", "no /model", "no evals",
		"brew install --cask tara-vision/taracode/taraengine"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice lacks %q: %q", want, notice)
		}
	}
	if r.ProviderNotice(&provider.Info{Type: provider.TypeVLLM}) != "" || r.ProviderNotice(nil) != "" {
		t.Error("no notice for other servers")
	}
}
