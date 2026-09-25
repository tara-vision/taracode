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
	for _, cmd := range []string{"kubectl -n default delete ns/kube-system", "kubectl delete ns a b"} {
		kube := Shell(cmd).Kube
		if len(kube) != 1 || kube[0].Cause != causeNamespaceObjects {
			t.Errorf("%q: %+v, want the namespace-object cause", cmd, kube)
		}
	}
}
