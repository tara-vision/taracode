package tools

import (
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// fakeKubeconfigKubectl answers the target resolution: the current context is kind-dev, or
// ctx-<file> when KUBECONFIG names a file, and the default namespace is team-a.
const fakeKubeconfigKubectl = `#!/bin/sh
if [ "$1 $2" = "config current-context" ]; then
  if [ -n "$KUBECONFIG" ]; then echo "ctx-$(basename "$KUBECONFIG")"; else echo kind-dev; fi
  exit 0
fi
if [ "$1 $2" = "config view" ]; then echo team-a; exit 0; fi
echo "kubectl $@"
`

// TestShellRunKubectlAndHelmCarryTheirKubeTargets is ruling P2-R34: a kubectl or helm mutation run
// through the shell carries its kube targets, resolved like the kubectl tool's (the current context
// and namespace when none is named, from the kubeconfig the line names), with "*" when the line
// names more than one, so the protected contexts and namespaces deny it in operate mode.
func TestShellRunKubectlAndHelmCarryTheirKubeTargets(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubeconfigKubectl)
	t.Setenv("KUBECONFIG", "")
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	shell := ShellTool(nil)
	classifyIn := func(command string) policy.Invocation {
		inv := shell.Classify(map[string]any{"command": command}, dir)
		inv.WorkingDir = dir
		return inv
	}
	denies := map[string]string{
		"kubectl delete pods --all -A":                                       "protected.kube_namespaces",
		"kubectl -n kube-system delete pod x":                                "protected.kube_namespaces",
		"kubectl --context prod-eu delete pod web":                           "protected.kube_contexts",
		"helm uninstall web -n kube-system":                                  "protected.kube_namespaces",
		"helm uninstall web --kube-context prod-eu":                          "protected.kube_contexts",
		"helm uninstall web -A":                                              "protected.kube_namespaces",
		"kubectl delete pod a -n x; kubectl delete pod b -n y":               "protected.kube_namespaces",
		"kubectl --context a delete pod x; kubectl --context b delete pod y": "protected.kube_contexts",
		"KUBECONFIG=~/.kube/prod.yaml kubectl delete pod web":                "protected.kube_contexts",
		"kubectl --context $CTX delete pod web":                              "protected.kube_contexts",
		"KUBECONFIG=$KC kubectl delete pod web":                              "protected.kube_contexts",
	}
	for command, rule := range denies {
		inv := classifyIn(command)
		if v := policy.Default().Evaluate(policy.ModeOperate, inv); v.Allow || v.Rule != rule {
			t.Errorf("%q: targets %+v: %+v, want %s", command, inv.Targets, v, rule)
		}
	}
	if inv := classifyIn("kubectl delete pod web"); inv.Targets.KubeContext != "kind-dev" || inv.Targets.KubeNamespace != "team-a" {
		t.Errorf("the current context and namespace apply: %+v", inv.Targets)
	}
	if inv := classifyIn("KUBECONFIG=~/.kube/prod.yaml kubectl delete pod web"); inv.Targets.KubeContext != "ctx-prod.yaml" {
		t.Errorf("the named kubeconfig decides the current context: %+v", inv.Targets)
	}
	for _, command := range []string{"kubectl delete pod web", "kubectl get pods -A > pods.txt"} {
		if v := policy.Default().Evaluate(policy.ModeOperate, classifyIn(command)); !v.Allow || v.Rule != "policy" {
			t.Errorf("%q goes on to the permission: %+v", command, v)
		}
	}
	if inv := classifyIn("kubectl get pods -n kube-system"); inv.Classification != policy.Read {
		t.Errorf("a kubectl read stays a read: %+v", inv)
	}
}
