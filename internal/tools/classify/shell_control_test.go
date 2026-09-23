package classify

import (
	"strings"
	"testing"
)

// TestShellControlWordsAndGrouping (pre-tag round B): the program of a segment comes after the shell's
// reserved words and grouping (do, then, else, elif, if, while, until, !, {, a subshell's
// parentheses, a case pattern), so a loop or a group is classified by what it runs and its written
// paths reach the policy. A segment that only opens or closes a construct runs nothing.
func TestShellControlWordsAndGrouping(t *testing.T) {
	checkReads(t, []string{
		"for f in a b; do cat $f; done", "if true; then ls; fi", "(ls -la)", "{ ls; }", "! grep -q x f.txt",
		"while false; do ls; done", "until true; do ls; done", "if ! grep -q x f; then echo no; else echo yes; fi",
		"for f in *.go; do wc -l $f; done", "case x in a) ls;; esac", "(ls) 2> /dev/null", "(ls | grep x)",
	})
	checkMutations(t, []hardeningCase{
		{"for p in a b; do kubectl delete pod $p -n kube-system; done", "delete"},
		{"(kubectl -n kube-system delete pod x)", "delete"},
		{"{ kubectl -n kube-system delete pod x; }", "delete"},
		{"if true; then kubectl -n kube-system delete pod x; fi", "delete"},
		{"! kubectl -n kube-system delete pod x", "delete"},
		{"for f in a; do rm .git/config; done", "rm"},
		{"(rm .git/config)", "rm"},
		{"{ rm .git/config; }", "rm"},
		{"(sleep 100) &", "background"},
		{"(ls) > out.txt", "out.txt"},
		{"{ ls; } > out.txt", "out.txt"},
		{"{ls", "{ls"},
		{"coproc kubectl delete pod x", "coproc"},
	})
	paths := []struct{ cmd, want string }{
		{"for f in a; do rm .git/config; done", ".git/config"},
		{"(rm .git/config)", ".git/config"},
		{"{ rm .git/config; }", ".git/config"},
		{"if true; then rm .git/config; fi", ".git/config"},
		{"! rm .git/config", ".git/config"},
		{"{rm .git/config", ".git/config"},
		{"case x in a) rm .git/config;; esac", ".git/config"},
		{"(ls) > out.txt", "out.txt"},
		{"for f in a; do cat $f; done > out.txt", "out.txt"},
	}
	for _, c := range paths {
		if got := strings.Join(Shell(c.cmd).Paths, "|"); got != c.want {
			t.Errorf("%q: paths %q, want %q", c.cmd, got, c.want)
		}
	}
}
