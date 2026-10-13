package policy

import (
	"strings"
	"testing"
)

// TestCanonicalTerraformCommandSpellsADestroyOneWay: terraform, tofu and terragrunt read as terraform, the
// global options and terragrunt's run words before the verb are dropped, and apply or plan with a true
// destroy flag reads as destroy. Everything else stays as written.
func TestCanonicalTerraformCommandSpellsADestroyOneWay(t *testing.T) {
	cases := []struct{ command, want string }{
		{"terraform destroy -auto-approve", "terraform destroy -auto-approve"},
		{"terraform apply -destroy -auto-approve", "terraform destroy -auto-approve"},
		{"terraform apply -auto-approve -destroy", "terraform destroy -auto-approve"},
		{"terraform apply --destroy", "terraform destroy"},
		{"terraform apply -destroy=true", "terraform destroy"},
		{"terraform apply -destroy=1 -lock=false", "terraform destroy -lock=false"},
		{"terraform apply -destroy=t", "terraform destroy"},
		{"terraform apply -destroy=false", "terraform apply -destroy=false"},
		{"terraform apply -destroy -destroy=false", "terraform apply -destroy -destroy=false"},
		{"terraform apply -destroy=false -destroy", "terraform destroy"},
		{"terraform apply -destroy=maybe", "terraform apply -destroy=maybe"},
		{"terraform plan -destroy -out=tfplan", "terraform destroy -out=tfplan"},
		{"terraform plan -out=tfplan", "terraform plan -out=tfplan"},
		{"terraform -chdir=infra apply -destroy", "terraform destroy"},
		{"terraform -chdir=infra destroy", "terraform destroy"},
		{"/opt/homebrew/bin/terraform apply -destroy", "terraform destroy"},
		{"tofu destroy -auto-approve", "terraform destroy -auto-approve"},
		{"tofu apply -destroy", "terraform destroy"},
		{"terragrunt destroy", "terraform destroy"},
		{"terragrunt run-all destroy", "terraform destroy"},
		{"terragrunt run --all destroy -auto-approve", "terraform destroy -auto-approve"},
		{"terragrunt run-all apply -destroy", "terraform destroy"},
		{"cd infra && terraform apply -destroy", "cd infra && terraform destroy"},
		{"terraform apply tfplan", "terraform apply tfplan"},
		{"terraform", "terraform"},
		{"terraform -chdir=infra", "terraform"},
		{"kubectl delete ns x", "kubectl delete ns x"},
		{"make deploy", "make deploy"},
		{"", ""},
	}
	for _, c := range cases {
		if got := canonicalTerraformCommand(c.command); got != c.want {
			t.Errorf("canonicalTerraformCommand(%q) = %q, want %q", c.command, got, c.want)
		}
	}
}

// TestDenyPatternsMatchTheCanonicalTerraformCommand: the shipped "terraform destroy*" refuses a destroy by
// every spelling, through the canonical form, and leaves an apply and a plan alone.
func TestDenyPatternsMatchTheCanonicalTerraformCommand(t *testing.T) {
	denied := []string{
		"terraform apply -destroy -auto-approve",
		"terraform apply -auto-approve -destroy",
		"terraform apply -destroy=true",
		"terraform apply --destroy",
		"terraform -chdir=infra apply -destroy",
		"Terraform Apply -Destroy",
		"tofu apply -destroy",
		"tofu destroy -auto-approve",
		"terragrunt destroy",
		"terragrunt run-all destroy",
		"terragrunt run --all destroy",
		"terraform plan -destroy -out=tfplan",
	}
	for _, command := range denied {
		v := Default().Evaluate(ModeOperate, mutate("shell", "", command, Targets{}))
		if v.Allow || v.Rule != "deny.commands" || !strings.Contains(v.Reason, "terraform destroy*") {
			t.Errorf("%q: %+v, want the destroy deny pattern", command, v)
		}
	}
	allowed := []string{
		"terraform apply",
		"terraform apply -destroy=false",
		"terraform apply tfplan",
		"terraform plan -out=tfplan",
		"terraform plan -destroy=false -out=tfplan",
		"tofu plan -out=tfplan",
	}
	for _, command := range allowed {
		if v := Default().Evaluate(ModeOperate, mutate("terraform", "apply", command, Targets{})); !v.Allow {
			t.Errorf("%q: %+v, want no deny pattern", command, v)
		}
	}
	if v := Default().Evaluate(ModeOperate, Invocation{Tool: "shell", Classification: Read, Command: "terraform plan -destroy"}); !v.Allow || v.Rule != "read" {
		t.Errorf("a plan without -out is a read and never reaches the patterns: %+v", v)
	}
	v := Default().Evaluate(ModeOperate, mutate("shell", "", "terraform apply -destroy -auto-approve", Targets{}))
	if !strings.Contains(v.Reason, `"terraform apply -destroy -auto-approve"`) || !strings.Contains(v.Reason, `"terraform destroy -auto-approve"`) {
		t.Errorf("the reason names the command as written and as read: %q", v.Reason)
	}
	own := Policy{Deny: DenyRules{Commands: []string{"tofu destroy*"}}}
	if v := own.Evaluate(ModeOperate, mutate("shell", "", "terraform apply -destroy -auto-approve", Targets{})); v.Allow {
		t.Errorf("a pattern is read in the canonical form too: %+v", v)
	}
}
