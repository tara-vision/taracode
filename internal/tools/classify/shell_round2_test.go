package classify

import (
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestShellFunctionDefinitionsAreMutations (pre-tag round 2, item 1): a shell function's body runs on
// the call with arguments the classifier cannot correlate, so every definition form is a mutation,
// and a kubectl or helm word anywhere on the line gives "*" targets. Verified against /bin/sh: each
// form runs the body and deletes files.
func TestShellFunctionDefinitionsAreMutations(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{`ls() { find . "$@"; }; ls -delete`, "shell function"},
		{`ls () { find . "$@"; }; ls -delete`, "shell function"},
		{`ls() ( find . "$@" ); ls -delete`, "shell function"},
		{`echo() { find . $*; }; echo -delete`, "shell function"},
		{`grep() { find "$@"; }; grep . -delete`, "shell function"},
		{`function ff { find . -delete; }; ff`, "shell function"},
		{`ls() { kubectl delete pod web "$@"; }; ls -n kube-system`, "shell function"},
		{`f() { ls; }; f`, "shell function"},
	})
	kube := map[string]string{
		`ls() { kubectl delete pod web "$@"; }; ls -n kube-system`: "{* * }",
		`f() { helm uninstall web; }; f -n kube-system`:            "{* * }",
		`f() { ls; }; f`: "", // no kube word: a plain mutation, no target
	}
	for cmd, want := range kube {
		if got := kubeOf(cmd); got != want {
			t.Errorf("%q: kube %q, want %q", cmd, got, want)
		}
	}
}

// TestShellSetWithArgumentsIsNotNeutral (pre-tag round 2, item 4): set with any argument changes the
// positional parameters (set --) or the keyword mode (set -k), so a later kubectl or helm mutation
// acts on a context taracode cannot know; a special or positional parameter ($@, $1, ...) among a
// kubectl or helm mutation's arguments can split into flags, so it gives "*" too. Bare set (which
// only lists variables) stays neutral.
func TestShellSetWithArgumentsIsNotNeutral(t *testing.T) {
	cases := map[string]string{
		`set -- --context=prod-eu -n kube-system; kubectl delete pod web $@`: "{* * }",
		`set -k; kubectl delete pod web KUBECONFIG=/k/prod.yaml`:             "{* * }",
		`set -- -n kube-system; kubectl delete pod web "$1" "$2"`:            "{* * }",
		`kubectl delete pod web $@ -n apps`:                                  "{* * }",
		`kubectl delete pod web $1 -n apps`:                                  "{* * }",
		`set; kubectl delete pod web -n apps`:                                "{ apps }", // bare set is neutral
	}
	for cmd, want := range cases {
		if got := kubeOf(cmd); got != want {
			t.Errorf("%q: kube %q, want %q", cmd, got, want)
		}
	}
}

// TestShellDockerVerbsThatTouchNoCluster (pre-tag round 2, item 6): only docker run and exec (and
// container/compose run and exec) can run a hidden kubectl, so they are opaque; every other docker
// verb touches no cluster, so a kubectl or helm image name in its arguments adds no "*" target. The
// commands are still mutations (they change local images), but not kube ones.
func TestShellDockerVerbsThatTouchNoCluster(t *testing.T) {
	for _, cmd := range []string{
		"docker pull bitnami/kubectl:1.30",
		"docker tag alpine/helm:3.14 registry.local/helm:3.14",
		"docker push registry.local/helm:3.14",
		"docker ps | grep kubectl > running.txt",
		"docker images | grep kubectl",
	} {
		if got := kubeOf(cmd); got != "" {
			t.Errorf("%q must carry no kube target: %q", cmd, got)
		}
	}
	// run/exec still add a "*" target for a hidden kubectl image.
	for _, cmd := range []string{
		"docker run --rm bitnami/kubectl:1.30 delete pod x -n kube-system",
		"docker exec c kubectl delete pod x -n kube-system",
		"docker compose run kubectl delete pod x",
	} {
		if got := kubeOf(cmd); got != "{* * }" {
			t.Errorf("%q must be opaque with a * target: %q", cmd, got)
		}
	}
}

// distroKubectlWrapperReads are the distro-kubectl-wrapper reads TestShellDistroKubectlWrappersClassifyByTheKubectlVerb
// locks: they reach a cluster through microk8s, k3s, k0s or minikube rather than kubectl itself. The
// differential harness runs each one through Shell() and, via its shim, cross-checks it against Kubectl too.
var distroKubectlWrapperReads = []string{
	"microk8s kubectl get pods", "k3s kubectl get pods -A", "minikube kubectl -- get pods",
	"k0s kubectl get pods -n kube-system",
}

// TestShellDistroKubectlWrappersClassifyByTheKubectlVerb (pre-tag round 2, item 6): microk8s, k3s,
// k0s and minikube run kubectl as a subcommand, so "<distro> kubectl <verb>" is classified by that
// verb (with "--" for minikube): a read is a read with no target, and a mutation collects its target
// from the tokens after kubectl.
func TestShellDistroKubectlWrappersClassifyByTheKubectlVerb(t *testing.T) {
	for _, cmd := range distroKubectlWrapperReads {
		if r := Shell(cmd); r.Classification != policy.Read {
			t.Errorf("%q must be a read: %+v", cmd, r)
		}
		if got := kubeOf(cmd); got != "" {
			t.Errorf("%q read must carry no target: %q", cmd, got)
		}
	}
	// sudo escalates, so the line is a mutation, but the distro-wrapped read still carries no target.
	if got := kubeOf("sudo k3s kubectl get pods"); got != "" {
		t.Errorf("sudo k3s kubectl get pods must carry no target: %q", got)
	}
	mutations := map[string]string{
		"microk8s kubectl delete pod web -n kube-system": "{ kube-system }",
		"k3s kubectl delete pod web --context prod-eu":   "{prod-eu  }",
		"minikube kubectl -- delete pod web -n apps":     "{ apps }",
	}
	for cmd, want := range mutations {
		if r := Shell(cmd); r.Classification != policy.Mutate {
			t.Errorf("%q must be a mutation: %+v", cmd, r)
		}
		if got := kubeOf(cmd); got != want {
			t.Errorf("%q: kube %q, want %q", cmd, got, want)
		}
	}
}

// TestShellKubeTargetsCarryACause (pre-tag round 2, item 5): the "*" target records why taracode
// could not pin it down, one short phrase per kind, so the deny can name the cause and a remedy.
func TestShellKubeTargetsCarryACause(t *testing.T) {
	cases := map[string]string{
		"kubectl config use-context prod-eu && kubectl delete pod web": causeContextSwitch,
		"kubectl delete pod $POD -n apps":                              causeRunTimeArg,
		"sudo kubectl delete pod web -n apps":                          causeWrapper,
		"./build.sh && kubectl apply -f k8s/":                          causeUnknownProg,
		"KUBECONFIG=/k/dev.yaml; kubectl delete pod web":               causeKubeconfigSet,
		"ls() { kubectl delete pod web; }; ls":                         causeFunctionDef,
	}
	for cmd, want := range cases {
		var cause string
		for _, k := range Shell(cmd).Kube {
			if k.Cause != "" {
				cause = k.Cause
			}
		}
		if cause != want {
			t.Errorf("%q: cause %q, want %q", cmd, cause, want)
		}
	}
}
