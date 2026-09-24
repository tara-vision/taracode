package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// relaxedReads are the operate-mode over-blocks the Phase 2 reviews documented and Phase 3 relaxes
// (Tasks 4 to 6 fill this table). Every entry runs under the differential harness too.
var relaxedReads = []string{}

// relaxedMutations pin the neighbours of each relaxation: the command that must stay a mutation,
// and the program or variable its verb or reason must name.
var relaxedMutations = map[string]string{}

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
