package policy

import (
	"strings"
	"testing"
)

func mutate(tool, verb, command string, targets Targets) Invocation {
	return Invocation{Tool: tool, Verb: verb, Classification: Mutate, Reason: "test", Command: command, WorkingDir: "/w", Targets: targets}
}

func TestEvaluateOrder(t *testing.T) {
	p := Default()
	cases := []struct {
		name  string
		mode  Mode
		inv   Invocation
		allow bool
		rule  string
		dry   string
	}{
		{"read never asks", ModeInvestigate, Invocation{Tool: "kubectl", Classification: Read}, true, "read", ""},
		{"investigate denies mutate", ModeInvestigate, mutate("shell", "", "rm -rf build", Targets{}), false, "mode", ""},
		{"protected context", ModeOperate, mutate("kubectl", "delete", "kubectl delete pod x", Targets{KubeContext: "gke-prod-eu"}), false, "protected.kube_contexts", ""},
		{"protected namespace", ModeOperate, mutate("kubectl", "apply", "kubectl apply -f x", Targets{KubeNamespace: "kube-system"}), false, "protected.kube_namespaces", ""},
		{"protected path", ModeOperate, mutate("write_file", "", "", Targets{Paths: []string{"/w/infra/terraform.tfstate"}}), false, "protected.paths", ""},
		{"policy file is protected", ModeOperate, mutate("edit_file", "", "", Targets{Paths: []string{"/w/.taracode/policy.yaml"}}), false, "protected.paths", ""},
		{"deny pattern", ModeOperate, mutate("terraform", "destroy", "terraform destroy -auto-approve", Targets{}), false, "deny.commands", ""},
		{"deny pattern is case-insensitive and whitespace-tolerant", ModeOperate, mutate("shell", "", "kubectl  delete   namespace  Foo", Targets{}), false, "deny.commands", ""},
		{"kubectl apply needs a dry run", ModeOperate, mutate("kubectl", "apply", "kubectl apply -f x", Targets{KubeContext: "dev", KubeNamespace: "apps"}), true, "policy", "kubectl_apply"},
		{"terraform apply needs a plan", ModeOperate, mutate("terraform", "apply", "terraform apply", Targets{Paths: []string{"/w/infra"}}), true, "policy", "terraform_apply"},
		{"helm install dry-runs", ModeOperate, mutate("helm", "install", "helm install x y", Targets{}), true, "policy", "helm_upgrade"},
		{"plain mutation passes to permissions", ModeOperate, mutate("shell", "", "make deploy", Targets{}), true, "policy", ""},
	}
	pol := p
	pol.Protected.Paths = append(pol.Protected.Paths, "/w/.taracode/policy.yaml")
	for _, c := range cases {
		v := pol.Evaluate(c.mode, c.inv)
		if v.Allow != c.allow || v.Rule != c.rule || v.DryRun != c.dry {
			t.Errorf("%s: got %+v", c.name, v)
		}
		if !v.Allow && v.Reason == "" {
			t.Errorf("%s: a denial needs a reason", c.name)
		}
	}
}

