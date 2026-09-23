package classify

import (
	"fmt"
	"strings"
	"testing"
)

// TestShellKubeNamesTheClustersAShellLineMutates: a kubectl or helm mutation run through the shell
// carries the context, namespace and kubeconfig it names, from the tokens before "--" (ruling
// P2-R34); a kubectl or helm read carries none, even when the line writes a file.
func TestShellKubeNamesTheClustersAShellLineMutates(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"kubectl delete pods --all -A", "{ * }"},
		{"kubectl -n kube-system delete pod x", "{ kube-system }"},
		{"kubectl --context prod-eu delete pod web", "{prod-eu  }"},
		{"kubectl --context=prod-eu --namespace=apps delete pod web", "{prod-eu apps }"},
		{"kubectl exec web -n apps -- env -n kube-system", "{ apps }"},
		{"helm uninstall web -n kube-system", "{ kube-system }"},
		{"helm uninstall web --kube-context prod-eu", "{prod-eu  }"},
		{"helm uninstall web -A", "{ * }"},
		{"kubectl delete pod a -n x; kubectl delete pod b -n y", "{ x }|{ y }"},
		{"sudo kubectl delete pod web -n apps", "{ apps }"},
		{"timeout 30 kubectl delete pod web", "{  }"},
		{"KUBECONFIG=/k/prod.yaml kubectl delete pod web", "{  /k/prod.yaml}"},
		{"env KUBECONFIG=/k/prod.yaml kubectl delete pod web", "{  /k/prod.yaml}"},
		{"KUBECONFIG=/k/prod.yaml; kubectl delete pod web", "{  /k/prod.yaml}"},
		{"export KUBECONFIG=/k/prod.yaml && kubectl delete pod web", "{  /k/prod.yaml}"},
		{"kubectl delete pod web --kubeconfig /k/dev.yaml", "{  /k/dev.yaml}"},
		{"helm uninstall web --kubeconfig=/k/dev.yaml", "{  /k/dev.yaml}"},
		{"kubectl get pods -n kube-system", ""},
		{"kubectl get pods -A > pods.txt", ""},
		{"helm upgrade web ./chart -n kube-system --dry-run", ""},
		{"ls -la", ""},
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
}

func TestHelmTargets(t *testing.T) {
	ctx, ns := HelmTargets(strings.Fields("upgrade web ./chart --kube-context staging -n apps"))
	if ctx != "staging" || ns != "apps" {
		t.Errorf("%q %q", ctx, ns)
	}
	if ctx, ns := HelmTargets(strings.Fields("uninstall web -- --kube-context prod -n kube-system")); ctx != "" || ns != "" {
		t.Errorf("flags after -- are release names: %q %q", ctx, ns)
	}
}
