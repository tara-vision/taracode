package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

var shellReadCases = []string{
	"ls -la", "cat /etc/hosts", "grep -rn TODO . | head -20", "ps aux | grep nginx | wc -l",
	"df -h && free -m", "kubectl get pods -A | grep -v Running", "git status; git log -n 3",
	"curl -s https://example.com/health", "curl -sI https://example.com", "dig +short example.com",
	"find . -name '*.tf' -type f", "sed -n '1,20p' main.go", "awk '{print $1}' access.log | sort | uniq -c",
	"tar -tzf release.tgz", "make -n build", "terraform plan -no-color", "docker compose ps",
	"systemctl status nginx", "journalctl -u nginx --since '1 hour ago'", "gh pr list --state open",
	"jq '.items[].metadata.name' out.json", "TZ=UTC env | grep TZ", "helm list -A", "echo hello 2>&1",
	"cat big.log > /dev/null", "python3 --version", "go version", "brew list", "npm ls --depth=0",
	"cat<x", "wget -O - https://example.com/x",
}

func TestShell(t *testing.T) {
	for _, c := range shellReadCases {
		if got := Shell(c); got.Classification != policy.Read {
			t.Errorf("%q should be read: %+v", c, got)
		}
	}
	mutate := map[string]string{
		"rm -rf build":                             "rm",
		"make build":                               "make",
		"echo x > out.txt":                         "redirect",
		"cat a >> b":                               "redirect",
		"ls>out.txt":                               "redirect",
		"cat a>>b":                                 "redirect",
		"sudo systemctl restart nginx":             "sudo",
		"cat $(whoami)":                            "cat",
		"echo `date`":                              "substitution",
		"sleep 30 &":                               "background",
		"kubectl delete pod x":                     "kubectl delete",
		"git push origin main":                     "git push",
		"find . -name '*.tmp' -delete":             "find",
		"find . -exec rm {} \\;":                   "find",
		"sed -i 's/a/b/' x.txt":                    "sed",
		"awk '{print > \"out\"}' x":                "awk",
		"curl -X POST https://x -d a=b":            "curl",
		"curl -o file https://x":                   "curl",
		"python3 -c 'import os; os.remove(\"x\")'": "python3",
		"ls | xargs rm":                            "xargs",
		"tee out.log":                              "tee",
		"npm install":                              "npm",
		"terraform apply":                          "terraform apply",
		"gh api -X DELETE repos/x/y":               "gh",
		"docker run x":                             "docker run",
		"unknowntool --flag":                       "unknowntool",
		"wget https://x":                           "wget",
		"PATH=/x env | grep PATH":                  "PATH",
	}
	for c, want := range mutate {
		got := Shell(c)
		if got.Classification != policy.Mutate {
			t.Errorf("%q should be mutate", c)
			continue
		}
		if !containsFold(got.Reason, want) && !containsFold(got.Verb, want) {
			t.Errorf("%q: reason %q verb %q should mention %q", c, got.Reason, got.Verb, want)
		}
	}
	if got := Shell(`echo "unterminated`); got.Classification != policy.Mutate {
		t.Error("unparseable commands are mutate")
	}
	if got := Shell("curl https://api.example.com/x | jq ."); len(got.Hosts) != 1 || got.Hosts[0] != "api.example.com" {
		t.Errorf("hosts %v", got.Hosts)
	}
}

func containsFold(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	ls, lsub := []rune(strings.ToLower(s)), []rune(strings.ToLower(sub))
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if string(ls[i:i+len(lsub)]) == string(lsub) {
			return i
		}
	}
	return -1
}
