package evals

import "testing"

func TestSignatures(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}, "kubectl get pod -n shop"},
		{"kubectl", map[string]any{"verb": "get", "resource": "po", "namespace": "shop", "output": "wide"}, "kubectl get pod -n shop -o wide"},
		{"shell", map[string]any{"command": "kubectl get pods -n shop -o wide"}, "kubectl get pod -n shop -o wide"},
		{"shell", map[string]any{"command": "kubectl -n shop get pods -owide"}, "kubectl get pod -n shop -o wide"},
		{"shell", map[string]any{"command": "kubectl get pods --namespace=shop"}, "kubectl get pod -n shop"},
		{"kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}, "kubectl describe pod/checkout-1 -n shop"},
		{"shell", map[string]any{"command": "kubectl describe pods/checkout-1 -n shop"}, "kubectl describe pod/checkout-1 -n shop"},
		{"shell", map[string]any{"command": "kubectl describe pod checkout-1 -n shop"}, "kubectl describe pod/checkout-1 -n shop"},
		{"kubectl", map[string]any{"verb": "logs", "name": "checkout-1", "namespace": "shop", "args": "--previous"}, "kubectl logs checkout-1 -n shop --previous"},
		{"shell", map[string]any{"command": "kubectl logs checkout-1 --previous -n shop"}, "kubectl logs checkout-1 -n shop --previous"},
		{"kubectl", map[string]any{"verb": "get", "resource": "events", "namespace": "shop", "args": "--sort-by=.lastTimestamp"}, "kubectl get event -n shop --sort-by=.lastTimestamp"},
		{"kubectl", map[string]any{"verb": "get", "resource": "nodes"}, "kubectl get node"},
		{"kubectl", map[string]any{"verb": "get", "resource": "deploy", "name": "checkout", "namespace": "shop", "context": "prod-cluster"}, "kubectl get deployment/checkout -n shop --context prod-cluster"},
		{"helm", map[string]any{"args": "list   -A"}, "helm list -A"},
		{"shell", map[string]any{"command": "helm list -A"}, "helm list -A"},
		{"git", map[string]any{"args": "status"}, "git status"},
		{"shell", map[string]any{"command": "git status"}, "git status"},
		{"docker", map[string]any{"args": "logs crashed-app --tail 50"}, "docker logs crashed-app --tail 50"},
		{"terraform", map[string]any{"command": "plan"}, "terraform plan dir=."},
		{"shell", map[string]any{"command": "terraform plan"}, "terraform plan dir=."},
		{"shell", map[string]any{"command": "terraform -chdir=infra plan -no-color"}, "terraform plan dir=infra -no-color"},
		{"terraform", map[string]any{"command": "state", "dir": "./infra/", "args": "list"}, "terraform state dir=infra list"},
		{"cloud", map[string]any{"provider": "aws", "args": "iam get-policy-version --policy-arn x --version-id v1"}, "aws iam get-policy-version --policy-arn x --version-id v1"},
		{"shell", map[string]any{"command": "aws iam get-policy-version --policy-arn x --version-id v1"}, "aws iam get-policy-version --policy-arn x --version-id v1"},
		{"shell", map[string]any{"command": "gcloud projects get-iam-policy acme-data-prod"}, "gcloud projects get-iam-policy acme-data-prod"},
		{"scan", map[string]any{"scanner": "trivy", "target": "python:3.9.0-alpine", "severity": "high,critical"}, "scan trivy python:3.9.0-alpine HIGH,CRITICAL"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "repo"}, "scan gitleaks repo"},
		{"read_file", map[string]any{"path": "a.txt", "start_line": 1}, "read_file path=a.txt start_line=1"},
		{"shell", map[string]any{"command": "  cat  file | grep x ; "}, "shell cat file | grep x"},
		{"shell", map[string]any{"command": "echo hi > out.txt"}, "shell echo hi > out.txt"},
		{"shell", map[string]any{"command": "kubectl get pods | grep Running"}, "shell kubectl get pods | grep Running"},
	}
	for _, c := range cases {
		if got := Signature(c.tool, c.args); got != c.want {
			t.Errorf("%s %v:\n got %q\nwant %q", c.tool, c.args, got, c.want)
		}
	}
	if got := DryRunSignature("helm", map[string]any{"args": "upgrade web ./chart -n apps"}); got != "dryrun:helm upgrade web ./chart -n apps" {
		t.Errorf("dry run %q", got)
	}
}
