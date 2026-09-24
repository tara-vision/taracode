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
}

// relaxedMutations pin the neighbours of each relaxation: the command that must stay a mutation,
// and the program or variable its verb or reason must name.
var relaxedMutations = map[string]string{
	"PATH=/tmp ls": "PATH", "LD_PRELOAD=x cat f": "LD_PRELOAD", "GIT_DIR=/x git log": "GIT_DIR",
	"IFS=: cat f": "IFS", "TF_CLI_CONFIG_FILE=x terraform plan": "TF_CLI_CONFIG_FILE", "MY_BIN=/tmp/x ls": "MY_BIN",
	"opt=-delete; find . $opt": "find", "find . ${OPT:--delete}": "find", "o=-o; sort $o out f": "sort",
	"ls -la; cat $_": "cat", "command -p rm x": "rm", "echo ${X:=y}": "echo",
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
