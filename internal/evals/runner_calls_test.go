package evals

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/agent"
)

func TestTranscriptMarksARealCall(t *testing.T) {
	var b strings.Builder
	writeCalls(&b, []loggedCall{
		{event: agent.ToolEvent{Tool: "shell", Allowed: true, Args: map[string]any{"command": "pwd"}},
			replayed: []ReplayCall{{Signature: "shell pwd", Real: true}}},
		{event: agent.ToolEvent{Tool: "shell", Allowed: true, Args: map[string]any{"command": "uname -a"}},
			replayed: []ReplayCall{{Signature: "shell uname -a", Miss: true, Err: ErrNoFixture}}},
	})
	out := b.String()
	if !strings.Contains(out, "#1 shell allowed shell pwd  REAL shell pwd") ||
		!strings.Contains(out, "#2 shell allowed shell uname -a  MISS shell uname -a") {
		t.Fatalf("transcript:\n%s", out)
	}
}
