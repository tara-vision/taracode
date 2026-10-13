package tools

import (
	"io"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestTheGateRefusesADestroyByEverySpelling: the shipped deny pattern holds through the shell tool's real
// classifier for every spelling of a teardown the board's evals and the review found, and a plan that only
// shows what would go still runs.
func TestTheGateRefusesADestroyByEverySpelling(t *testing.T) {
	wd, p := gateFixture(t)
	shell := ShellTool(io.Discard)
	deny := []string{
		`terraform apply -destroy -auto-approve`,
		`terraform apply -auto-approve -destroy`,
		`terraform apply -destroy=true`,
		`terraform apply --destroy`,
		`terraform -chdir=infra apply -destroy`,
		`command terraform apply -destroy`,
		`sh -c "terraform apply -destroy -auto-approve"`,
		`echo infra | xargs terraform -chdir=infra apply -destroy`,
		`tofu apply -destroy`,
		`tofu destroy -auto-approve`,
		`terragrunt destroy`,
		`terragrunt run-all destroy`,
		`terragrunt run --all destroy`,
		`terragrunt --working-dir infra destroy -auto-approve`,
		`terragrunt --log-level debug destroy`,
		`terragrunt run --all --queue-include-dir app destroy`,
		`terragrunt stack run destroy`,
		`terragrunt destroy-all`,
		`terraform plan -destroy -out=tfplan`,
		`cd infra && terraform plan -destroy -out=tfplan`,
		`TF_CLI_ARGS_apply=-destroy terraform apply -auto-approve`,
		`export TF_CLI_ARGS_apply=-destroy; terraform apply -auto-approve`,
	}
	for _, cmd := range deny {
		if v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd); v.Allow || v.Rule != "deny.commands" {
			t.Errorf("%s: %+v, want the deny pattern", cmd, v)
		}
	}
	allow := []string{
		`terraform plan -destroy`,
		`terraform plan -out=tfplan`,
		`tofu plan -out=tfplan`,
		`terraform state list`,
	}
	for _, cmd := range allow {
		if v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd); !v.Allow {
			t.Errorf("%s: refused, want allowed (%s)", cmd, v.Reason)
		}
	}
	_ = policy.ModeOperate
}

// TestTheGateHoldsAShellApplyToTheSessionPlan: while the policy requires a plan from this session, a terraform
// apply through the shell (which runs the line as given, with no plan) is refused and steered to the terraform
// tool; with the requirement off it goes on to the prompt, and the other applies through the shell are unchanged.
func TestTheGateHoldsAShellApplyToTheSessionPlan(t *testing.T) {
	wd, p := gateFixture(t)
	shell := ShellTool(io.Discard)
	held := []string{
		`terraform apply`,
		`terraform apply -auto-approve`,
		`terraform apply tfplan`,
		`terraform apply -destroy=false`,
		`terraform -chdir=infra apply -auto-approve`,
		`cd infra && terraform apply -auto-approve`,
		`sh -c "terraform apply -auto-approve"`,
		`TF_CLI_ARGS_apply=-lock=false terraform apply -auto-approve`,
		`export TF_CLI_ARGS_apply=-lock=false; terraform apply -auto-approve`,
		`tofu apply -auto-approve`,
		`terragrunt run-all apply`,
		`terragrunt --working-dir infra apply -auto-approve`,
		`terragrunt apply-all`,
		`echo infra | xargs -I{} terraform -chdir={} apply -auto-approve`,
	}
	for _, cmd := range held {
		v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd)
		if v.Allow || v.Rule != "require_dry_run" || !strings.Contains(v.Reason, "terraform tool") {
			t.Errorf("%s: %+v, want the plan requirement", cmd, v)
		}
	}
	off := p
	f := false
	off.RequireDryRun.TerraformApply = &f
	for _, cmd := range held {
		if v := gateVerdict(off, shell, map[string]any{"command": cmd}, wd); !v.Allow || v.Rule != "policy" {
			t.Errorf("%s with terraform_apply off: %+v, want an ordinary mutation", cmd, v)
		}
	}
	// A destroy set through terraform's environment is a destroy for the deny pattern, with the plan
	// requirement off as well.
	for _, cmd := range []string{
		`TF_CLI_ARGS_apply=-destroy tofu apply -auto-approve`,
		`TF_CLI_ARGS=-destroy terraform apply -auto-approve`,
		`TF_CLI_ARGS_plan=-destroy terraform plan -out=x`,
		`export TF_CLI_ARGS_apply=-destroy; terraform apply -auto-approve`,
	} {
		if v := gateVerdict(off, shell, map[string]any{"command": cmd}, wd); v.Allow || v.Rule != "deny.commands" {
			t.Errorf("%s with terraform_apply off: %+v, want the deny pattern", cmd, v)
		}
	}
	unchanged := []string{
		`terraform -chdir=infra`,
		`echo terraform apply > notes.txt`,
		`grep -c "terraform apply" deploy.log > count.txt`,
		`kubectl --context dev -n apps apply -f x.yaml`,
		`helm --kube-context dev -n apps upgrade web ./chart`,
		`terraform plan -out=tfplan`,
		`terraform init`,
	}
	for _, cmd := range unchanged {
		if v := gateVerdict(p, shell, map[string]any{"command": cmd}, wd); !v.Allow || v.Rule != "policy" {
			t.Errorf("%s: %+v, want an ordinary mutation", cmd, v)
		}
	}
}
