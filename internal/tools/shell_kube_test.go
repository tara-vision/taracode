package tools

import (
	"os"
	"path/filepath"
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

// processKubeconfig puts taracode's own environment in one of the two states a shell kubectl can
// run in (pre-tag round F): KUBECONFIG exported, naming a file env.yaml, or not set at all. It
// returns the context the resolution then reports for a line that names no kubeconfig.
func processKubeconfig(t *testing.T, exported bool) string {
	t.Helper()
	t.Setenv("KUBECONFIG", "") // restores the caller's value after the test
	if !exported {
		if err := os.Unsetenv("KUBECONFIG"); err != nil {
			t.Fatal(err)
		}
		return "kind-dev"
	}
	t.Setenv("KUBECONFIG", writeKubeconfig(t, t.TempDir(), "env.yaml"))
	return "ctx-env.yaml"
}

// writeKubeconfig writes a small kubeconfig file named name into dir and returns its path.
func writeKubeconfig(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestShellRunKubectlAndHelmCarryTheirKubeTargets is ruling P2-R34: a kubectl or helm mutation run
// through the shell carries its kube targets, resolved like the kubectl tool's (the current context
// and namespace when none is named, from the kubeconfig the line names), with "*" when the line
// names more than one, so the protected contexts and namespaces deny it in operate mode. Both
// states of taracode's own KUBECONFIG are covered (pre-tag round F): a plain KUBECONFIG=
// assignment earlier on the line is exported by sh only when KUBECONFIG is already in the
// environment, and a conditional or a subshell can skip it, so it is never trusted: the context
// is "*" either way.
func TestShellRunKubectlAndHelmCarryTheirKubeTargets(t *testing.T) {
	for _, exported := range []bool{false, true} {
		name := map[bool]string{false: "KUBECONFIG unset", true: "KUBECONFIG exported"}[exported]
		t.Run(name, func(t *testing.T) { checkShellKubeTargets(t, exported) })
	}
}

func checkShellKubeTargets(t *testing.T, exported bool) {
	fakeBin(t, "kubectl", fakeKubeconfigKubectl)
	current := processKubeconfig(t, exported)
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	prod := writeKubeconfig(t, home, ".kube/prod.yaml")
	dev := writeKubeconfig(t, home, ".kube/dev.yaml")
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
		"KUBECONFIG=" + dev + "; kubectl delete pod web -n apps":             "protected.kube_contexts",
		"export KUBECONFIG=" + dev + " && kubectl delete pod web -n apps":    "protected.kube_contexts",
	}
	for command, rule := range denies {
		inv := classifyIn(command)
		if v := policy.Default().Evaluate(policy.ModeOperate, inv); v.Allow || v.Rule != rule {
			t.Errorf("%q: targets %+v: %+v, want %s", command, inv.Targets, v, rule)
		}
	}
	if inv := classifyIn("kubectl delete pod web"); inv.Targets.KubeContext != current || inv.Targets.KubeNamespace != "team-a" {
		t.Errorf("the current context and namespace apply: %+v, want %s", inv.Targets, current)
	}
	for command, want := range map[string]string{
		"KUBECONFIG=~/.kube/prod.yaml kubectl delete pod web": "ctx-prod.yaml",
		"env KUBECONFIG=" + dev + " kubectl delete pod web":   "ctx-dev.yaml",
		"kubectl delete pod web --kubeconfig " + prod:         "ctx-prod.yaml",
	} {
		if inv := classifyIn(command); inv.Targets.KubeContext != want {
			t.Errorf("%q: the kubeconfig the command names decides the current context: %+v, want %s",
				command, inv.Targets, want)
		}
	}
	for _, command := range []string{"kubectl delete pod web", "kubectl get pods -A > pods.txt",
		"KUBECONFIG=" + dev + " kubectl delete pod web -n apps"} {
		if v := policy.Default().Evaluate(policy.ModeOperate, classifyIn(command)); !v.Allow || v.Rule != "policy" {
			t.Errorf("%q goes on to the permission: %+v", command, v)
		}
	}
	if inv := classifyIn("kubectl get pods -n kube-system"); inv.Classification != policy.Read {
		t.Errorf("a kubectl read stays a read: %+v", inv)
	}
}

// TestThePreTagRoundLinesDenyUnderTheBuiltInPolicy evaluates the reviewer's lines of the pre-tag
// round through the shell tool and policy.Default() in operate mode: behind a shell keyword or
// grouping (B, kube targets and protected paths), after a context switch (C), with repeated or
// glued flags (D) and with HELM_NAMESPACE or HELM_KUBECONTEXT (E), each is a hard deny; the reads
// with global flags before the verb (A) stay reads.
func TestThePreTagRoundLinesDenyUnderTheBuiltInPolicy(t *testing.T) {
	fakeBin(t, "kubectl", fakeKubeconfigKubectl)
	processKubeconfig(t, false)
	dir := t.TempDir()
	shell := ShellTool(nil)
	evaluate := func(command string) (policy.Invocation, policy.Verdict) {
		inv := shell.Classify(map[string]any{"command": command}, dir)
		inv.WorkingDir = dir
		return inv, policy.Default().Evaluate(policy.ModeOperate, inv)
	}
	for command, rule := range map[string]string{
		"for p in a b; do kubectl delete pod $p -n kube-system; done":           "protected.kube_namespaces",
		"(kubectl -n kube-system delete pod x)":                                 "protected.kube_namespaces",
		"{ kubectl -n kube-system delete pod x; }":                              "protected.kube_namespaces",
		"if true; then kubectl -n kube-system delete pod x; fi":                 "protected.kube_namespaces",
		"! kubectl -n kube-system delete pod x":                                 "protected.kube_namespaces",
		"for f in a; do rm .git/config; done":                                   "protected.paths",
		"(rm .git/config)":                                                      "protected.paths",
		"{ rm .git/config; }":                                                   "protected.paths",
		"kubectl config use-context prod-eu && kubectl delete pod web":          "protected.kube_contexts",
		"kubens kube-system && kubectl delete pod coredns":                      "protected.kube_contexts",
		"kubectl delete pod x -n default -n kube-system":                        "protected.kube_namespaces",
		"kubectl delete pod x --context kind-dev --context prod-eu":             "protected.kube_contexts",
		"helm uninstall web -n default --namespace kube-system":                 "protected.kube_namespaces",
		"kubectl delete pod x -nkube-system":                                    "protected.kube_namespaces",
		"HELM_NAMESPACE=kube-system helm uninstall web --kube-context kind-dev": "protected.kube_namespaces",
		"env HELM_KUBECONTEXT=prod-eu helm uninstall web -n apps":               "protected.kube_contexts",
		"export HELM_NAMESPACE=kube-system && helm uninstall web":               "protected.kube_contexts",
	} {
		if inv, v := evaluate(command); v.Allow || v.Rule != rule {
			t.Errorf("%q: targets %+v: %+v, want %s", command, inv.Targets, v, rule)
		}
	}
	for _, command := range []string{"kubectl -n kube-system get pods", "kubectl --context prod-eu get pods",
		"helm -n kube-system list", "kubectl --context kind-dev -n kube-system get pods"} {
		if inv, v := evaluate(command); inv.Classification != policy.Read || !v.Allow || v.Rule != "read" {
			t.Errorf("%q must stay a read: %+v %+v", command, inv, v)
		}
	}
}
