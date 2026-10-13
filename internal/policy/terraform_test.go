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
		{"terragrunt --working-dir infra destroy -auto-approve", "terraform destroy -auto-approve"},
		{"terragrunt --log-level debug destroy", "terraform destroy"},
		{"terragrunt run --all --queue-include-dir app destroy", "terraform destroy"},
		{"terragrunt stack run destroy", "terraform destroy"},
		{"terragrunt --working-dir infra apply -destroy", "terraform destroy"},
		{"terragrunt hcl fmt", "terraform fmt"},
		{"terragrunt catalog", "terraform"},
		{"terraform -var region=apply plan -out=x", "terraform plan -out=x"},
		{"tf_cli_args_apply=-destroy terraform apply -auto-approve", "terraform destroy -auto-approve"},
		{"tf_cli_args=-destroy tofu apply", "terraform destroy"},
		{"tf_cli_args_plan=-destroy terraform plan -out=x", "terraform destroy -out=x"},
		{"export tf_cli_args_apply=-destroy; terraform apply", "terraform destroy"},
		{"tf_cli_args_apply=\"-destroy -auto-approve\" terraform apply", "terraform destroy"},
		{"tf_cli_args_apply=-destroy=false terraform apply", "tf_cli_args_apply=-destroy=false terraform apply"},
		{"tf_cli_args_apply=-destroy terraform init", "tf_cli_args_apply=-destroy terraform init"},
		{"tf_var_destroy=true terraform apply", "tf_var_destroy=true terraform apply"},
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

// TestAPatternKeepsItsOwnWordsInTheCanonicalForm: a deny pattern is read with its program name and its destroy
// flag canonical, but the words the operator wrote between the program and the verb stay, so a pattern
// narrower than the shipped one refuses only what it names.
func TestAPatternKeepsItsOwnWordsInTheCanonicalForm(t *testing.T) {
	narrow := Policy{Deny: DenyRules{Commands: []string{"terraform -chdir=*prod* apply*"}}}
	if v := narrow.Evaluate(ModeOperate, mutate("shell", "", "terraform -chdir=prod-eu apply", Targets{})); v.Allow {
		t.Errorf("the pattern refuses what it names: %+v", v)
	}
	if v := narrow.Evaluate(ModeOperate, mutate("terraform", "apply", "terraform apply", Targets{Paths: []string{"/w/dev"}})); !v.Allow {
		t.Errorf("the pattern leaves another directory's apply alone: %+v", v)
	}
	runAll := Policy{Deny: DenyRules{Commands: []string{"terragrunt run-all *"}}}
	if v := runAll.Evaluate(ModeOperate, mutate("shell", "", "terraform init", Targets{})); !v.Allow {
		t.Errorf("a terragrunt pattern leaves terraform alone: %+v", v)
	}
	if v := runAll.Evaluate(ModeOperate, mutate("shell", "", "terragrunt run-all init", Targets{})); v.Allow {
		t.Errorf("the terragrunt pattern refuses what it names: %+v", v)
	}
	if got := canonicalTerraformPattern("tofu -chdir=*prod* apply -destroy -auto-approve*"); got != "terraform -chdir=*prod* destroy -auto-approve*" {
		t.Errorf("canonicalTerraformPattern = %q", got)
	}
}
