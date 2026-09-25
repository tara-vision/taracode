package policy

import (
	"strings"
	"testing"
)

// TestDenyPatternsMatchTheCanonicalCommand pins ruling P3-R69 item 2: a deny pattern matches the
// command as written and its canonical form, where kubectl resource aliases are spelled out and
// resource/name is spelled resource name; a pattern written with an alias is read the same way. The
// raw match stays, so every existing pattern keeps working.
func TestDenyPatternsMatchTheCanonicalCommand(t *testing.T) {
	denied := []string{
		"kubectl delete namespace shop",
		"kubectl delete ns shop",
		"kubectl delete namespaces shop",
		"kubectl delete namespace/shop",
		"kubectl delete ns/shop",
		"kubectl delete NS/Shop",
		"kubectl  delete   ns   shop --wait=false",
		"kubectl delete ns/a ns/b",
		"/usr/local/bin/kubectl delete ns shop",
	}
	for _, command := range denied {
		v := Default().Evaluate(ModeOperate, mutate("shell", "", command, Targets{KubeContext: "dev", KubeNamespace: "shop"}))
		if v.Allow || v.Rule != "deny.commands" || !strings.Contains(v.Reason, "kubectl delete namespace *") {
			t.Errorf("%q: %+v, want the namespace deny pattern", command, v)
		}
	}
	allowed := []string{
		"kubectl delete pod ns",
		"kubectl delete deploy/namespace",
		"kubectl get ns shop",
		"helm uninstall ns",
	}
	for _, command := range allowed {
		v := Default().Evaluate(ModeOperate, mutate("shell", "", command, Targets{KubeContext: "dev", KubeNamespace: "shop"}))
		if !v.Allow {
			t.Errorf("%q: %+v, want no deny pattern", command, v)
		}
	}
	aliased := Policy{Deny: DenyRules{Commands: []string{"kubectl delete deploy *", "kubectl scale sts/db *"}}}
	for _, command := range []string{"kubectl delete deployment/web", "kubectl delete deployments web",
		"kubectl delete deploy web", "kubectl scale statefulset db --replicas=0"} {
		if v := aliased.Evaluate(ModeOperate, mutate("kubectl", "", command, Targets{})); v.Allow || v.Rule != "deny.commands" {
			t.Errorf("%q: %+v, want the aliased pattern to deny", command, v)
		}
	}
	v := Default().Evaluate(ModeOperate, mutate("kubectl", "delete", "kubectl delete ns/shop", Targets{KubeContext: "dev"}))
	if !strings.Contains(v.Reason, `"kubectl delete ns/shop"`) || !strings.Contains(v.Reason, `"kubectl delete namespace shop"`) {
		t.Errorf("the reason names the command as written and as read: %q", v.Reason)
	}
}

// TestCanonicalKubeResourceIsTheSharedAliasTable: the table the kubectl tool, the classifier, the eval
// signature and the deny patterns read resource words with.
func TestCanonicalKubeResourceIsTheSharedAliasTable(t *testing.T) {
	for word, want := range map[string]string{"ns": "namespace", "Namespaces": "namespace", "namespace": "namespace",
		"po": "pod", "deploy": "deployment", "sts": "statefulset", "certificates": "certificates"} {
		if got := CanonicalKubeResource(word); got != want {
			t.Errorf("CanonicalKubeResource(%q) = %q, want %q", word, got, want)
		}
	}
	kinds := KubeResourceKinds()
	if len(kinds) == 0 || !strings.Contains(strings.Join(kinds, ","), "namespace") {
		t.Errorf("kinds %v", kinds)
	}
	kinds[0] = "changed"
	if KubeResourceKinds()[0] == "changed" {
		t.Error("KubeResourceKinds must return a copy")
	}
}
