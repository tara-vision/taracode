package classify

import (
	"fmt"
	"strings"
	"testing"
)

// TestANamespaceObjectIsTheNamespaceTarget pins ruling P3-R69 item 1: a kubectl verb that changes
// objects (delete, edit, patch, replace, apply, annotate, label, create) and names a namespace object
// in any spelling targets that namespace, so the protected namespaces cover kubectl delete ns
// kube-system. A -n that names another namespace, several namespace objects, a selection (--all, -l,
// --field-selector), a mix with objects of other kinds, or an option the classifier does not know
// before the objects make the target "*". A read, and an object of another kind, keep the -n target.
func TestANamespaceObjectIsTheNamespaceTarget(t *testing.T) {
	cases := []struct{ args, ctx, ns string }{
		{"delete ns kube-system", "", "kube-system"},
		{"delete namespace kube-system", "", "kube-system"},
		{"delete namespaces kube-system", "", "kube-system"},
		{"delete namespace/kube-system", "", "kube-system"},
		{"delete ns/kube-system", "", "kube-system"},
		{"delete NS kube-system", "", "kube-system"},
		{"delete Namespace/kube-system", "", "kube-system"},
		{"delete namespaces.v1 kube-system", "", "kube-system"},
		{"delete ns kube-system -n kube-system", "", "kube-system"},
		{"delete ns kube-system --context prod-eu", "prod-eu", "kube-system"},
		{"delete ns kube-system -n default", "", "*"},
		{"-n default delete ns kube-system", "", "*"},
		{"delete ns a b", "", "*"},
		{"delete ns kube-system kube-system", "", "kube-system"},
		{"delete ns/a ns/b", "", "*"},
		{"delete ns --all", "", "*"},
		{"delete ns -l team=x", "", "*"},
		{"delete ns --selector=team=x", "", "*"},
		{"delete ns --field-selector metadata.name=kube-system", "", "*"},
		{"delete ns/foo pod/bar", "", "*"},
		{"delete ns,pod foo", "", "*"},
		{"delete --grace-period 0 ns kube-system", "", "kube-system"},
		{"delete -R ns kube-system --wait=false --now", "", "kube-system"},
		{"delete --weird pod ns kube-system", "", "*"},
		{"delete -Z pod ns kube-system", "", "*"},
		{"--weird x delete ns kube-system", "", "*"},
		{"delete pod ns -n shop", "", "shop"},
		{"delete pod x -n kube-system", "", "kube-system"},
		{"label ns kube-system team=x", "", "kube-system"},
		{"annotate ns kube-system note-", "", "kube-system"},
		{"patch ns kube-system -p {}", "", "kube-system"},
		{"patch ns kube-system --type merge -p {}", "", "kube-system"},
		{"edit namespace/kube-system", "", "kube-system"},
		{"create namespace shop", "", "shop"},
		{"create ns shop -o yaml", "", "shop"},
		{"apply edit-last-applied ns kube-system", "", "kube-system"},
		{"replace -f ns.yaml", "", ""},
		{"get ns kube-system", "", ""},
		{"describe namespace/kube-system", "", ""},
		{"delete ns", "", ""},
		// After a lone "--" pflag reads no more options, and the words stay objects of the command.
		{"delete ns kube-system -- extra", "", "*"},
		{"delete -- ns kube-system", "", "kube-system"},
		{"delete ns -- kube-system", "", "kube-system"},
		{"delete ns -- -n kube-system", "", "*"},
		// A raw URI can name any namespace, the namespace object included.
		{"delete --raw /api/v1/namespaces/kube-system", "", "*"},
		{"delete --raw=/api/v1/namespaces/kube-system/pods/x", "", "*"},
		{"create --raw /api/v1/namespaces -f ns.json", "", "*"},
		{"get --raw /api/v1/namespaces/kube-system", "", ""},
		{"delete ns/kube-system/extra", "", "*"},
		{"delete --cascade ns kube-system", "", "kube-system"},
		// Round 2, item 1: label and annotate split their changes off the objects the way kubectl does
		// (GetResourcesAndPairs): a change is KEY=VALUE with the "=" not first, or KEY- other than a lone
		// "-". Every word before the first change is an object, so "-" and "=a" are names and the
		// kube-system after them is changed too.
		{"label ns - kube-system team=x", "", "*"},
		{"annotate ns =a kube-system note=x", "", "*"},
		{"label ns -- - kube-system a=b", "", "*"},
		{"label ns kube-system team- note=x", "", "kube-system"},
		{"label ns team=x", "", ""},
		{"label -f ns.yaml team=x", "", ""},
		// Round 2, item 2: in the TYPE/NAME form the changes are not objects of another kind.
		{"label ns/shop team=x", "", "shop"},
		{"annotate namespace/shop note=x", "", "shop"},
		{"annotate ns/shop link=https://example.com/a/b", "", "shop"},
		{"label ns/kube-system team=x", "", "kube-system"},
		{"label ns/shop pod/web team=x", "", "*"},
		// Round 2: the kubectl tool runs no shell, so a brace or a glob is a literal word there (kubectl
		// refuses {ns,kube-system} as a type); the shell path reads the expansion (shell_kube.go). With
		// the objects unreadable anyway, a brace leaf of the namespace kind makes the target "*".
		{"delete {ns,kube-system}", "", ""},
		{"delete ns kube-sys{tem,}", "", "kube-sys{tem,}"},
		{"patch ns shop -p {\"a\":1,\"b\":2}", "", "shop"},
		{"delete --weird x {ns,kube-system}", "", "*"},
		{"delete --weird x n? kube-system", "", "*"},
	}
	for _, c := range cases {
		if ctx, ns := KubeTargets(strings.Fields(c.args)); ctx != c.ctx || ns != c.ns {
			t.Errorf("KubeTargets(%s) = %q %q, want %q %q", c.args, ctx, ns, c.ctx, c.ns)
		}
	}
}

