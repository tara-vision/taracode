package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/tools"
)

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
		{"terraform", map[string]any{"command": "plan", "dir": "my infra"}, `terraform plan dir="my infra"`},
		{"cloud", map[string]any{"provider": "aws", "args": "iam get-policy-version --policy-arn x --version-id v1"}, "aws iam get-policy-version --policy-arn x --version-id v1"},
		{"shell", map[string]any{"command": "aws iam get-policy-version --policy-arn x --version-id v1"}, "aws iam get-policy-version --policy-arn x --version-id v1"},
		{"shell", map[string]any{"command": "gcloud projects get-iam-policy acme-data-prod"}, "gcloud projects get-iam-policy acme-data-prod"},
		{"cloud", map[string]any{"provider": "terraform", "args": "plan dir=."}, "cloud terraform plan dir=."},
		{"scan", map[string]any{"scanner": "trivy", "target": "python:3.9.0-alpine", "severity": "high,critical"}, "scan trivy python:3.9.0-alpine HIGH,CRITICAL"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "repo"}, "scan gitleaks repo"},
		{"read_file", map[string]any{"path": "a.txt", "start_line": 1}, "read_file path=a.txt start_line=1"},
		{"shell", map[string]any{"command": "  cat  file | grep x ; "}, "shell cat file | grep x"},
		{"shell", map[string]any{"command": "echo hi > out.txt"}, "shell echo hi > out.txt"},
		{"shell", map[string]any{"command": "kubectl get pods | grep Running"}, "shell kubectl get pods | grep Running"},
		{"shell", map[string]any{"command": "kubectl -n logs get pods"}, "kubectl get pod -n logs"},
		{"shell", map[string]any{"command": "kubectl -n get delete pods"}, "kubectl delete pod -n get"},
		{"shell", map[string]any{"command": "kubectl -n shop-v2 rollout restart deploy/cart"}, "kubectl rollout restart deploy/cart -n shop-v2"},
		{"shell", map[string]any{"command": "kubectl -n shop scale deploy/checkout --replicas=3"}, "kubectl scale deployment/checkout -n shop --replicas=3"},
		{"shell", map[string]any{"command": "kubectl --context prod-cluster -n shop get deploy checkout"}, "kubectl get deployment/checkout -n shop --context prod-cluster"},
		{"shell", map[string]any{"command": "kubectl get -n shop pods"}, "kubectl get pod -n shop"},
		// The first shake-out's shapes (ruling P3-R59): the command line repeated in args keys the
		// command the kubectl tool runs once it has undone the repetition.
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"},
			"kubectl get pod -n billing"},
		{"kubectl", map[string]any{"verb": "get", "resource": "deployment", "namespace": "shop",
			"args": "get deployment checkout -n shop --context prod-cluster"},
			"kubectl get deployment/checkout -n shop --context prod-cluster"},
		{"kubectl", map[string]any{"verb": "get", "args": "get pod report-builder -n analytics"},
			"kubectl get pod/report-builder -n analytics"},
		// The second shake-out's shapes (ruling P3-R64). A describe whose resource and name arrive in one
		// parameter keys as the recorded structured call; kubectl itself refuses the one-word form, so
		// the kubectl tool runs the words apart and the signature follows it.
		{"kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "invoice-worker-fbf95d7bd-ccb5n",
			"namespace": "billing"}, "kubectl describe pod/invoice-worker-fbf95d7bd-ccb5n -n billing"},
		{"kubectl", map[string]any{"verb": "describe", "resource": "pod invoice-worker-fbf95d7bd-ccb5n", "namespace": "billing"},
			"kubectl describe pod/invoice-worker-fbf95d7bd-ccb5n -n billing"},
		{"kubectl", map[string]any{"verb": "describe", "name": "pod storefront-5996978c75-4pxx5", "namespace": "web"},
			"kubectl describe pod/storefront-5996978c75-4pxx5 -n web"},
		{"shell", map[string]any{"command": "kubectl describe pod storefront-5996978c75-4pxx5 -n web"},
			"kubectl describe pod/storefront-5996978c75-4pxx5 -n web"},
		{"kubectl", map[string]any{"verb": "describe pod", "name": "orders-api-674454676b-dmf7d", "namespace": "api"},
			"kubectl describe pod/orders-api-674454676b-dmf7d -n api"},
		{"kubectl", map[string]any{"verb": "describe", "args": "pod orders-api-674454676b-dmf7d -n api"},
			"kubectl describe pod/orders-api-674454676b-dmf7d -n api"},
		// type,name, which kubectl reads as two resource types and refuses: the kubectl tool runs
		// type/name, so it keys as the structured call. logs takes pod/NAME and NAME alike and keys the
		// bare name. A shell line runs as written and fails in kubectl, so it keeps its comma.
		{"kubectl", map[string]any{"verb": "describe", "resource": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
			"kubectl describe pod/metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "metrics-agent-767dd6b94f-wvb2z",
			"namespace": "platform"}, "kubectl describe pod/metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"kubectl", map[string]any{"verb": "logs", "name": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
			"kubectl logs metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"kubectl", map[string]any{"verb": "logs", "name": "metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
			"kubectl logs metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"shell", map[string]any{"command": "kubectl logs pod/metrics-agent-767dd6b94f-wvb2z -n platform"},
			"kubectl logs metrics-agent-767dd6b94f-wvb2z -n platform"},
		{"kubectl", map[string]any{"verb": "get", "resource": "deploy,metrics-agent", "namespace": "platform"},
			"kubectl get deployment/metrics-agent -n platform"},
		{"kubectl", map[string]any{"verb": "get", "resource": "deployment", "name": "metrics-agent", "namespace": "platform"},
			"kubectl get deployment/metrics-agent -n platform"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods,services", "namespace": "platform"},
			"kubectl get pods,services -n platform"},
		{"shell", map[string]any{"command": "kubectl describe pod,metrics-agent-767dd6b94f-wvb2z -n platform"},
			"kubectl describe pod,metrics-agent-767dd6b94f-wvb2z -n platform"},
		// A selector keeps its value, in one spelling per flag, with the other extra flags.
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "-l app=shop-web"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "-l=app=shop-web"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "-lapp=shop-web"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "--selector app=shop-web"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "--selector=app=shop-web"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"shell", map[string]any{"command": "kubectl get pods -l app=shop-web -n helm-demo"},
			"kubectl get pod -n helm-demo -l app=shop-web"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "batch",
			"args": "--field-selector status.phase=Pending"}, "kubectl get pod -n batch --field-selector status.phase=Pending"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "batch",
			"args": "--field-selector=status.phase=Pending"}, "kubectl get pod -n batch --field-selector status.phase=Pending"},
		{"kubectl", map[string]any{"verb": "logs", "namespace": "shop", "args": "-l app=checkout --tail=20"},
			"kubectl logs -n shop -l app=checkout --tail=20"},
		{"kubectl", map[string]any{"verb": "get", "resource": "pods", "args": "--sort-by=.metadata.name -l app=web"},
			"kubectl get pod --sort-by=.metadata.name -l app=web"},
		{"shell", map[string]any{"command": "kubectl get pods -l"}, "kubectl get pod -l"},
		// Ruling P3-R65: a command word left after the repeated verb and resource is the object's name.
		{"kubectl", map[string]any{"verb": "get", "resource": "configmap", "args": "get configmap config"},
			"kubectl get configmap/config"},
		{"kubectl", map[string]any{"verb": "get", "resource": "configmap", "name": "config"}, "kubectl get configmap/config"},
		// Ruling P3-R66 (the qwen3.8:27b shake-out). A terraform command parameter that holds several words
		// keys as the command with the rest leading the args, as the tool runs it.
		{"terraform", map[string]any{"command": "state list"}, "terraform state dir=. list"},
		{"terraform", map[string]any{"command": "state", "args": "list"}, "terraform state dir=. list"},
		{"shell", map[string]any{"command": "terraform state list"}, "terraform state dir=. list"},
		{"terraform", map[string]any{"command": "state show aws_instance.web"}, "terraform state dir=. show aws_instance.web"},
		{"terraform", map[string]any{"command": "state show", "args": "aws_instance.web"},
			"terraform state dir=. show aws_instance.web"},
		{"terraform", map[string]any{"command": "state", "args": "show aws_instance.web"},
			"terraform state dir=. show aws_instance.web"},
		{"terraform", map[string]any{"command": "workspace list", "dir": "infra"}, "terraform workspace dir=infra list"},
		{"terraform", map[string]any{"command": "workspace", "dir": "infra", "args": "list"}, "terraform workspace dir=infra list"},
		{"terraform", map[string]any{"command": "providers lock", "args": "-platform=linux_amd64"},
			"terraform providers dir=. lock -platform=linux_amd64"},
		{"terraform", map[string]any{"command": "providers", "args": "lock -platform=linux_amd64"},
			"terraform providers dir=. lock -platform=linux_amd64"},
		// A scan of the working directory keys as the default target, a path inside it relative to it,
		// and a severity list in rising order, as the scan tool reads them. The run directory is found in
		// the path itself (the replay does not pass it); a path outside it keeps its absolute form.
		{"scan", map[string]any{"scanner": "gitleaks"}, "scan gitleaks"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "."}, "scan gitleaks"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "./"}, "scan gitleaks"},
		{"scan", map[string]any{"scanner": "gitleaks",
			"target": "/private/var/folders/np/f7hblt7s1xs3xbn0_450896m0000gn/T/taracode-eval-1192437596"}, "scan gitleaks"},
		{"scan", map[string]any{"scanner": "gitleaks",
			"target": "/private/var/folders/np/f7hblt7s1xs3xbn0_450896m0000gn/T/taracode-eval-1192437596/repo/"},
			"scan gitleaks repo"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "repo"}, "scan gitleaks repo"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "/etc/app"}, "scan gitleaks /etc/app"},
		{"scan", map[string]any{"scanner": "gitleaks", "target": "/private/var/folders/x/T/taracode-eval-env-12/repo"},
			"scan gitleaks /private/var/folders/x/T/taracode-eval-env-12/repo"},
		{"scan", map[string]any{"scanner": "trivy", "target": "python:3.9.0-alpine", "severity": "CRITICAL,HIGH"},
			"scan trivy python:3.9.0-alpine HIGH,CRITICAL"},
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

