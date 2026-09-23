package assistant

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/tools"
)

// TestSwitchModelRefusesAModelWithoutToolsAndKeepsTheOldOne covers the capability gate SwitchModel
// runs on the new model: a model the fake server lists without the "tools" capability is refused,
// and the assistant is left exactly as it was (the model, the provider's model and the resolved
// context window all stay at their pre-switch values).
func TestSwitchModelRefusesAModelWithoutToolsAndKeepsTheOldOne(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Models = append(srv.Models, ollamatest.ModelSpec{
		Name: "old:7b", Capabilities: []string{"completion"}, ContextLength: 4096, Family: "old",
	})
	beforeModel := a.GetCurrentModel()
	beforeWindow := a.contextWindow

	err := a.SwitchModel("old:7b")

	if err == nil || !strings.Contains(err.Error(), "does not support tools") {
		t.Fatalf("SwitchModel(old:7b) = %v, want the capability gate error", err)
	}
	if a.GetCurrentModel() != beforeModel {
		t.Fatalf("GetCurrentModel() = %q after a refused switch, want unchanged %q", a.GetCurrentModel(), beforeModel)
	}
	if a.GetProviderInfo().Model != beforeModel {
		t.Fatalf("provider model = %q after a refused switch, want unchanged %q", a.GetProviderInfo().Model, beforeModel)
	}
	if a.contextWindow != beforeWindow {
		t.Fatalf("contextWindow = %d after a refused switch, want unchanged %d", a.contextWindow, beforeWindow)
	}
}

// TestNewConstructsAgainstAShowlessBackend covers New() against an OpenAI-compatible backend
// (vendor "vllm", so the go-openai adapter is used): Show always returns llm.ErrNotSupported on
// that path, and New must still construct a usable Assistant instead of failing, leaving the
// context window unresolved (0).
func TestNewConstructsAgainstAShowlessBackend(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	a, err := New(srv.URL, "", "gemma4:12b", "vllm", false, false, tools.Config{})
	if err != nil {
		t.Fatalf("New() against a Show-less backend should still construct: %v", err)
	}
	if a.contextWindow != 0 {
		t.Fatalf("contextWindow = %d, want 0 (unresolved on a Show-less backend)", a.contextWindow)
	}
	if a.GetCurrentModel() != "gemma4:12b" {
		t.Fatalf("GetCurrentModel() = %q, want gemma4:12b", a.GetCurrentModel())
	}
}
