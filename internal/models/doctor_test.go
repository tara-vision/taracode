package models

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	ollamaclient "github.com/tara-vision/taracode/internal/llm/ollama"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// noLookup is the lookPath stub every test that does not care about external tools passes.
func noLookup(string) (string, error) { return "", errors.New("not found") }

func TestDiagnoseReportsServerModelsAndRecommendation(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Version = "0.34.2"
	srv.Models = []ollamatest.ModelSpec{
		{Name: "qwen2.5:7b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "qwen2", Size: 4_700_000_000},
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools", "thinking", "vision"}, ContextLength: 262144, Family: "gemma4", Size: 7_600_000_000},
	}
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 32768}}
	lookPath := func(name string) (string, error) {
		if name == "kubectl" || name == "git" {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	rep := Diagnose(context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 32, "gemma4:12b", lookPath, nil)
	if !rep.ServerOK || rep.ServerVersion != "0.34.2" || rep.RAMGB != 32 || rep.Tier != Tier32 {
		t.Fatalf("server section: %+v", rep)
	}
	if len(rep.Models) != 2 || !rep.Models[0].Tools || !rep.Models[0].Thinking || rep.Models[0].Context != 262144 { // sorted by name: gemma4 first
		t.Fatalf("models section: %+v", rep.Models)
	}
	if rep.LoadedContext != 32768 || rep.ConfiguredModel != "gemma4:12b" {
		t.Fatalf("context section: %+v", rep)
	}
	rec, ok := rep.Recommendation()
	if !ok || rec.Name != "qwen3.8:27b" || rep.RecommendationInstalled {
		t.Fatalf("recommendation: %+v %v", rec, ok)
	}
	if got := rep.Tools["kubectl"]; got != "/usr/bin/kubectl" {
		t.Fatalf("tools: %v", rep.Tools)
	}
	if got := rep.Tools["terraform"]; got != "" {
		t.Fatalf("missing tool should be empty: %q", got)
	}
	text := rep.Render()
	for _, want := range []string{"Ollama 0.34.2", "32 GB", "gemma4:12b", "tools thinking vision", "ollama pull qwen3.8:27b", "terraform: not found"} {
		if !strings.Contains(text, want) {
			t.Fatalf("render lacks %q:\n%s", want, text)
		}
	}
	assertRegistryMarkers(t, text, "gemma4:12b", "qwen2.5:7b")
}

// assertRegistryMarkers covers the registry-membership column (fix round 1, item a): the first
// line mentioning each model name (its Models-section row; a later "Model ..." line can repeat
// the configured model's name) must carry the right marker.
func assertRegistryMarkers(t *testing.T, text, inRegistryModel, notInRegistryModel string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	var inLine, outLine string
	for _, line := range lines {
		if inLine == "" && strings.Contains(line, inRegistryModel) {
			inLine = line
		}
		if outLine == "" && strings.Contains(line, notInRegistryModel) {
			outLine = line
		}
	}
	if !strings.HasSuffix(inLine, "registry") || strings.Contains(inLine, "not in registry") {
		t.Fatalf("%s line = %q, want it to end with \"registry\"", inRegistryModel, inLine)
	}
	if !strings.Contains(outLine, "not in registry") {
		t.Fatalf("%s line = %q, want \"not in registry\"", notInRegistryModel, outLine)
	}
}

func TestDiagnoseUnreachableServer(t *testing.T) {
	rep := Diagnose(
		context.Background(), ollamaclient.New("http://127.0.0.1:1", http.DefaultClient), "http://127.0.0.1:1", 16,
		"", func(string) (string, error) { return "", errors.New("x") }, nil,
	)
	if rep.ServerOK || rep.ServerError == "" {
		t.Fatalf("expected an unreachable report: %+v", rep)
	}
	if !strings.Contains(rep.Render(), "unreachable") {
		t.Fatalf("render: %s", rep.Render())
	}
}

// TestDiagnoseWarnsWhenOllamaIsOlderThanAModelNeeds covers the Ollama-version gate (fix round 1,
// item b): a server older than a model's registry minimum gets a "needs Ollama >=" note on that
// model's line; a model the server already satisfies gets none. Registry minimums per
// registry.yaml: gemma4:12b needs 0.20.0, qwen3.8:27b needs 0.32.12.
func TestDiagnoseWarnsWhenOllamaIsOlderThanAModelNeeds(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Version = "0.30.0"
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 262144, Family: "gemma4"},
		{Name: "qwen3.8:27b", Capabilities: []string{"completion", "tools"}, ContextLength: 262144, Family: "qwen3.8"},
	}
	rep := Diagnose(context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 32, "", noLookup, nil)
	lines := strings.Split(rep.Render(), "\n")
	var gemmaLine, qwenLine string
	for _, line := range lines {
		if gemmaLine == "" && strings.Contains(line, "gemma4:12b") {
			gemmaLine = line
		}
		if qwenLine == "" && strings.Contains(line, "qwen3.8:27b") {
			qwenLine = line
		}
	}
	if !strings.Contains(qwenLine, "needs Ollama >= 0.32.12") {
		t.Fatalf("qwen line = %q, want the version gate", qwenLine)
	}
	if strings.Contains(gemmaLine, "needs Ollama") {
		t.Fatalf("gemma line = %q, should not need a newer Ollama", gemmaLine)
	}
}

