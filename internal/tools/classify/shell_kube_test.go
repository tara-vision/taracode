package classify

import (
	"fmt"
	"strings"
	"testing"
)

// kubeOf formats the kube targets of a shell line: {context namespace kubeconfig}, "|" between two.
func kubeOf(command string) string {
	var out []string
	for _, k := range Shell(command).Kube {
		out = append(out, fmt.Sprintf("{%s %s %s}", k.Context, k.Namespace, k.Kubeconfig))
	}
	return strings.Join(out, "|")
}

// TestShellKubeNamesTheClustersAShellLineMutates: a kubectl or helm mutation run through the shell
// carries the context, namespace and kubeconfig it names, from the tokens before "--" (ruling
// P2-R34); a kubectl or helm read carries none, even when the line writes a file. Since the pre-tag
// round, sudo (another user and environment) and a KUBECONFIG set by an earlier segment (which a
// conditional or a subshell can skip) make the context "*" and so a namespace the command does not
// name.
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
		{"sudo kubectl delete pod web -n apps", "{* apps }"},
		{"timeout 30 kubectl delete pod web", "{  }"},
		{"KUBECONFIG=/k/prod.yaml kubectl delete pod web", "{  /k/prod.yaml}"},
		{"env KUBECONFIG=/k/prod.yaml kubectl delete pod web", "{  /k/prod.yaml}"},
		{"KUBECONFIG=/k/prod.yaml; kubectl delete pod web", "{* * }"},
		{"export KUBECONFIG=/k/prod.yaml && kubectl delete pod web", "{* * }"},
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

// TestShellKubeTargetsAfterAConfigChange (pre-tag round C): a kubectl or helm mutation after a
// segment that can change the kube configuration (kubectl config, kubectx, kubens, a KUBECONFIG
// assignment or export, a cloud credentials command, a write to a kubeconfig, a program the
// classifier does not know) acts on a context it cannot know, so the context is "*", and so is a
// namespace the command does not name. sudo, xargs and env -i or -u change the kubeconfig or the
// arguments kubectl gets. A change later on the line, cd and the safe variables change nothing. A
// kubectl config command keeps its own target, as before the round.
func TestShellKubeTargetsAfterAConfigChange(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"kubectl config use-context prod-eu && kubectl delete pod web", "{  }|{* * }"},
		{"kubens kube-system && kubectl delete pod coredns", "{* * }"},
		{"kubectx prod-eu; kubectl delete pod web -n apps", "{* apps }"},
		{"kubectl config set-context --current --namespace kube-system && kubectl delete pod x", "{ kube-system }|{* * }"},
		{"kubectl ctx prod-eu && kubectl delete pod web", "{  }|{* * }"},
		{"export KUBECONFIG=/k/prod.yaml && kubectl delete pod web", "{* * }"},
		{"KUBECONFIG=/k/prod.yaml; kubectl delete pod web", "{* * }"},
		{"aws eks update-kubeconfig --name prod && kubectl delete pod web", "{* * }"},
		{"gcloud container clusters get-credentials prod && kubectl delete pod web", "{* * }"},
		{"cp prod.yaml ~/.kube/config && kubectl delete pod web", "{* * }"},
		{"cp prod.yaml k.yaml && kubectl delete pod web --kubeconfig k.yaml", "{* * k.yaml}"},
		{"./use-prod.sh && helm uninstall web", "{* * }"},
		{". ./env.sh && kubectl delete pod web", "{* * }"},
		{"sudo kubectl delete pod web -n apps", "{* apps }"},
		{"echo web | xargs kubectl delete pod -n apps", "{* * }"},
		{"echo --dry-run=none | xargs kubectl apply -f x.yaml --dry-run=client", "{* * }"},
		{"echo delete pod web | xargs kubectl", "{* * }"},
		{"echo web | xargs kubectl get pods", ""},
		{"env -u KUBECONFIG kubectl delete pod web", "{* * }"},
		{"PATH=/tmp/bin kubectl delete pod web", "{* * }"},
		{"cd infra && KUBECONFIG=./kc kubectl delete pod web", "{* * ./kc}"},
		{"kubectl delete pod web && kubectl config use-context prod-eu", "{  }|{  }"},
		{"cd k8s && kubectl delete pod web", "{  }"},
		{"TZ=UTC; kubectl delete pod web", "{  }"},
		{"mkdir -p out && kubectl delete pod web -n apps", "{ apps }"},
		{"[ -f x.yaml ] && kubectl apply -f x.yaml -n apps", "{ apps }"},
		{"set -euo pipefail; kubectl rollout restart deploy/web -n apps", "{ apps }"},
		{"docker build -t app . && kubectl apply -f k8s/ -n apps", "{ apps }"},
		{"kubectl apply -f a.yaml && kubectl delete pod web", "{  }|{  }"},
	}
	for _, c := range cases {
		if got := kubeOf(c.cmd); got != c.want {
			t.Errorf("%q: kube %q, want %q", c.cmd, got, c.want)
		}
	}
}

