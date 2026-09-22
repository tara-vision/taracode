package assistant

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestResetServerContextCheck(t *testing.T) {
	a := &Assistant{serverContextChecked: true, serverContextTokens: 32000}

	a.resetServerContextCheck()

	if a.serverContextChecked {
		t.Fatal("serverContextChecked = true, want false after reset")
	}
	if a.serverContextTokens != 0 {
		t.Fatalf("serverContextTokens = %d, want 0 after reset", a.serverContextTokens)
	}
}

func TestServerContextAdvice(t *testing.T) {
	tests := []struct {
		name          string
		serverCtx     int
		systemTokens  int
		toolTokens    int
		configuredMax int
		threshold     float64
		wantWarn      bool
		wantContains  string
	}{
		{name: "unknown server context", serverCtx: 0, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "default 4096 overflows the tool budget", serverCtx: 4096, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: true, wantContains: "context.window"},
		{name: "server below the compaction point", serverCtx: 16384, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: true, wantContains: "max_context_tokens"},
		{name: "server slightly below config but above the compaction point", serverCtx: 32000, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "server matches config", serverCtx: 32000, systemTokens: 300, toolTokens: 5800, configuredMax: 32000, threshold: 0.75, wantWarn: false},
		{name: "server above config", serverCtx: 65536, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0.75, wantWarn: false},
		{name: "threshold out of range compares to the full max", serverCtx: 30000, systemTokens: 300, toolTokens: 5800, configuredMax: 32768, threshold: 0, wantWarn: true, wantContains: "max_context_tokens"},
		{name: "no configured max", serverCtx: 16384, systemTokens: 300, toolTokens: 5800, configuredMax: 0, threshold: 0.75, wantWarn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, warn := ServerContextAdvice(tt.serverCtx, tt.systemTokens, tt.toolTokens, tt.configuredMax, tt.threshold)
			if warn != tt.wantWarn {
				t.Fatalf("warn = %v, want %v (msg %q)", warn, tt.wantWarn, msg)
			}
			if tt.wantContains != "" && !strings.Contains(msg, tt.wantContains) {
				t.Fatalf("message %q does not contain %q", msg, tt.wantContains)
			}
			if !tt.wantWarn && msg != "" {
				t.Fatalf("expected empty message, got %q", msg)
			}
		})
	}
}

func TestLoadedContextLength(t *testing.T) {
	loaded := []llm.LoadedModel{
		{Name: "qwen3.8:27b", ContextLength: 32768},
		{Name: "gemma4:latest", ContextLength: 16384},
	}

	tests := []struct {
		name  string
		model string
		want  int
	}{
		{name: "exact tag", model: "qwen3.8:27b", want: 32768},
		{name: "untagged model matches :latest", model: "gemma4", want: 16384},
		{name: "model not loaded", model: "llama9:70b", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := loadedContextLength(loaded, tt.model); got != tt.want {
				t.Fatalf("loadedContextLength(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestCheckServerContextOnceWarnsAboutASmallWindow(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Loaded = []ollamatest.LoadedSpec{{Name: "gemma4:12b", ContextLength: 4096}}
	srv.Turns = []ollamatest.Turn{{Content: "hi"}}

	if err := a.ProcessMessage("hello"); err != nil {
		t.Fatal(err)
	}

	if !a.serverContextChecked || a.serverContextTokens != 4096 {
		t.Fatalf("checked = %v, tokens = %d", a.serverContextChecked, a.serverContextTokens)
	}
	if info := a.GetContextInfo(); info.ServerContextTokens != 4096 {
		t.Fatalf("context info = %+v", info)
	}
}

func TestCheckServerContextOnceKeepsTryingWhileTheModelIsNotLoaded(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{{Content: "hi"}}

	if err := a.ProcessMessage("hello"); err != nil {
		t.Fatal(err)
	}

	if a.serverContextChecked {
		t.Fatal("the check should stay open until the server reports the model")
	}
}
