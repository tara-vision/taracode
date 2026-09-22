package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestHelmToolDryRunsUpgrades(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectl)
	fakeBin(t, "helm", "")
	tool := HelmTool()
	inv := tool.Classify(map[string]any{"args": "upgrade web ./chart -n apps --kube-context staging"}, "/w")
	if inv.Classification != policy.Mutate || inv.Verb != "upgrade" || inv.Targets.KubeContext != "staging" || inv.Targets.KubeNamespace != "apps" {
		t.Errorf("%+v", inv)
	}
	inv = tool.Classify(map[string]any{"args": "list -A"}, "/w")
	if inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
	dry, err := tool.DryRun(context.Background(), map[string]any{"args": "upgrade web ./chart"}, "")
	if err != nil || !strings.Contains(dry, "helm upgrade web ./chart --dry-run") {
		t.Errorf("%q %v", dry, err)
	}
	if _, err := tool.DryRun(context.Background(), map[string]any{"args": "uninstall web"}, ""); err != ErrNoDryRun {
		t.Errorf("%v", err)
	}
	out, err := tool.Run(context.Background(), map[string]any{"args": "status web"}, "")
	if err != nil || !strings.Contains(out, "helm status web") {
		t.Errorf("%q %v", out, err)
	}
}
