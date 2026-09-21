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
	rep := Diagnose(context.Background(), ollamaclient.New(srv.URL, http.DefaultClient), srv.URL, 32, "gemma4:12b", lookPath)
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
}

func TestDiagnoseUnreachableServer(t *testing.T) {
	rep := Diagnose(context.Background(), ollamaclient.New("http://127.0.0.1:1", http.DefaultClient), "http://127.0.0.1:1", 16, "", func(string) (string, error) { return "", errors.New("x") })
	if rep.ServerOK || rep.ServerError == "" {
		t.Fatalf("expected an unreachable report: %+v", rep)
	}
	if !strings.Contains(rep.Render(), "unreachable") {
		t.Fatalf("render: %s", rep.Render())
	}
}
