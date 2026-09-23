package classify

import (
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// lockedDenials are the lines pre-tag round 2 must keep classifying as mutations with the kube target
// shown (an empty want is a mutation that carries no kube target). A relaxation that reopened any of
// these holes would change a row, so the table is the round's regression lock. The kube string is
// {context namespace kubeconfig} per target, "|" between two; the classifications and targets were
// confirmed against /bin/sh where a shell runs the line.
var lockedDenials = []struct{ cmd, kube string }{
	// item 1: a shell function's body runs on the call and can shadow any read-only name.
	{`ls() { find . "$@"; }; ls -delete`, ""},
	{`ls () { find . "$@"; }; ls -delete`, ""},
	{`ls() ( find . "$@" ); ls -delete`, ""},
	{`echo() { find . $*; }; echo -delete`, ""},
	{`grep() { find "$@"; }; grep . -delete`, ""},
	{`function ff { find . -delete; }; ff`, ""},
	{`ls() { kubectl delete pod web "$@"; }; ls -n kube-system`, "{* * }"},
	// item 2: bash drops the NUL and acts on the truncated word.
	{`kubectl delete pod coredns -n kube-system$'\x00'`, "{ kube-system }"},
	{`kubectl delete pod web -n $'kube-system\x00'`, "{ kube-system }"},
	{`find . $'-delete\x00'`, ""},
	{`find . $'-del\x00'ete`, ""},
	// item 4: set with arguments, and special or positional parameters among kubectl's arguments.
	{`set -- --context=prod-eu -n kube-system; kubectl delete pod web $@`, "{* * }"},
	{`set -k; kubectl delete pod web KUBECONFIG=/k/prod.yaml`, "{* * }"},
	{`kubectl delete pod web $@ -n apps`, "{* * }"},
	// round 1: kubectl and helm behind the shell's reserved words and grouping.
	{`for p in a b; do kubectl delete pod $p -n kube-system; done`, "{ kube-system }"},
	{`(kubectl -n kube-system delete pod x)`, "{ kube-system }"},
	{`{ kubectl -n kube-system delete pod x; }`, "{ kube-system }"},
	{`if true; then kubectl -n kube-system delete pod x; fi`, "{ kube-system }"},
	{`! kubectl -n kube-system delete pod x`, "{ kube-system }"},
	// round 1: a context switched earlier, repeated flags, HELM_*, a plain KUBECONFIG, sudo, xargs, $POD.
	{`kubectl config use-context prod-eu && kubectl delete pod web`, "{  }|{* * }"},
	{`kubectl delete pod web -n default -n kube-system`, "{ * }"},
	{`HELM_NAMESPACE=kube-system helm uninstall web`, "{ kube-system }"},
	{`KUBECONFIG=/k/dev.yaml; kubectl delete pod web`, "{* * }"},
	{`sudo kubectl delete pod web -n apps`, "{* apps }"},
	{`echo web | xargs kubectl delete pod -n apps`, "{* * }"},
	{`kubectl delete pod $POD -n apps`, "{* * }"},
}

// TestTheLockedDenialsStayDenials locks lockedDenials (pre-tag round 2, item 9): each must classify
// as a mutation and carry the kube target shown, so a later relaxation cannot reopen a fail-open hole
// unnoticed.
func TestTheLockedDenialsStayDenials(t *testing.T) {
	for _, c := range lockedDenials {
		if got := Shell(c.cmd); got.Classification != policy.Mutate {
			t.Errorf("%q must stay a mutation: %+v", c.cmd, got)
		}
		if got := kubeOf(c.cmd); got != c.kube {
			t.Errorf("%q: kube %q, want %q", c.cmd, got, c.kube)
		}
	}
}
