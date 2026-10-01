package cmd

import (
	"strings"
	"testing"

	"github.com/manifoldco/promptui"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/models"
)

// twoModelREPL is an initialised repl on a fake serving gemma4:12b (current) and qwen3.5:9b.
func twoModelREPL(t *testing.T, secondCapabilities ...string) (*repl, *ollamatest.Server) {
	t.Helper()
	r, srv := projectREPL(t)
	if len(secondCapabilities) == 0 {
		secondCapabilities = []string{"completion", "tools"}
	}
	srv.Models = append(srv.Models, ollamatest.ModelSpec{Name: "qwen3.5:9b", Capabilities: secondCapabilities,
		ContextLength: 32768, Family: "qwen3", ParameterSize: "9B"})
	return r, srv
}

func TestModelSwitchesToTheChosenModel(t *testing.T) {
	r, srv := twoModelREPL(t)
	shown := answerPicker(t, 1, nil)
	out := captureStdoutForTest(t, func() { r.dispatch("/model") })
	for _, want := range []string{
		"Current model: gemma4:12b", "Switching from gemma4:12b to qwen3.5:9b...", "Now using: qwen3.5:9b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/model lacks %q:\n%s", want, out)
		}
	}
	items, _ := shown.Items.([]string)
	if shown.Label != "Select a model" || shown.Size != 15 || len(items) != 2 || items[0] != "* gemma4:12b (12B)" ||
		items[1] != "  qwen3.5:9b (9B)" {
		t.Fatalf("the picker showed %+v", shown)
	}
	if r.asst.GetCurrentModel() != "qwen3.5:9b" || r.asst.GetStorage().GetPreferredModel() != "qwen3.5:9b" {
		t.Fatalf("model %s, saved %s", r.asst.GetCurrentModel(), r.asst.GetStorage().GetPreferredModel())
	}
	if strings.Join(srv.Unloaded, ",") != "gemma4:12b" {
		t.Fatalf("the old model is unloaded first: %v", srv.Unloaded)
	}
}

func TestModelKeepsTheCurrentModel(t *testing.T) {
	r, srv := twoModelREPL(t)
	answerPicker(t, 0, nil)
	out := captureStdoutForTest(t, func() { r.dispatch("/model") })
	if !strings.Contains(out, "Already using gemma4:12b") || len(srv.Unloaded) != 0 {
		t.Fatalf("output %q, unloaded %v", out, srv.Unloaded)
	}
}

func TestModelSwitchCancelled(t *testing.T) {
	r, _ := twoModelREPL(t)
	answerPicker(t, 0, promptui.ErrInterrupt)
	out := captureStdoutForTest(t, func() { r.dispatch("/model") })
	if !strings.Contains(out, "Model switch cancelled.") || strings.Contains(out, "Already using") ||
		strings.Contains(out, "Switching") || r.asst.GetCurrentModel() != "gemma4:12b" {
		t.Fatalf("%q", out)
	}
}

func TestModelSwitchRefusedForAModelWithoutTools(t *testing.T) {
	r, _ := twoModelREPL(t, "completion")
	answerPicker(t, 1, nil)
	_, stderr := captureOutput(t, func() { r.dispatch("/model") })
	if !strings.Contains(stderr, "Error switching model:") || !strings.Contains(stderr, "does not support tools") ||
		r.asst.GetCurrentModel() != "gemma4:12b" {
		t.Fatalf("stderr %q, model %s", stderr, r.asst.GetCurrentModel())
	}
}

func TestModelWithNothingToChoose(t *testing.T) {
	r, srv := projectREPL(t)
	srv.Models = nil
	out := captureStdoutForTest(t, func() { r.dispatch("/model") })
	want := "Pull a model with: ollama pull " + models.DefaultName(models.Tier16) + " (16 GB) or " +
		models.DefaultName(models.Tier32) + " (32 GB)"
	if !strings.Contains(out, "No models available.") || !strings.Contains(out, want) {
		t.Fatalf("%q", out)
	}
}

// TestModelOnAServerThatCannotListModels: an OpenAI-compatible server has no model listing.
func TestModelOnAServerThatCannotListModels(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true, func(o *agent.Options) { o.Vendor = "vllm" })
	stdout, stderr := captureOutput(t, func() { r.dispatch("/model") })
	if !strings.Contains(stdout, "Current model: gemma4:12b") || !strings.Contains(stderr, "does not support model listing") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
}
