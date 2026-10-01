package evals

import (
	"strings"
	"testing"
)

func TestWordsFallsBackToFieldsOnAnUnbalancedQuote(t *testing.T) {
	if got := words(`-l 'app=web`); strings.Join(got, "|") != `-l|'app=web` {
		t.Fatalf("%q", got)
	}
}

// TestShellKubectlFlagsBeforeTheVerbKeyLikeTheTool: whatever flags come before the verb, a shell
// kubectl line keys like the kubectl tool's call for the same command (ruling R11).
func TestShellKubectlFlagsBeforeTheVerbKeyLikeTheTool(t *testing.T) {
	tool := Signature("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"})
	for _, command := range []string{
		"kubectl -n=shop get pods",
		"kubectl -nshop get pods",
		"kubectl get pods -nshop",
		"kubectl --namespace shop get pods",
	} {
		if got := Signature("shell", map[string]any{"command": command}); got != tool {
			t.Errorf("%q keys %q, the tool keys %q", command, got, tool)
		}
	}
	got := Signature("shell", map[string]any{"command": "kubectl --insecure-skip-tls-verify get pods -n shop"})
	if got != "kubectl get pod -n shop --insecure-skip-tls-verify" {
		t.Errorf("a flag without a value before the verb: %q", got)
	}
}

// TestShellKubectlWithoutAVerb: a kubectl line with flags only keys its words as written.
func TestShellKubectlWithoutAVerb(t *testing.T) {
	if got := Signature("shell", map[string]any{"command": "kubectl -n shop"}); got != "kubectl -n shop" {
		t.Fatalf("%q", got)
	}
}

// TestKubectlCallTheToolRefusesKeysItsParameters: arguments the kubectl tool refuses still key every
// parameter given, the output format included.
func TestKubectlCallTheToolRefusesKeysItsParameters(t *testing.T) {
	got := Signature("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop", "context": "prod",
		"output": "wide", "args": `-l 'app=web`})
	if got != "kubectl get pod -n shop --context prod -o wide -l 'app=web" {
		t.Fatalf("%q", got)
	}
}
