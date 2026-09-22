package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestGitToolTokenizesArgsAndClassifies(t *testing.T) {
	fakeBin(t, "git", "")
	tool := GitTool()
	out, err := tool.Run(context.Background(), map[string]any{"args": `log --format='%h %s' -n 3`}, t.TempDir())
	if err != nil || !strings.Contains(out, "git log --format=%h %s -n 3") {
		t.Fatalf("%q %v", out, err)
	}
	if inv := tool.Classify(map[string]any{"args": "status"}, "/w"); inv.Classification != policy.Read || inv.Command != "git status" {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"args": "push origin main"}, "/w"); inv.Classification != policy.Mutate || inv.Verb != "push" {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"args": `commit -m "unterminated`}, "/w"); inv.Classification != policy.Mutate {
		t.Errorf("unparseable args are mutate: %+v", inv)
	}
}
