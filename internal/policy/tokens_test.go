package policy

import "testing"

func TestAccountsAndHostsMatchThePiecesOfAWord(t *testing.T) {
	p := Default()
	p.Protected.CloudAccounts = []string{"acme-prod-acct"}
	p.Protected.Hosts = []string{"db.prod.example.com", "*.bank.internal"}
	cases := []struct{ command, rule string }{
		{"aws --profile=acme-prod-acct s3 rm s3://b/k", "protected.cloud_accounts"},
		{"AWS_PROFILE=acme-prod-acct aws s3 rm s3://b/k", "protected.cloud_accounts"},
		{"ssh admin@db.prod.example.com reboot", "protected.hosts"},
		{"scp f db.prod.example.com:/tmp/", "protected.hosts"},
		{"curl -X DELETE https://db.prod.example.com/x", "protected.hosts"},
		{"curl -X DELETE https://core.bank.internal/x", "protected.hosts"},
	}
	for _, c := range cases {
		if v := p.Evaluate(ModeOperate, mutate("shell", "", c.command, Targets{})); v.Allow || v.Rule != c.rule {
			t.Errorf("%s: got %+v, want %s", c.command, v, c.rule)
		}
	}
	for _, command := range []string{"aws --profile=acme-dev s3 rm s3://b/k", "ssh admin@db.dev.example.com reboot"} {
		if v := p.Evaluate(ModeOperate, mutate("shell", "", command, Targets{})); !v.Allow {
			t.Errorf("%s: got %+v, want it allowed", command, v)
		}
	}
}