// TestShellHelmNamespaceAndContextVariables (pre-tag round E): HELM_NAMESPACE and HELM_KUBECONTEXT
// set for a helm command (prefix or env) are its namespace and context when its flags name none; an
// export of them earlier on the line is a change the classifier cannot follow; an empty value is
// unset for helm, so the namespace is the kubeconfig's and is not known here. kubectl ignores them.
func TestShellHelmNamespaceAndContextVariables(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"HELM_NAMESPACE=kube-system helm uninstall web --kube-context kind-dev", "{kind-dev kube-system }"},
		{"env HELM_KUBECONTEXT=prod-eu helm uninstall web", "{prod-eu  }"},
		{"HELM_NAMESPACE=kube-system helm uninstall web -n apps", "{ apps }"},
		{"export HELM_NAMESPACE=kube-system && helm uninstall web", "{* * }"},
		{"HELM_NAMESPACE= helm uninstall web", "{ * }"},
		{"HELM_NAMESPACE=kube-system kubectl delete pod web", "{  }"},
	}
	for _, c := range cases {
		if got := kubeOf(c.cmd); got != c.want {
			t.Errorf("%q: kube %q, want %q", c.cmd, got, c.want)
		}
	}
}

// TestShellKubeTargetsBehindKeywordsAndGrouping (pre-tag round B): a kubectl or helm mutation behind
// the shell's reserved words and grouping carries its targets. A kubectl or helm word the classifier
// cannot place as a program, on a line that runs something it cannot see into, gives "*". A run-time
// value among the arguments (a variable, a substitution, a brace expansion) can split into options
// that override the ones read, so it gives "*" too, except a loop variable over plain words.
func TestShellKubeTargetsBehindKeywordsAndGrouping(t *testing.T) {
	kube := []struct{ cmd, want string }{
		{"for p in a b; do kubectl delete pod $p -n kube-system; done", "{ kube-system }"},
		{"(kubectl -n kube-system delete pod x)", "{ kube-system }"},
		{"{ kubectl -n kube-system delete pod x; }", "{ kube-system }"},
		{"if true; then kubectl -n kube-system delete pod x; fi", "{ kube-system }"},
		{"! kubectl -n kube-system delete pod x", "{ kube-system }"},
		{"{kubectl -n kube-system delete pod x", "{ kube-system }"},
		{"kubectl delete pod {web,-nkube-system}", "{* * }"},
		{"while true; do helm uninstall web -n kube-system; done", "{ kube-system }"},
		{"case $x in a) kubectl delete pod web -n kube-system;; esac", "{ kube-system }"},
		{"echo $(kubectl delete pod x -n kube-system)", "{ kube-system }"},
		{"coproc kubectl delete pod x", "{* * }"},
		{"echo kubectl delete pod x -n kube-system | sh", "{* * }"},
		{"find . -name '*.yaml' -exec kubectl delete -f {} -n kube-system ';'", "{* * }"},
		{"kubectl exec web -- kubectl delete pod x -n kube-system", "{  }|{* * }"},
		{"docker run --rm bitnami/kubectl:1.30 delete pod x -n kube-system", "{* * }"},
		{"for p in a b; do kubectl delete pod $p -n apps; done", "{ apps }"},
		{"for p in $(cat pods.txt); do kubectl delete pod $p -n apps; done", "{* * }"},
		{"for p in -nkube-system; do kubectl delete pod web $p; done", "{* * }"},
		{"for f in *.yaml; do kubectl delete -f $f; done", "{* * }"},
		{"kubectl delete pod $(cat pods.txt)", "{* * }"},
		{"kubectl delete pod $POD -n apps", "{* * }"},
		{"kubectl exec web -n apps -- sh -c \"$CMD\"", "{ apps }"},
		{"env -S 'kubectl config use-context prod-eu' && kubectl delete pod web", "{* * }"},
		{"grep kubectl notes.txt > found.txt", ""},
		{"chmod +x kubectl && sudo mv kubectl /usr/local/bin/", ""},
		{"for p in a b; do kubectl get pod $p; done > pods.txt", ""},
	}
	for _, c := range kube {
		if got := kubeOf(c.cmd); got != c.want {
			t.Errorf("%q: kube %q, want %q", c.cmd, got, c.want)
		}
	}
}
