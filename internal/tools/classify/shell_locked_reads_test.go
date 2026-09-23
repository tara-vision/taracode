package classify

import (
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// everydayReads are the commands users and models run most through the shell tool. Each must stay a
// read with no mutation reason and no kube target: a classifier change that turns one of them into a
// mutation breaks investigate mode for everyday work, and in operate mode a kube target on a read
// can turn it into a hard deny.
var everydayReads = []string{
	"ls -la", "cat file.txt", "grep -rn TODO .", "git status", "git log --oneline -5", "git diff",
	"kubectl get pods -n default", "kubectl -n kube-system get pods", "kubectl --context prod-eu get pods",
	"kubectl describe pod x", "kubectl logs x --tail 100", "helm list -A", "helm -n kube-system list",
	"terraform plan", "terraform show", "docker ps", "docker logs x", "aws s3 ls", "az account show",
	"gcloud config list", "head -n 20 f", "wc -l f", "find . -name '*.go'", "env", "echo hi",
	"cat a | grep b | sort | uniq -c", "kubectl get pods -o yaml", "kubectl get pods --context dev -n apps",
	"KUBECONFIG=~/.kube/dev kubectl get pods", "TZ=UTC date", "sed 's/[/]/_/g' f", "sed -n '1,5p' f",
	"for f in a b; do cat $f; done", "if true; then ls; fi",
}

// TestTheEverydayReadsStayReads locks everydayReads (pre-tag round).
func TestTheEverydayReadsStayReads(t *testing.T) {
	for _, c := range everydayReads {
		got := Shell(c)
		if got.Classification != policy.Read || got.Reason != "" || len(got.Kube) != 0 {
			t.Errorf("%q must stay a read with no reason and no kube target: %+v", c, got)
		}
	}
}