// TestShellKubeReadsANamespaceObject drives item 1 through the shell classifier: every spelling of a
// namespace deletion on a shell line targets the namespace, and a "*" from namespace objects says why.
func TestShellKubeReadsANamespaceObject(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"kubectl delete ns kube-system", "{ kube-system }"},
		{"kubectl delete namespace/kube-system", "{ kube-system }"},
		{"kubectl delete namespaces kube-system --wait=false", "{ kube-system }"},
		{"kubectl -n default delete ns/kube-system", "{ * }"},
		{"kubectl delete ns --all", "{ * }"},
		{"kubectl label ns kube-system team=x", "{ kube-system }"},
		{"kubectl delete -- ns kube-system", "{ kube-system }"},
		{"kubectl delete --raw /api/v1/namespaces/kube-system", "{ * }"},
		{"kubectl get ns kube-system", ""},
		{"kubectl label ns - kube-system team=x", "{ * }"},
		{"kubectl annotate ns =a kube-system note=x", "{ * }"},
		{"kubectl label ns -- - kube-system a=b", "{ * }"},
		{"kubectl label ns/shop team=x", "{ shop }"},
		{"kubectl annotate namespace/shop note=x", "{ shop }"},
	}
	for _, c := range cases {
		var got []string
		for _, k := range Shell(c.cmd).Kube {
			got = append(got, fmt.Sprintf("{%s %s %s}", k.Context, k.Namespace, k.Kubeconfig))
		}
		if strings.Join(got, "|") != c.want {
			t.Errorf("%q: kube %q, want %q", c.cmd, strings.Join(got, "|"), c.want)
		}
	}
	for _, cmd := range []string{"kubectl -n default delete ns/kube-system", "kubectl delete ns a b",
		"kubectl label ns - kube-system team=x"} {
		kube := Shell(cmd).Kube
		if len(kube) != 1 || kube[0].Cause != causeNamespaceObjects {
			t.Errorf("%q: %+v, want the namespace-object cause", cmd, kube)
		}
	}
}

