package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestShellRunsInTheWorkingDirectoryAndStreams(t *testing.T) {
	var stream bytes.Buffer
	tool := ShellTool(&stream)
	dir := t.TempDir()
	out, err := tool.Run(context.Background(), map[string]any{"command": "pwd && echo hi && echo err 1>&2"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "hi") || !strings.Contains(out, "err") {
		t.Errorf("output %q", out)
	}
	if !strings.Contains(stream.String(), "hi") {
		t.Errorf("stream %q", stream.String())
	}
	out, err = tool.Run(context.Background(), map[string]any{"command": "exit 3"}, dir)
	if err != nil || !strings.Contains(out, "[exit status 3]") {
		t.Errorf("non-zero exit is reported in the text: %q %v", out, err)
	}
	_, err = tool.Run(context.Background(), map[string]any{"command": "sleep 5", "timeout": 1.0}, dir)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: %v", err)
	}
}

func TestShellClassifiesThroughTheShellClassifier(t *testing.T) {
	tool := ShellTool(nil)
	inv := tool.Classify(map[string]any{"command": "ls -la | head"}, "/w")
	if inv.Classification != policy.Read || inv.Command != "ls -la | head" {
		t.Errorf("%+v", inv)
	}
	inv = tool.Classify(map[string]any{"command": "curl https://api.example.com/x > out.json"}, "/w")
	if inv.Classification != policy.Mutate || !strings.Contains(inv.Reason, "redirect") {
		t.Errorf("%+v", inv)
	}
	inv = tool.Classify(map[string]any{"command": "dig api.example.com"}, "/w")
	if len(inv.Targets.Hosts) != 1 {
		t.Errorf("hosts %+v", inv.Targets)
	}
	if _, err := tool.Run(context.Background(), map[string]any{}, "/tmp"); err == nil {
		t.Error("command is required")
	}
}
