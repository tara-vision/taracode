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

// TestHelmDryRunIsAlwaysARealDryRun: the dry run the policy requires before the prompt must never
// run the release: a --dry-run=false or =none the model wrote is replaced, and --dry-run goes before
// a "--" that would make it a release name.
func TestHelmDryRunIsAlwaysARealDryRun(t *testing.T) {
	fakeBin(t, "helm", "")
	tool := HelmTool()
	for args, want := range map[string]string{
		"upgrade web ./chart --dry-run=false": "helm upgrade web ./chart --dry-run",
		"upgrade web ./chart --dry-run=none":  "helm upgrade web ./chart --dry-run",
		"install web ./chart --dry-run":       "helm install web ./chart --dry-run",
		"upgrade web ./chart -- extra":        "helm upgrade web ./chart --dry-run -- extra",
	} {
		out, err := tool.DryRun(context.Background(), map[string]any{"args": args}, "")
		if err != nil || out != want {
			t.Errorf("%q: dry run ran %q (%v), want %q", args, out, err, want)
		}
	}
}
