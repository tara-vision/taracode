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
