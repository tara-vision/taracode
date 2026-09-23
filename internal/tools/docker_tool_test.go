package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestDockerToolAddsNoStreamToStats(t *testing.T) {
	fakeBin(t, "docker", "")
	tool := DockerTool()
	out, err := tool.Run(context.Background(), map[string]any{"args": "stats"}, "")
	if err != nil || !strings.Contains(out, "docker stats --no-stream") {
		t.Fatalf("%q %v", out, err)
	}
	if inv := tool.Classify(map[string]any{"args": "compose up -d"}, "/w"); inv.Classification != policy.Mutate || inv.Command != "docker compose up -d" {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"args": "ps -a"}, "/w"); inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
}

// TestDockerToolAddsNoStreamToStatsAfterGlobalFlags (pre-tag round 2, item 7): docker stats streams
// until the command timeout, so --no-stream is added whenever the verb is stats even behind a global
// flag such as --context; a non-stats verb behind a global flag is left alone.
func TestDockerToolAddsNoStreamToStatsAfterGlobalFlags(t *testing.T) {
	fakeBin(t, "docker", "")
	tool := DockerTool()
	out, err := tool.Run(context.Background(), map[string]any{"args": "--context x stats"}, "")
	if err != nil || !strings.Contains(out, "docker --context x stats --no-stream") {
		t.Fatalf("stats after a global flag must get --no-stream: %q %v", out, err)
	}
	out, err = tool.Run(context.Background(), map[string]any{"args": "--context x ps"}, "")
	if err != nil || strings.Contains(out, "--no-stream") {
		t.Fatalf("a non-stats verb must not get --no-stream: %q %v", out, err)
	}
}
