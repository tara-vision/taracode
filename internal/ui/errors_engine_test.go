package ui

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/provider"
)

// TestEnhanceErrorPutsTheEngineHintFirst: the server's hint is the first suggestion and shows once; the dimmed line
// under the title carries the error without it.
func TestEnhanceErrorPutsTheEngineHintFirst(t *testing.T) {
	cases := []struct {
		err   error
		title string
		dim   string   // the dimmed error line, in full
		lines []string // the suggestions, in the order they must appear; the first shows once in the whole output
	}{
		{&llm.ServerError{Status: 401, Message: "missing or wrong API key", Hint: "send Authorization: Bearer <api_key>"},
			"Engine Wants a Key", "missing or wrong API key", []string{"send Authorization: Bearer <api_key>",
				"Set key in ~/.taracode/config.yaml, or pass --key"}},
		{&llm.ServerError{Status: 404, Message: "gemma4:12b is not pulled", Model: "gemma4:12b"}, "Model Not Pulled",
			"gemma4:12b is not pulled", []string{"Pull it: taraengine pull gemma4:12b", "Switch models with: /model"}},
		{&llm.ServerError{Status: 409, Message: "model is faulty: x", Hint: "load it with force to try again", Model: "x"},
			"Model Marked Faulty", "model is faulty: x", []string{"load it with force to try again",
				"Retry it: taraengine load x --force"}},
		{&llm.ServerError{Status: 502, Message: "x exited", Hint: "taraengine doctor shows the last lines of the log"},
			"Model Failed to Load", "x exited", []string{"taraengine doctor shows the last lines of the log",
				"taracode doctor shows why"}},
		{&llm.ServerError{Status: 503, Message: "the engine backend is not running"}, "Engine Not Ready",
			"the engine backend is not running", []string{"taracode already retried once"}},
		{&llm.ServerError{Status: 504, Message: "load timed out", Hint: "raise load_timeout in the config for a slow disk"},
			"Model Load Timed Out", "load timed out", []string{"raise load_timeout in the config for a slow disk",
				"taracode doctor shows the model's state"}},
		{&llm.ServerError{Status: 400, Message: "context 256 is out of bounds"}, "Server Refused the Request",
			"context 256 is out of bounds", []string{"Run taracode doctor"}},
		{&llm.ServerError{Status: 500, Message: "Failed to parse tool call arguments as JSON: syntax error at line 1, " +
			"column 10764: missing closing quote"}, "Tool Call Cut Off",
			"Failed to parse tool call arguments as JSON: syntax error at line 1, column 10764: missing closing quote",
			[]string{"The model's tool call was longer than the output limit",
				"Raise generation.num_predict in config.yaml, or ask for a smaller step"}},
		{fmt.Errorf("openai: %w", &openai.APIError{HTTPStatusCode: 500, HTTPStatus: "500 Internal Server Error",
			Message: "500: incomplete GLM tool call"}), "Tool Call Cut Off",
			"openai: error, status code: 500, status: 500 Internal Server Error, message: 500: incomplete GLM tool call",
			[]string{"The model's tool call was longer than the output limit",
				"Raise generation.num_predict in config.yaml, or ask for a smaller step"}},
		{fmt.Errorf("loading x: %w", &llm.ServerError{Status: 404, Message: "unknown model: x",
			Hint: "taraengine list shows the catalog"}), "Model Not Pulled", "loading x: unknown model: x",
			[]string{"taraengine list shows the catalog", "taraengine pull <model>"}},
		{&llm.UnreachableError{Host: "http://127.0.0.1:8272", Hint: "start taraengine with `taraengine serve`"},
			"Server Not Reachable", "nothing answers at http://127.0.0.1:8272",
			[]string{"start taraengine with `taraengine serve`", "Run taracode doctor"}},
		{fmt.Errorf("chat: %w", llm.ErrStreamCut), "Answer Cut Off", "chat: " + llm.ErrStreamCut.Error(),
			[]string{"Run taracode doctor to see why"}},
	}
	for _, c := range cases {
		out := EnhanceError(c.err)
		head, suggestions, ok := strings.Cut(out, "Suggestions:")
		if !ok || !strings.Contains(head, c.title) || !strings.Contains(head, "\n  "+c.dim+"\n") {
			t.Errorf("%v: want the title %q and the dimmed line %q above the suggestions:\n%s", c.err, c.title, c.dim,
				out)
			continue
		}
		at := 0
		for _, line := range c.lines {
			i := strings.Index(suggestions[at:], line)
			if i < 0 {
				t.Errorf("%v: the suggestions lack %q after offset %d:\n%s", c.err, line, at, suggestions)
				break
			}
			at += i + len(line)
		}
		if n := strings.Count(out, c.lines[0]); n != 1 {
			t.Errorf("%v: %q shows %d times, want once:\n%s", c.err, c.lines[0], n, out)
		}
	}
}

// TestStartupErrorExplainsTheServersAnswer: a refusal the server explained, an engine with nothing pulled and a model
// without tools get their own suggestions; only a connection failure, or an error nothing explains, gets the box that
// says taracode cannot connect.
func TestStartupErrorExplainsTheServersAnswer(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"a refusal", fmt.Errorf("loading x: %w", &llm.ServerError{Status: 401, Message: "missing or wrong API key"}),
			[]string{"Engine Wants a Key"}},
		{"nothing pulled", errors.New("no models available and no fallback configured"),
			[]string{"No Models Available", "taraengine pull <model>", "taracode doctor"}},
		{"a model without tools", fmt.Errorf("%w: embed:1b (pick one from `taracode doctor`)",
			errors.New("model does not support tools")),
			[]string{"Model Without Tools", "Choose a model with tools: taracode doctor lists them"}},
	}
	for _, c := range cases {
		out := StartupError("http://h", c.err)
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: lacks %q:\n%s", c.name, want, out)
			}
		}
		if strings.Contains(out, "Cannot connect") {
			t.Errorf("%s: the connection box for an error that is not about the connection:\n%s", c.name, out)
		}
	}
	refused := &url.Error{Op: "Get", URL: "http://h/v1/models",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}}
	for _, err := range []error{
		fmt.Errorf("failed to detect models and no fallback configured: request failed: %w", refused),
		errors.New("failed to detect models and no fallback configured: context deadline exceeded"),
		errors.New("tls: handshake failure"),
	} {
		if out := StartupError("http://h", err); !strings.Contains(out, "Cannot connect to LLM server") ||
			!strings.Contains(out, "curl http://h/api/health") {
			t.Errorf("%v: want the connection box:\n%s", err, out)
		}
	}
}

func TestProviderNoticeIsForOllamaOnly(t *testing.T) {
	r := NewRenderer()
	notice := r.ProviderNotice(&provider.Info{Type: provider.TypeOllama, Name: "Ollama"})
	want := IconWarning + " Ollama runs on the generic OpenAI-compatible path: no window control (set " +
		"OLLAMA_CONTEXT_LENGTH on the Ollama server), no thinking on or off, no /model and no evals. taraengine is the " +
		"native backend: brew install --cask tara-vision/taracode/taraengine on a Mac, or curl -fsSL " +
		"https://engine.tara.vision/install.sh | bash on Linux\n"
	if notice != want {
		t.Errorf("the notice:\n got %q\nwant %q", notice, want)
	}
	if r.ProviderNotice(&provider.Info{Type: provider.TypeVLLM}) != "" || r.ProviderNotice(nil) != "" {
		t.Error("no notice for other servers")
	}
}
