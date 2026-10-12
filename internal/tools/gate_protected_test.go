package tools

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// gateFixture is the working directory of the gate cases and the policy they run under: the built-in
// protections, one exact cloud account and one exact host.
func gateFixture(t *testing.T) (string, policy.Policy) {
	t.Helper()
	wd := t.TempDir()
	for _, f := range []string{"terraform.tfstate", "infra/x.tfstate", ".git/config", "build/out.bin",
		"node_modules/pkg/x.tfstate"} {
		path := filepath.Join(wd, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("terraform.tfstate", filepath.Join(wd, "s.json")); err != nil {
		t.Fatal(err)
	}
	p := policy.Default()
	p.Protected.CloudAccounts = []string{"acme-prod-acct"}
	p.Protected.Hosts = []string{"db.prod.example.com"}
	return wd, p
}

// gateVerdict runs one call through its tool's classifier and the policy in operate mode, as the agent does.
func gateVerdict(p policy.Policy, tool *Tool, args map[string]any, wd string) policy.Verdict {
	inv := tool.Classify(args, wd)
	inv.Tool, inv.WorkingDir = tool.Name, wd
	return p.Evaluate(policy.ModeOperate, inv)
}

// TestTheGateKeepsProtectedTargetsForEveryCommand: every way of reaching a protected target that a review
// and the board's evals found is refused, and the commands that name nothing protected still run.
func TestTheGateKeepsProtectedTargetsForEveryCommand(t *testing.T) {
	wd, p := gateFixture(t)
	shell := ShellTool(io.Discard)
	deny := []string{
		`python3 -c "open('terraform.tfstate','w').write('{}')"`,
		`perl -e 'open(F, ">terraform.tfstate"); print F "{}"'`,
		`awk 'BEGIN { print "{}" > "terraform.tfstate" }'`,
		`sh -c "echo {} > terraform.tfstate"`,
		`eval 'echo {} > terraform.tfstate'`,
		"echo `rm -f terraform.tfstate`",
		`echo "$(rm -f terraform.tfstate)"`,
		`echo terraform.tfstate | xargs truncate -s 0`,
		"python3 <<'EOF'\nopen('terraform.tfstate', 'w').write('{}')\nEOF",
		`perl -i -pe 's/.*//' terraform.tfstate`,
		`ls -la terraform.tfstate* 2>/dev/null; echo "---"; head -c 500 terraform.tfstate 2>/dev/null; echo; ` +
			`python3 -c "import json;json.load(open('terraform.tfstate'))" 2>&1 | tail -2`,
		`sh -c "kubectl --context prod delete ns x"`,
		`python3 -c "import subprocess; subprocess.run(['kubectl', '--context', 'prod', 'delete', 'ns', 'x'])"`,
		`aws --profile=acme-prod-acct s3 rm s3://b/k`,
		`AWS_PROFILE=acme-prod-acct aws s3 rm s3://b/k`,
		`ssh admin@db.prod.example.com reboot`,
		`aws --profile acme-prod-'acct' s3 rm s3://b/k`,
		`ssh admin@db.prod.exa''mple.com reboot`,
		`ssh admin@db.prod.example.com. reboot`,
		`curl -X DELETE https://db.prod.example.com/x`,
		`cd . && terraform destroy`,
		`terraform -chdir=. destroy`,
		`true && terraform destroy -auto-approve`,
		`sh -c "terraform destroy"`,
		`rm -rf .git`,
		`mv .git /tmp/x`,
		`rm -rf infra`,
		`rm -rf .`,
		`sh -c "rm -rf infra"`,
		`find . -name config -delete`,
		`find . -path '*/.git/*' -delete`,
		`find . -name '*.tfstate' -delete`,
		`find . -type d -name infra -exec rm -rf {} +`,
		`find -L . -name '*.pyc' -delete`,
		`cp /tmp/x.tfstate infra`,
		`cp /tmp/x.tfstate inf*`,
		`find . -name '*.pyc' -exec rm -rf infra \;`,
		`find . -name '*.tf' -exec terraform destroy \;`,
		`find . -name '*.pyc' -exec sh -c 'rm -rf "$(dirname "$1")"' _ {} \;`,
		`find . -name '*.pyc' -execdir rm -rf infra \;`,
		"echo $((1<<20))\necho {} > terraform.tfstate",
		"echo $((1<<20))\nterraform destroy -auto-approve",
		"echo $((1<<20))\nkubectl --context prod delete ns kube-system",
		"echo $(( \"(\" ))\nterraform destroy -auto-approve\n# ))",
		`find -x . -delete`,
		`find -name config -delete`,
		`find -f . -name config -delete`,
	}
	for _, cmd := range deny {
		if v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd); v.Allow {
			t.Errorf("allowed, want refused: %s", cmd)
		}
	}
	for _, path := range []string{"TERRAFORM.TFSTATE", "s.json"} {
		if v := gateVerdict(p, writeFileTool(), map[string]any{"path": path, "content": "{}"}, wd); v.Allow {
			t.Errorf("write_file %s allowed, want refused", path)
		}
	}
	allow := []string{
		`make`,
		`make deploy`,
		`python3 manage.py migrate`,
		`python3 -c "print(1)"`,
		`python3 -c "import os; print(os.listdir('.'))"`,
		`sh -c "echo hi > out.txt"`,
		`rm -rf build`,
		`rm -rf node_modules`,
		`find . -name '*.pyc' -delete`,
		`find . -type d -name __pycache__ -exec rm -rf {} +`,
		`find . -name node_modules -type d -prune -exec rm -rf {} +`,
		`cp terraform.tfstate terraform.tfstate.backup`,
		`find . -name '*.py' -exec sed -i 's/a/b/' {} +`,
		`cat terraform.tfstate`,
		`kubectl --context dev -n apps delete pod x`,
		`aws --profile=acme-dev s3 rm s3://b/k`,
	}
	for _, cmd := range allow {
		if v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd); !v.Allow {
			t.Errorf("refused, want allowed: %s (%s)", cmd, v.Reason)
		}
	}
}
