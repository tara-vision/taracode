package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

const fakeKubectl = `#!/bin/sh
case "$1 $2" in
  "config current-context") echo "kind-dev"; exit 0;;
  "config view") echo "team-a"; exit 0;;
esac
if [ "$1" = "diff" ]; then echo "+ replicas: 3 ($@)"; exit 1; fi
echo "kubectl $@"
`

// fakeKubectlDiffExit130 makes "diff" exit 130 (an unrelated failure, not "differences found") so
// tests can tell the anchored exit-status check apart from an unanchored substring match: the string
// "exited with status 1" is itself a substring of "exited with status 130".
const fakeKubectlDiffExit130 = `#!/bin/sh
if [ "$1" = "diff" ]; then echo "kubectl: error: unknown flag"; exit 130; fi
echo "kubectl $@"
`

func TestKubectlBuildsArgvAndResolvesTargets(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectl)
	tool := KubectlTool()
	out, err := tool.Run(context.Background(), map[string]any{"verb": "get", "resource": "pods", "namespace": "apps", "output": "wide", "args": "-l app=web"}, "")
	if err != nil || !strings.Contains(out, "kubectl get pods -n apps -o wide -l app=web") {
		t.Fatalf("%q %v", out, err)
	}
	inv := tool.Classify(map[string]any{"verb": "get", "resource": "pods"}, "/w")
	if inv.Classification != policy.Read || inv.Targets.KubeContext != "" {
		t.Errorf("reads do not resolve the context: %+v", inv)
	}
	inv = tool.Classify(map[string]any{"verb": "delete", "resource": "pod", "name": "x"}, "/w")
	if inv.Classification != policy.Mutate || inv.Targets.KubeContext != "kind-dev" || inv.Targets.KubeNamespace != "team-a" {
		t.Errorf("mutations resolve the current context and namespace: %+v", inv)
	}
	inv = tool.Classify(map[string]any{"verb": "apply", "args": "-f x.yaml --context prod-eu -n kube-system"}, "/w")
	if inv.Verb != "apply" || inv.Targets.KubeContext != "prod-eu" || inv.Targets.KubeNamespace != "kube-system" || inv.Command != "kubectl apply -f x.yaml --context prod-eu -n kube-system" {
		t.Errorf("%+v", inv)
	}
	dry, err := tool.DryRun(context.Background(), map[string]any{"verb": "apply", "args": "-f x.yaml"}, "")
	if err != nil || !strings.Contains(dry, "replicas: 3") {
		t.Errorf("kubectl diff exit 1 means differences: %q %v", dry, err)
	}
	if _, err := tool.DryRun(context.Background(), map[string]any{"verb": "delete", "resource": "pod", "name": "x"}, ""); err != ErrNoDryRun {
		t.Errorf("only apply has a dry run: %v", err)
	}
	dry, err = tool.DryRun(context.Background(), map[string]any{"verb": "apply", "args": "-f x.yaml", "output": "yaml"}, "")
	if err != nil || strings.Contains(dry, "-o yaml") || !strings.Contains(dry, "-f x.yaml") {
		t.Errorf("the output parameter must not reach kubectl diff: %q %v", dry, err)
	}
}

// TestKubectlDiffDryRunOnlyTreatsExitStatusOneAsDifferences guards the exit-status check: it must be
// anchored to exactly "kubectl exited with status 1\n", not an unanchored substring, or exit codes
// like 130 (which contains "1" as its first digit) would be misread as "differences found".
func TestKubectlDiffDryRunOnlyTreatsExitStatusOneAsDifferences(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectlDiffExit130)
	tool := KubectlTool()
	dry, err := tool.DryRun(context.Background(), map[string]any{"verb": "apply", "args": "-f x.yaml"}, "")
	if err == nil || !strings.Contains(err.Error(), "130") {
		t.Errorf("exit status 130 must be reported as an error, not read as differences found: %q %v", dry, err)
	}
}

// TestKubectlRefusesFlagsGivenBothAsParameterAndInArgs covers namespace, context and output: each is
// refused, not silently resolved one way, when the same flag also appears (in any spelling) in args.
func TestKubectlRefusesFlagsGivenBothAsParameterAndInArgs(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubectl)
	tool := KubectlTool()
	cases := []struct {
		name string
		args map[string]any
	}{
		{"namespace", map[string]any{"verb": "apply", "namespace": "sandbox", "args": "-f x.yaml --namespace kube-system"}},
		{"context", map[string]any{"verb": "apply", "context": "dev", "args": "-f x.yaml --context prod"}},
		{"output", map[string]any{"verb": "get", "resource": "pods", "output": "json", "args": "-o yaml"}},
	}
	for _, c := range cases {
		inv := tool.Classify(c.args, "/w")
		if inv.Classification != policy.Mutate || !strings.Contains(inv.Reason, "both") {
			t.Errorf("%s: a flag given both ways must be refused: %+v", c.name, inv)
		}
		if _, err := tool.Run(context.Background(), c.args, ""); err == nil || !strings.Contains(err.Error(), "both") {
			t.Errorf("%s: Run must refuse the same duplicated flag: %v", c.name, err)
		}
	}
}
