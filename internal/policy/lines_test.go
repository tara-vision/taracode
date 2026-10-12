package policy

import "testing"

func TestDenyPatternsReadTheLinesOfACommand(t *testing.T) {
	p := Default()
	inv := mutate("shell", "", "cd . && terraform destroy", Targets{})
	if v := p.Evaluate(ModeOperate, inv); !v.Allow {
		t.Fatalf("the whole line alone: %+v", v)
	}
	inv.Lines = []string{"cd .", "terraform destroy"}
	if v := p.Evaluate(ModeOperate, inv); v.Allow || v.Rule != "deny.commands" {
		t.Fatalf("with its lines: %+v, want deny.commands", v)
	}
}
