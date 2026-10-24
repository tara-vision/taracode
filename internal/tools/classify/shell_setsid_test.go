package classify

import (
	"slices"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestSetsidIsAWrapperLikeNohup: setsid runs the command that follows it in a session of its own (the shell
// tool's way to let a process outlive a call), so the line is classified by that command, its flags skipped,
// and a file writer behind it keeps its paths.
func TestSetsidIsAWrapperLikeNohup(t *testing.T) {
	if got := Shell("setsid kubectl get pods"); got.Classification != policy.Read {
		t.Errorf("setsid kubectl get pods: %+v", got)
	}
	if got := Shell("setsid -f -w cat /etc/hostname"); got.Classification != policy.Read {
		t.Errorf("setsid -f -w cat: %+v", got)
	}
	got := Shell("setsid rm -rf /srv/protected")
	if got.Classification != policy.Mutate || !slices.Contains(got.Paths, "/srv/protected") {
		t.Errorf("setsid rm: %+v", got)
	}
	if got := Shell("setsid"); got.Classification != policy.Read {
		t.Errorf("a bare setsid runs nothing: %+v", got)
	}
}

// TestBackgroundJobReasonSaysWhatItIs: a background job no longer outlives the call (the shell tool ends it), so
// the reason a line with & is a mutation names what is true: it runs unobserved alongside the command.
func TestBackgroundJobReasonSaysWhatItIs(t *testing.T) {
	got := Shell("sleep 5 & echo hi")
	if got.Classification != policy.Mutate || !strings.Contains(got.Reason, "runs unobserved") {
		t.Errorf("%+v", got)
	}
}

// TestSetsidSkipsADoubleDashAndCombinedFlags: `setsid -- kubectl get pods` and `setsid -fw cat` are the wrapped
// command, like `setsid -f -w cat`.
func TestSetsidSkipsADoubleDashAndCombinedFlags(t *testing.T) {
	for _, line := range []string{"setsid -- kubectl get pods", "setsid -fw cat /etc/hostname", "setsid -f -- cat /etc/hostname"} {
		if got := Shell(line); got.Classification != policy.Read {
			t.Errorf("%q: %+v", line, got)
		}
	}
}