// TestKubectlSignatureKeysTheArgvTheToolRuns pins the parity of ruling P3-R59: the signature of a
// kubectl call is the canonical form of the argv the kubectl tool actually runs, its normalization of a
// repeated command line included, so a replayed call meets the fixture recorded for the command it
// would run. A fake kubectl prints its arguments one per line.
func TestKubectlSignatureKeysTheArgvTheToolRuns(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(script), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	inputs := []map[string]any{
		{"verb": "get", "resource": "pods", "namespace": "shop"},
		{"verb": "get", "resource": "po", "namespace": "shop", "output": "wide"},
		{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"},
		{"verb": "logs", "name": "checkout-1", "namespace": "shop", "args": "--previous"},
		{"verb": "get", "resource": "events", "namespace": "shop", "args": "--sort-by=.lastTimestamp"},
		{"verb": "get", "resource": "nodes"},
		{"verb": "get", "resource": "deploy", "name": "checkout", "namespace": "shop", "context": "prod-cluster"},
		{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"},
		{"verb": "get", "resource": "deployment", "namespace": "shop",
			"args": "get deployment checkout -n shop --context prod-cluster"},
		{"verb": "get", "args": "get pod report-builder -n analytics"},
		{"verb": "get", "resource": "pods", "args": "kubectl get pods -o wide"},
		{"verb": "describe", "resource": "pod", "name": "x", "namespace": "shop", "args": "describe pod/x -n shop"},
		{"verb": "describe", "resource": "pods", "args": "describe pod/x -n shop"},
		{"verb": "describe", "resource": "deploy/web", "namespace": "shop", "args": "describe deployment web"},
		{"verb": "logs", "name": "web-1", "namespace": "shop", "args": "logs web-1 --tail=50 -n shop"},
		{"verb": "get", "resource": "pods", "namespace": "billing", "args": "--namespace=billing -nbilling"},
		{"verb": "get", "resource": "pods", "context": "kind-dev", "output": "yaml", "args": "--context=kind-dev -oyaml"},
		{"verb": "get", "args": "events -n shop"},
		{"verb": "scale", "resource": "deployment", "name": "checkout", "namespace": "shop",
			"args": "scale deployment checkout --replicas=3 -n shop"},
		{"verb": "get", "resource": "pods", "namespace": "apps", "output": "wide", "args": "-l app=web"},
		// The second shake-out's shapes (ruling P3-R64).
		{"verb": "describe", "resource": "pod invoice-worker-fbf95d7bd-ccb5n", "namespace": "billing"},
		{"verb": "describe", "name": "pod storefront-5996978c75-4pxx5", "namespace": "web"},
		{"verb": "describe pod", "name": "orders-api-674454676b-dmf7d", "namespace": "api"},
		{"verb": "rollout status", "resource": "deployment/cart", "namespace": "shop-v2"},
		{"verb": "describe", "resource": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
		{"verb": "logs", "name": "pod,metrics-agent-767dd6b94f-wvb2z", "namespace": "platform"},
		{"verb": "get", "resource": "deploy,metrics-agent", "namespace": "platform"},
		{"verb": "get", "resource": "pods,services", "namespace": "platform"},
		{"verb": "get", "resource": "pods", "namespace": "helm-demo", "args": "--selector=app=shop-web"},
		{"verb": "get", "resource": "pods", "namespace": "batch", "args": "--field-selector status.phase=Pending -o wide"},
		{"verb": "get", "resource": "configmap", "args": "get configmap config"},
	}
	tool := tools.KubectlTool()
	for _, params := range inputs {
		out, err := tool.Run(context.Background(), params, "")
		if err != nil {
			t.Errorf("%v: %v", params, err)
			continue
		}
		ran := kubectlSignature(strings.Split(out, "\n"))
		if got := Signature("kubectl", params); got != ran {
			t.Errorf("%v:\n signature %q\n tool ran %q", params, got, ran)
		}
	}
	// A call the tool refuses never runs or replays (the gate refuses it first); its signature still
	// starts with the verb it named, so a verb or signature matcher sees what it tried.
	refused := map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout", "namespace": "shop",
		"context": "prod-cluster", "args": "scale deployment checkout --replicas=3 -n prod-shop"}
	if _, err := tool.Run(context.Background(), refused, ""); err == nil {
		t.Fatal("the kubectl tool must refuse a namespace given twice with different values")
	}
	if got := Signature("kubectl", refused); !strings.HasPrefix(got, "kubectl scale deployment/checkout ") {
		t.Errorf("refused call signature %q", got)
	}
	// Another verb with no copy of the verb or the resource before it stays refused (ruling P3-R65).
	anotherVerb := map[string]any{"verb": "get", "args": "describe pods"}
	if _, err := tool.Run(context.Background(), anotherVerb, ""); err == nil {
		t.Fatal("the kubectl tool must refuse args that start with another verb")
	}
	if got := Signature("kubectl", anotherVerb); !strings.HasPrefix(got, "kubectl get ") {
		t.Errorf("refused call signature %q", got)
	}
}

// TestTerraformSignatureKeysTheCommandTheToolRuns pins the parity of ruling P3-R66: the signature of a
// terraform call is the command and arguments terraform actually receives (a command parameter of
// several words split as the tool splits it), for commands the tool passes through as given. A fake
// terraform prints its arguments one per line.
func TestTerraformSignatureKeysTheCommandTheToolRuns(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"
	if err := os.WriteFile(filepath.Join(bin, "terraform"), []byte(script), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	inputs := []map[string]any{
		{"command": "state list"}, {"command": "state", "args": "list"},
		{"command": "state show aws_instance.web"}, {"command": "state show", "args": "aws_instance.web"},
		{"command": "workspace list", "dir": "infra"}, {"command": "workspace show"},
		{"command": "providers lock", "args": "-platform=linux_amd64"}, {"command": "providers schema -json"},
		{"command": "fmt -check"}, {"command": "graph"},
	}
	tool := tools.TerraformTool()
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(work, "infra"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, params := range inputs {
		out, err := tool.Run(context.Background(), params, work)
		if err != nil {
			t.Errorf("%v: %v", params, err)
			continue
		}
		ran := strings.Split(out, "\n")
		if got, want := Signature("terraform", params), terraformSignature(ran[0], str(params, "dir"), ran[1:]); got != want {
			t.Errorf("%v:\n signature %q\n tool ran %q", params, got, want)
		}
	}
}

// TestScanSignatureKeysTheTargetTheToolScans pins the scan half of ruling P3-R66 against a real run
// directory (makeRunDir, the eval's working directory): targets the scan tool scans alike key alike,
// the run directory itself as the default target and a path inside it relative to it.
func TestScanSignatureKeysTheTargetTheToolScans(t *testing.T) {
	runDir, remove, err := makeRunDir("taracode-eval-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(remove)
	if err := os.Mkdir(filepath.Join(runDir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gitleaks"), []byte("#!/bin/sh\necho \"gitleaks $@\"\n"), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := tools.ScanTool("")
	groups := [][]map[string]any{
		{{"scanner": "gitleaks"}, {"scanner": "gitleaks", "target": "."}, {"scanner": "gitleaks", "target": runDir},
			{"scanner": "gitleaks", "target": runDir + "/"}},
		{{"scanner": "gitleaks", "target": "repo"}, {"scanner": "gitleaks", "target": filepath.Join(runDir, "repo")}},
	}
	for _, group := range groups {
		first, err := tool.Run(context.Background(), group[0], runDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, params := range group[1:] {
			ran, err := tool.Run(context.Background(), params, runDir)
			if err != nil || ran != first {
				t.Errorf("%v ran %q (%v), want %q", params, ran, err, first)
			}
			if got, want := Signature("scan", params), Signature("scan", group[0]); got != want {
				t.Errorf("%v:\n signature %q\n want %q", params, got, want)
			}
		}
	}
}