// TestRenderSkipsVersionGateWithoutAServerVersion covers the other half of the version gate: the
// OpenAI-compatible path has no version (Version() is a stub returning ""), so a model with a
// registry minimum must not be flagged. It also covers the fix for the empty-version Server line:
// without a version, Render must print "server reachable" rather than "Ollama  reachable" with an
// empty version left in the middle.
func TestRenderSkipsVersionGateWithoutAServerVersion(t *testing.T) {
	rep := Report{
		Host:     "http://example",
		ServerOK: true,
		Models:   []InstalledModel{{Name: "gemma4:12b", MinOllama: "0.20.0", InRegistry: true}},
	}
	text := rep.Render()
	if strings.Contains(text, "needs Ollama") {
		t.Fatalf("render should not gate on version without a server version:\n%s", text)
	}
	if !strings.Contains(text, "server reachable") {
		t.Fatalf("render should name a versionless server reachable:\n%s", text)
	}
	if strings.Contains(text, "Ollama  reachable") {
		t.Fatalf("render should not print an empty version:\n%s", text)
	}
}

// TestDiagnoseResolvesTheContextWindowForTheConfiguredModel covers the context-window section
// (fix round 1, item c): once the configured model is found among the installed models,
// resolveWindow runs against its native context and the report shows what it returned.
func TestDiagnoseResolvesTheContextWindowForTheConfiguredModel(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Version = "0.34.2"
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 131072, Family: "gemma4"},
	}
	resolveWindow := func(modelMax int) (int, string) {
		if modelMax != 131072 {
			t.Fatalf("resolveWindow called with modelMax = %d, want 131072", modelMax)
		}
		return 32768, "context window capped for the test"
	}
	rep := Diagnose(
		context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 32, "gemma4:12b", noLookup,
		resolveWindow,
	)
	text := rep.Render()
	if !strings.Contains(text, "Context   32768 tokens will be requested per turn") {
		t.Fatalf("render lacks the context window line:\n%s", text)
	}
	if !strings.Contains(text, "context window capped for the test") {
		t.Fatalf("render lacks the context note:\n%s", text)
	}
}

// TestDiagnoseSkipsTheContextWindowWhenTheConfiguredModelIsNotInstalled covers the other side: no
// installed model matches ConfiguredModel, so there is nothing to resolve a window for.
func TestDiagnoseSkipsTheContextWindowWhenTheConfiguredModelIsNotInstalled(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Version = "0.34.2"
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 131072, Family: "gemma4"},
	}
	resolveWindow := func(_ int) (int, string) {
		t.Fatal("resolveWindow must not be called when the configured model is not installed")
		return 0, ""
	}
	rep := Diagnose(
		context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 32, "not-installed:1b", noLookup,
		resolveWindow,
	)
	if strings.Contains(rep.Render(), "Context   ") {
		t.Fatalf("render should not show a context window for an uninstalled model:\n%s", rep.Render())
	}
}

// TestRenderShowsARegistryLoadError covers item d: a Report built with RegistryError set (the
// embedded registry failed to parse, which Diagnose cannot force in a test) prints it right after
// the Server block.
func TestRenderShowsARegistryLoadError(t *testing.T) {
	rep := Report{RegistryError: "boom"}
	if !strings.Contains(rep.Render(), "Registry  unavailable: boom") {
		t.Fatalf("render lacks the registry error:\n%s", rep.Render())
	}
}

// TestRenderShowsThePolicyNote covers the doctor's policy line (Task 14): a Report with PolicyNote
// set prints it right after the model/context section; an empty PolicyNote (Diagnose does not set
// it - only cmd's runDoctor and handleDoctor do) prints no Policy line at all.
func TestRenderShowsThePolicyNote(t *testing.T) {
	rep := Report{Host: "http://example", ServerOK: true, PolicyNote: "ok (built-in)"}
	if !strings.Contains(rep.Render(), "Policy    ok (built-in)") {
		t.Fatalf("render lacks the policy note:\n%s", rep.Render())
	}
	if strings.Contains((&Report{Host: "http://example", ServerOK: true}).Render(), "Policy") {
		t.Fatal("render should not show a Policy line without a PolicyNote")
	}
}
