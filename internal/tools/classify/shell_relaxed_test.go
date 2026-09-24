package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// relaxedReads are the operate-mode over-blocks the Phase 2 reviews documented and Phase 3 relaxes
// (Tasks 4 to 6 fill this table). Every entry runs under the differential harness too.
var relaxedReads = []string{
	// plain-literal assignments for later commands (wave A)
	"x=1; echo $x", "n=3; head -n $n f", "name=web; kubectl get pods -l app=$name", "echo hi; cat $_",
	// substitution operators with a literal default
	"echo ${HOME:-/root}", "ls ${DIR:-.}", "echo ${X-default}", "echo ${Y:+set}",
	// cd as a read; command -v
	"cd /tmp && ls", "cd infra && terraform plan", "pushd sub && cat inner.txt && popd", "dirs",
	"command -v kubectl", "command -V ls", "command -p ls",
	// wave B
	"awk '$3 > 100 {print $1}' access.log", "awk '{ if ($3 > 100) print $1 }' access.log",
	"awk '{ x = $1 >= 2; print x }' f", "awk '/err/ {print /a|b/}' app.log",
	"gcloud run services list", "gcloud run services describe web --region europe-west1",
	"gcloud deploy releases list --delivery-pipeline web", "gcloud logging read 'severity>=ERROR' --limit 10",
	"git config user.email", "git config --global user.email", "git config get user.email", "git config list",
	"ifconfig en0 inet", "ifconfig eth0 inet6",
	// fix round 1: $_ still tracks across a line of plain commands (P3-R6)
	"ls /tmp; cat $_",
	// substitution loops (Task 6, ruling R3)
	"for p in $(ls); do echo $p; done", "echo $(date)", "echo \"today: $(date +%F)\"",
	"for f in $(find . -name '*.tf'); do echo $f; done",
}

// relaxedMutations pin the neighbours of each relaxation: the command that must stay a mutation,
// and the program or variable its verb or reason must name.
var relaxedMutations = map[string]string{
	"PATH=/tmp ls": "PATH", "LD_PRELOAD=x cat f": "LD_PRELOAD", "GIT_DIR=/x git log": "GIT_DIR",
	"IFS=: cat f": "IFS", "TF_CLI_CONFIG_FILE=x terraform plan": "TF_CLI_CONFIG_FILE", "MY_BIN=/tmp/x ls": "MY_BIN",
	"opt=-delete; find . $opt": "find", "find . ${OPT:--delete}": "find", "o=-o; sort $o out f": "sort",
	"ls -la; cat $_": "cat", "command -p rm x": "rm", "echo ${X:=y}": "echo",
	"awk '{print $1 > \"out\"}' x": "awk", "awk '{printf \"%s\\n\", $1 > \"f\"}' x": "awk", "awk '{print > \"out\"}' x": "awk",
	"gcloud run deploy web --image x": "deploy", "gcloud run services delete web": "delete",
	"gcloud logging write mylog hello": "write", "gcloud delete describe-x": "delete",
	"git config user.email me@example.com": "config", "git config --unset user.email": "config",
	"git config -e": "config", "git config set user.email me@example.com": "config",
	"ifconfig en0 inet 10.0.0.2": "ifconfig", "ifconfig en0 down": "ifconfig",
	// fix round 1, C1: a control word, a case head, a brace group or a parenthesis poisons $_
	"for x in 1; do echo -delete; done; find . $_": "find", "if true; then echo -delete; fi; find . $_": "find",
	"{ echo -delete; }; find . $_": "find", "case a in a) echo -delete;; esac; find . $_": "find",
	"echo -delete; (true); find . $_": "find", "echo -delete; case a in *) find . $_;; esac": "find",
	// fix round 1, C2: a reference immediately followed by "-" injects, since it can expand to nothing
	"find . ${X:-}-delete": "find", "find . ${X-}-delete": "find", "find . ${X:+}-delete": "find",
	"find . ${X:+a}-delete": "find", "find . $X-delete": "find",
	// fix round 1, C3: an assignment-only segment's own value is checked too; caught at the assignment
	// (x=...) before "find" is even reached, so the neighbour names x, the assignment's program
	"x=${y:=-delete}; find . $y": "x", "x=${y=-delete}; find . $y": "x",
	// fix round 1, C4: note never downgrades a variable to a less dangerous kind
	"x=-delete; x=1 true; find . $x": "find", "x=-delete; for x in; do true; done; find . $x": "find",
	"x=-delete; for x; do true; done; find . $x": "find",
	// fix round 1, I2: more variables that smuggle an option into an allowlisted reader
	"echo x | LESS=-O/tmp/x less": "LESS", "PYTHONUSERBASE=/tmp/x aws s3 ls": "PYTHONUSERBASE",
	"GNUPGHOME=/tmp/x git log --show-signature": "GNUPGHOME", "WGETRC=/tmp/x wget -qO- https://example.com": "WGETRC",
	// substitution loops (Task 6, ruling R3): a body that writes, runs or names a cluster, a backtick,
	// and a substitution feeding any program that is not a for-list or an option-harmless one
	"for p in $(ls); do cat $p; done": "cat", "cat $(ls)": "cat", "for p in $(rm -rf x); do echo $p; done": "rm",
	"echo $(kubectl delete pod x)": "kubectl", "echo `date`": "substitution", "kubectl get pods -n $(cat ns)": "kubectl",
	"echo $(cat f > g)": "redirect",
}

func TestRelaxedReads(t *testing.T) {
	for _, c := range relaxedReads {
		if got := Shell(c); got.Classification != policy.Read {
			t.Errorf("%q should be a read: %+v", c, got)
		}
	}
}

func TestRelaxedMutations(t *testing.T) {
	for c, want := range relaxedMutations {
		got := Shell(c)
		if got.Classification != policy.Mutate {
			t.Errorf("%q should be a mutation", c)
			continue
		}
		if got.Verb != want && !strings.Contains(got.Reason, want) {
			t.Errorf("%q: verb %q and reason %q should name %q", c, got.Verb, got.Reason, want)
		}
	}
}
