package agent

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
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
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	a, err := New(Options{Host: srv.URL, Model: "gemma4:12b", Vendor: "vllm"})
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

// TestNewClampsInvalidCompactionSettings covers a fix review regression: New used to clamp an
// out-of-range compaction threshold (2.x examples used percent-like values such as 75 instead of
// 0.75) and a non-positive keep-recent, but that clamp was lost when the viper reads moved into
// Options. Losing it does not just silently disable compaction (an unclamped 75 as a fraction
// never triggers): a negative KeepRecent also reopens a slice-bounds panic the first time
// compaction fires, since CompactConversation's keepEnd := len(conversation) - KeepRecent*2
// overshoots len(conversation) once KeepRecent is negative. This covers both: the assistant ends
// up with the same 0.75/4 defaults the old code fell back to, and ForceCompact on a short
// conversation refuses cleanly instead of panicking.
func TestNewClampsInvalidCompactionSettings(t *testing.T) {
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	a, err := New(Options{
		Host: srv.URL, Model: "gemma4:12b", Vendor: "ollama",
		Compaction: CompactionConfig{Enabled: true, Threshold: 75, KeepRecent: -1, MaxTokens: 32768},
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if a.compactionCfg.Threshold != 0.75 {
		t.Fatalf("compactionCfg.Threshold = %v, want the 0.75 default", a.compactionCfg.Threshold)
	}
	if a.compactionCfg.KeepRecent != 4 {
		t.Fatalf("compactionCfg.KeepRecent = %v, want the 4 default", a.compactionCfg.KeepRecent)
	}

	// The panic path: before the clamp, KeepRecent -1 made ForceCompact's own too-short guard
	// (KeepRecent*2+3) go non-positive, so a short conversation sailed straight into
	// CompactConversation and panicked on the resulting out-of-range slice. With KeepRecent
	// correctly clamped to 4, the same short conversation is refused instead.
	a.conversation = append(a.conversation,
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "hi"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "hello"},
	)
	if err := a.ForceCompact(); err == nil || !strings.Contains(err.Error(), "too short to compact") {
		t.Fatalf("ForceCompact() = %v, want the too-short-to-compact error, not a panic", err)
	}
}