func TestEvaluateCloudAccountsAndHostsMatchCommandTokens(t *testing.T) {
	p := Default()
	p.Protected.CloudAccounts = []string{"*123456789012*", "prod-project"}
	p.Protected.Hosts = []string{"*.bank.internal"}
	if v := p.Evaluate(ModeOperate, mutate("cloud", "put", "aws s3api put-object --bucket arn:aws:s3::123456789012:x", Targets{})); v.Allow || v.Rule != "protected.cloud_accounts" {
		t.Errorf("account in command: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, mutate("cloud", "delete", "gcloud compute instances delete vm", Targets{CloudAccount: "prod-project"})); v.Allow {
		t.Errorf("account target: %+v", v)
	}
	if v := p.Evaluate(ModeOperate, mutate("shell", "", "ssh core.bank.internal reboot", Targets{Hosts: []string{"core.bank.internal"}})); v.Allow || v.Rule != "protected.hosts" {
		t.Errorf("host: %+v", v)
	}
}

func TestDryRunCanBeSwitchedOffPerKind(t *testing.T) {
	p := Default()
	f := false
	p.RequireDryRun.KubectlApply = &f
	v := p.Evaluate(ModeOperate, mutate("kubectl", "apply", "kubectl apply -f x", Targets{KubeContext: "dev"}))
	if !v.Allow || v.DryRun != "" {
		t.Fatalf("%+v", v)
	}
	if !strings.Contains(Default().Evaluate(ModeInvestigate, mutate("git", "push", "git push", Targets{})).Reason, "/mode operate") {
		t.Fatal("the investigate denial must tell the model how the user switches modes")
	}
}

// TestAllNamespacesMutationHitsProtectedNamespaces: -A maps to the namespace "*", which no concrete
// protected pattern matches; a mutation of every namespace touches the protected ones too (final
// review I2).
func TestAllNamespacesMutationHitsProtectedNamespaces(t *testing.T) {
	inv := mutate("kubectl", "delete", "kubectl delete pods --all -A", Targets{KubeContext: "kind-dev", KubeNamespace: "*"})
	v := Default().Evaluate(ModeOperate, inv)
	if v.Allow || v.Rule != "protected.kube_namespaces" || !strings.Contains(v.Reason, "every namespace") ||
		!strings.Contains(v.Reason, "kube-system") {
		t.Fatalf("an all-namespace mutation must hit the protected namespaces: %+v", v)
	}
	open := Default()
	open.Protected.KubeNamespaces = nil
	if v := open.Evaluate(ModeOperate, inv); !v.Allow || v.Rule != "policy" {
		t.Fatalf("without protected namespaces an all-namespace mutation goes on to the permission: %+v", v)
	}
}

// TestBuiltInPolicyDeniesCommandsNamingThePolicyFile: belt and braces for R6 (final review I1): a
// mutating command that names a policy file is refused by the built-in deny patterns, even when its
// path is built so the protected paths cannot see it.
func TestBuiltInPolicyDeniesCommandsNamingThePolicyFile(t *testing.T) {
	for _, command := range []string{
		"echo x > .taracode/policy.yaml", "sed -i 's/investigate/operate/' ~/.taracode/policy.yaml",
		"rm $HOME/.taracode/policy.yaml", "cp /tmp/p.yaml ./.taracode/policy.yaml",
	} {
		v := Default().Evaluate(ModeOperate, mutate("shell", "", command, Targets{}))
		if v.Allow || v.Rule != "deny.commands" {
			t.Errorf("%q: %+v", command, v)
		}
	}
	if v := Default().Evaluate(ModeOperate, mutate("shell", "", "echo x > notes/policy.yaml", Targets{})); !v.Allow {
		t.Errorf("another policy.yaml is not the policy file: %+v", v)
	}
}

// TestAStarKubeTargetDenyNamesItsCauseAndRemedy (pre-tag round 2, item 5): a "*" kube target denial
// names why taracode could not pin it down (the cause the classifier recorded) and how to avoid it,
// so a model does not retry the same line. Two causes are checked, on the context and the namespace.
func TestAStarKubeTargetDenyNamesItsCauseAndRemedy(t *testing.T) {
	remedy := "run kubectl or helm as its own command with a literal --context and -n, or use the"
	ctxDeny := Default().Evaluate(ModeOperate, mutate("shell", "", "kubectl config use-context prod-eu && kubectl delete pod web",
		Targets{KubeContext: "*", KubeNamespace: "apps", KubeReason: "the context is switched earlier on the line"}))
	if ctxDeny.Allow || ctxDeny.Rule != "protected.kube_contexts" ||
		!strings.Contains(ctxDeny.Reason, "the context is switched earlier on the line") ||
		!strings.Contains(ctxDeny.Reason, remedy) {
		t.Fatalf("a context-switch * deny must name the cause and the remedy: %+v", ctxDeny)
	}
	nsDeny := Default().Evaluate(ModeOperate, mutate("shell", "", "kubectl delete pod web -n default -n kube-system",
		Targets{KubeNamespace: "*", KubeReason: "conflicting --context, -n or --kubeconfig values"}))
	if nsDeny.Allow || nsDeny.Rule != "protected.kube_namespaces" ||
		!strings.Contains(nsDeny.Reason, "conflicting --context, -n or --kubeconfig values") ||
		!strings.Contains(nsDeny.Reason, remedy) {
		t.Fatalf("a conflicting-flags * deny must name the cause and the remedy: %+v", nsDeny)
	}
	// A "*" with no recorded cause still names the remedy, with no empty parentheses.
	plain := Default().Evaluate(ModeOperate, mutate("shell", "", "kubectl delete pod web",
		Targets{KubeContext: "*", KubeNamespace: "apps"}))
	if !strings.Contains(plain.Reason, remedy) || strings.Contains(plain.Reason, "()") {
		t.Fatalf("a * deny with no cause still names the remedy without empty parentheses: %+v", plain)
	}
}

// TestANamespaceObjectStarDenyNamesItsOwnRemedy (round 2, item 3): a "*" namespace that a command's
// namespace objects make (CauseNamespaceObjects) is fixed by naming one namespace object without a -n
// that names another, not by a literal -n or the kubectl tool, so its deny names that remedy. A "*"
// context keeps the -n remedy even with that cause, and so does a "*" namespace with any other cause.
func TestANamespaceObjectStarDenyNamesItsOwnRemedy(t *testing.T) {
	const objectRemedy = "; name a single namespace object, or drop -n"
	v := Default().Evaluate(ModeOperate, mutate("kubectl", "label", "kubectl label ns - kube-system team=x",
		Targets{KubeContext: "kind-dev", KubeNamespace: "*", KubeReason: CauseNamespaceObjects}))
	if v.Allow || v.Rule != "protected.kube_namespaces" || !strings.HasSuffix(v.Reason, objectRemedy) ||
		!strings.Contains(v.Reason, "("+CauseNamespaceObjects+")") || strings.Contains(v.Reason, "literal --context") {
		t.Fatalf("a namespace-object * deny names its cause and its own remedy: %+v", v)
	}
	ctx := Default().Evaluate(ModeOperate, mutate("kubectl", "label", "kubectl label ns - kube-system team=x",
		Targets{KubeContext: "*", KubeNamespace: "*", KubeReason: CauseNamespaceObjects}))
	if ctx.Rule != "protected.kube_contexts" || !strings.Contains(ctx.Reason, "literal --context and -n") {
		t.Fatalf("a * context keeps the -n remedy: %+v", ctx)
	}
	other := Default().Evaluate(ModeOperate, mutate("shell", "", "kubectl delete pod web -n default -n kube-system",
		Targets{KubeContext: "kind-dev", KubeNamespace: "*", KubeReason: "conflicting --context, -n or --kubeconfig values"}))
	if !strings.Contains(other.Reason, "literal --context and -n") || strings.Contains(other.Reason, objectRemedy) {
		t.Fatalf("another cause keeps the -n remedy: %+v", other)
	}
	// Round 3 (M3): a word the shell expands is not fixed by a literal -n; the kubectl tool, which runs no
	// shell, reads the same words literally.
	expanded := Default().Evaluate(ModeOperate, mutate("shell", "", `kubectl patch ns shop -p '{"a":1,"b":2}'`,
		Targets{KubeContext: "kind-dev", KubeNamespace: "*", KubeReason: CauseExpandedObjects}))
	if expanded.Rule != "protected.kube_namespaces" || !strings.HasSuffix(expanded.Reason,
		"("+CauseExpandedObjects+"), including the protected kube-system; use the kubectl tool, which runs no shell") {
		t.Fatalf("an expanded word names the kubectl tool alone: %+v", expanded)
	}
}

// TestSeveralContextsHitProtectedContexts: a shell line that names more than one kube context
// reports "*", which touches the protected contexts too (ruling P2-R34).
func TestSeveralContextsHitProtectedContexts(t *testing.T) {
	inv := mutate("shell", "", "kubectl --context a delete pod x; kubectl --context b delete pod y",
		Targets{KubeContext: "*", KubeNamespace: "apps"})
	if v := Default().Evaluate(ModeOperate, inv); v.Allow || v.Rule != "protected.kube_contexts" ||
		!strings.Contains(v.Reason, "*prod*") {
		t.Fatalf("%+v", v)
	}
	open := Default()
	open.Protected.KubeContexts = nil
	if v := open.Evaluate(ModeOperate, inv); !v.Allow {
		t.Fatalf("without protected contexts the call goes on to the permission: %+v", v)
	}
}
