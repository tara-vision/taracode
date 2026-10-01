package cmd

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
)

func TestThinkShowsAndSetsTheReasoningMode(t *testing.T) {
	srv := fakeOllama(t)
	srv.Models[0].Capabilities = append(srv.Models[0].Capabilities, "thinking")
	r := replOn(t, srv, t.TempDir(), true)
	tests := []struct {
		command, want string
	}{
		{"/think", "Reasoning mode: auto\n"},
		{"/think high", "Reasoning mode set to high.\n"},
		{"/think", "Reasoning mode: high\n"},
		{"/think sideways", `Unknown reasoning mode "sideways". Use one of: auto, off, on, low, medium, high`},
	}
	for _, tt := range tests {
		if out := captureStdoutForTest(t, func() { r.dispatch(tt.command) }); !strings.Contains(out, tt.want) {
			t.Errorf("%s lacks %q: %q", tt.command, tt.want, out)
		}
	}
	if r.asst.Think() != llm.ThinkHigh {
		t.Fatalf("think %q", r.asst.Think())
	}
}

// TestThinkReportsTheModeThatIsSent: a model without the thinking capability keeps auto, and /think
// says so rather than echoing the request.
func TestThinkReportsTheModeThatIsSent(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	out := captureStdoutForTest(t, func() { r.dispatch("/think high") })
	if !strings.Contains(out, "Reasoning mode set to auto.") || r.asst.Think() != llm.ThinkAuto {
		t.Fatalf("output %q, think %q", out, r.asst.Think())
	}
}
