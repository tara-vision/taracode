package assistant

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
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

	_, err := New(srv.URL, "", "old:7b", "ollama", false, false)
	if err == nil || !strings.Contains(err.Error(), "does not support tools") {
		t.Fatalf("expected the capability gate, got %v", err)
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
