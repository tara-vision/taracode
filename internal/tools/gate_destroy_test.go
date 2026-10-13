package tools

import (
	"io"
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
		`terraform plan -destroy -out=tfplan`,
		`cd infra && terraform plan -destroy -out=tfplan`,
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