// namespaceObjectReads are reads next to the namespace-object rules (round 2): a get of a namespace
// object and a client-side dry run of its deletion stay reads. Each also runs through the
// differential harness.
var namespaceObjectReads = []string{"kubectl get ns kube-system", "kubectl delete ns kube-system --dry-run=client"}

func TestNamespaceObjectReadsStayReads(t *testing.T) {
	checkReads(t, namespaceObjectReads)
}

// TestShellKubeReadsAnExpandedNamespaceObject (round 2, next to item 5): sh expands a glob or a brace
// with alternatives before kubectl reads the words, and brace expansion adds words anywhere: it can
// name the namespace kind ({ns,kube-system} is ns kube-system), add a name to a namespace object
// (--grace-period {0,kube-system}), move the verb (kubectl {delete,ns} kube-system) or a global
// option's value, and a glob can match a file named ns or kube-system. So on a shell line a kubectl
// command that changes objects, holds such a word and can name the namespace kind (among its objects,
// in a value's expansion, or anywhere once the verb is unreadable) acts on any namespace. A pod name
// with a brace, a glob in -f and a read keep their targets. A brace that names kubectl as the program
// is a word that names kubectl where the classifier does not read it as a program.
func TestShellKubeReadsAnExpandedNamespaceObject(t *testing.T) {
	expanded := []string{
		"kubectl delete {ns,kube-system}",
		"kubectl delete n{s,} kube-system",
		"kubectl delete namespace{,} kube-system",
		"kubectl delete {ns/kube-system,pod/x}",
		"kubectl delete n? kube-system",
		"kubectl delete [n]s kube-system",
		"kubectl delete --wait=false ns kube-sys{tem,}",
		"kubectl label ns kube-sys{tem,} x=y",
		"kubectl label {ns,kube-system} team=x",
		"kubectl label ns {kube-system,a=b}",
		"kubectl delete ns shop --grace-period {0,kube-system}",
		"kubectl delete ns shop --grace-period *",
		"kubectl delete --grace-period {0,ns} kube-system",
		"kubectl delete --grace-period * kube-system",
		"kubectl {delete,ns} kube-system",
		"kubectl --context {x,delete} ns kube-system",
		"kubectl -n shop {delete,ns,kube-system}",
		"kubectl delete -- {ns,kube-system}",
		"kubectl patch ns shop -p {\"a\":1,\"b\":2}",
	}
	for _, cmd := range expanded {
		kube := Shell(cmd).Kube
		if len(kube) != 1 || kube[0].Namespace != "*" || kube[0].Cause != causeExpandedObjects {
			t.Errorf("%q: %+v, want the namespace * with the expanded-object cause", cmd, kube)
		}
	}
	for cmd, want := range map[string]string{
		"kubectl delete pod web-{1,2} -n shop":       "shop",
		"kubectl delete pod web-* -n shop":           "shop",
		"kubectl apply -f namespaces/*.yaml -n shop": "shop",
		"kubectl delete -f k8s/{a,b}.yaml -n shop":   "shop",
		"kubectl delete ns shop":                     "shop",
	} {
		if kube := Shell(cmd).Kube; len(kube) != 1 || kube[0].Namespace != want || kube[0].Cause != "" {
			t.Errorf("%q: %+v, want the namespace %s", cmd, kube, want)
		}
	}
	if kube := Shell("kubectl get {ns,pods} kube-system").Kube; len(kube) != 0 {
		t.Errorf("a read has no kube target: %+v", kube)
	}
	for _, cmd := range []string{"{kubectl,delete} ns kube-system", "/usr/local/bin/kube{ctl,} delete ns kube-system"} {
		if kube := Shell(cmd).Kube; len(kube) != 1 || kube[0].Namespace != "*" || kube[0].Cause != causeUnknownProg {
			t.Errorf("%q: %+v, want * as a kubectl the classifier does not read as the program", cmd, kube)
		}
	}
}
