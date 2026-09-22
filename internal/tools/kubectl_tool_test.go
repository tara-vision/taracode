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
if [ "$1" = "diff" ]; then echo "+ replicas: 3"; exit 1; fi
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
}
