package assistant

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/tools"
)

func TestResolveContextWindow(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		modelMax   int
		want       int
		warn       string
	}{
		{"auto caps at 32k", "auto", 262144, 32768, ""},
		{"auto follows a smaller model", "auto", 16384, 16384, ""},
		{"auto below the floor warns", "auto", 8192, 8192, "below 16384"},
		{"auto with unknown model max", "auto", 0, 32768, ""},
		{"explicit number wins", "65536", 262144, 65536, ""},
		{"explicit above the model max is clamped", "65536", 32768, 32768, "clamped"},
		{"explicit below the floor warns", "8192", 262144, 8192, "below 16384"},
		{"garbage falls back to auto", "lots", 262144, 32768, "not a number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warn := ResolveContextWindow(tc.configured, tc.modelMax)
			if got != tc.want || (tc.warn == "" && warn != "") || (tc.warn != "" && !strings.Contains(warn, tc.warn)) {
				t.Fatalf("got %d %q, want %d containing %q", got, warn, tc.want, tc.warn)
			}
		})
	}
}

// TestNewRefusesAModelWithoutToolSupport runs New for real against a fake server whose only model
// lacks the "tools" capability: the capability gate must refuse it before the assistant is usable.
func TestNewRefusesAModelWithoutToolSupport(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "old:7b", Capabilities: []string{"completion"}, ContextLength: 4096, Family: "old"}}

	_, err := New(srv.URL, "", "old:7b", "ollama", false, false, tools.Config{})
	if err == nil || !strings.Contains(err.Error(), "does not support tools") {
		t.Fatalf("expected the capability gate, got %v", err)
	}
}

// TestApplyModelDetailsHandlesShowErrors covers the fix for every Show error being treated as the
// OpenAI-compatible case: only llm.ErrNotSupported keeps num_ctx unresolved (a server without
// capability data); any other error (a transient Show failure, for example) still resolves and requests a
// window instead of silently disabling num_ctx for the whole session, and warns once through the
// renderer.
func TestApplyModelDetailsHandlesShowErrors(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantWindow int
		wantWarn   bool
	}{
		{"not supported keeps num_ctx unresolved", llm.ErrNotSupported, 0, false},
		{"a transient error still resolves a window", errors.New("dial tcp: connection refused"), defaultContextWindow, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestAssistant(t, false)
			a.contextWindow = -1 // sentinel: applyModelDetails must set this itself, not leave it stale

			var applyErr error
			out := captureStdout(t, func() {
				applyErr = a.applyModelDetails(nil, tc.err)
			})

			if applyErr != nil {
				t.Fatalf("applyModelDetails: %v", applyErr)
			}
			if a.contextWindow != tc.wantWindow {
				t.Fatalf("contextWindow = %d, want %d", a.contextWindow, tc.wantWindow)
			}
			if tc.wantWarn != strings.Contains(out, "Could not read model capabilities") {
				t.Fatalf("output = %q, wantWarn = %v", out, tc.wantWarn)
			}
		})
	}
}

// TestApplyModelDetailsDowngradesThinkWhenTheModelHasNoThinkingCapability covers the fix for a
// configured think level making every turn fail with Ollama's 400 on a model whose /api/show lacks
// the thinking capability: applyModelDetails resets think to auto and warns once.
func TestApplyModelDetailsDowngradesThinkWhenTheModelHasNoThinkingCapability(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.think = llm.ThinkHigh
	details := &llm.ModelDetails{Capabilities: []string{"completion", "tools"}, ContextLength: 32768}

	out := captureStdout(t, func() {
		if err := a.applyModelDetails(details, nil); err != nil {
			t.Fatal(err)
		}
	})

	if a.think != llm.ThinkAuto {
		t.Fatalf("think = %q, want auto", a.think)
	}
	if !strings.Contains(out, "does not support thinking") {
		t.Fatalf("no downgrade warning printed: %q", out)
	}
}

// TestSetThinkDowngradesOnAModelWithoutThinkingSupport covers the runtime /think command bypassing
// the capability downgrade applyModelDetails already applies at startup and on model switch: on a
// tools-only model, SetThink must apply the same downgrade, warn the same way, and report the mode
// that will actually reach the wire, so a turn never puts a think level Ollama will 400 on.
func TestSetThinkDowngradesOnAModelWithoutThinkingSupport(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.thinkingSupported = false // as applyModelDetails leaves it for a tools-only model
	srv.Turns = []ollamatest.Turn{{Content: "ok"}}

	var effective llm.Think
	out := captureStdout(t, func() {
		effective = a.SetThink(llm.ThinkHigh)
	})

	if effective != llm.ThinkAuto {
		t.Fatalf("SetThink returned %q, want auto", effective)
	}
	if a.Think() != llm.ThinkAuto {
		t.Fatalf("Think() = %q, want auto", a.Think())
	}
	if !strings.Contains(out, "does not support thinking") {
		t.Fatalf("no downgrade warning printed: %q", out)
	}

	if err := a.ProcessMessage("hi"); err != nil {
		t.Fatal(err)
	}
	body := lastChatBody(t, srv)
	if think, present := body["think"]; present {
		t.Fatalf("think should be omitted on the wire (auto semantics), got %v", think)
	}
}

func TestThinkAndWindowReachTheRequest(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.SetThink(llm.ThinkHigh)
	a.contextWindow = 16384
	srv.Turns = []ollamatest.Turn{{Content: "ok"}}
	if err := a.ProcessMessage("hi"); err != nil {
		t.Fatal(err)
	}
	// lastChatBody (loop_test.go), not the raw last request: ProcessMessage's deferred server
	// context check issues a trailing /api/ps probe after /api/chat.
	body := lastChatBody(t, srv)
	if body["think"] != "high" || body["options"].(map[string]any)["num_ctx"] != float64(16384) {
		t.Fatalf("request: %v", body)
	}
	if a.Think() != llm.ThinkHigh {
		t.Fatalf("Think() = %q", a.Think())
	}
}
