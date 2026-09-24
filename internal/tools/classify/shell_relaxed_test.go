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
	"x=1; echo $x", "n=3; head -n $n f", "name=web; kubectl get pods -l app=$name",
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
	// substitution loops (Task 6, ruling R3)
	"for p in $(ls); do echo $p; done", "echo $(date)", "echo \"today: $(date +%F)\"",
	"for f in $(find . -name '*.tf'); do echo $f; done",
	// Task 6 fix round 1, minor: ":" is the null utility (a read), so it may receive a substitution
	": $(date)",
	// Task 6 fix round 2, ruling P3-R23: a # glued after a subshell's ) is a comment, so at top level
	// (true)#rm ... runs only (true) and the comment hides rm; verified a read on bash 3.2/5.3, dash, zsh
	"(true)#rm -rf /tmp/x",
	// fix round 2, P3-R14(a): literal text in front of a reference means the word can never start
	// with "-", however the reference resolves; a plain brace expansion with no $ stays a read too
	"kubectl get pods -l app=$APP-api", "echo {a,b}-x",
	// fix round 3, P3-R20: literal text in front of a whole leading run of references (not just one)
	// still keeps the word safe, and a brace prefix keeps a rebuilt name (x{a,$X}) from starting with -
	"TAG=$VERSION-rc1 echo x", "find . x{a,$X}-delete",
	// fix round 4, P3-R31: a brace sequence with no assignment rebuilds no line variable, and one set
	// to a plain value is safe to expand
	"sort $a{1..9} out f", "a5=x; sort $a{1..9} out f",
	// fix round 4, P3-R32: a ${...} with a balanced brace pair (no stripped, unmatched "}") is a read
	"find . -name ${X}",
	// fix round 4, minors: an unset or plain variable, and a brace expansion that rebuilds a plain or
	// (under an option-harmless program) an option-valued line variable, stay reads
	"ls $X", "cat $HOME/x", "find . -name $X",
	"ab=x; sort $a{b,x} out f", "a=1; sort $a{b,x} out f", "ab=-o; echo $a{b,x}",
	// fix round 5, P3-R40: a substitution operator whose word has no brace stays a read (the two
	// reads echo ${HOME:-/root} and find . -name ${X} are already pinned above)
	"echo ${X:+a}",
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
	// fix round 1, C1 (replaced by P3-R13 in round 2): $_ is never tracked, so it always injects,
	// exactly as before Task 4; the six control-word probes below still hold, by the simpler rule
	"for x in 1; do echo -delete; done; find . $_": "find", "if true; then echo -delete; fi; find . $_": "find",
	"{ echo -delete; }; find . $_": "find", "case a in a) echo -delete;; esac; find . $_": "find",
	"echo -delete; (true); find . $_": "find", "echo -delete; case a in *) find . $_;; esac": "find",
	// fix round 2, P3-R13: every $_ read from round 1 moves here, since $_ always injects again
	"echo hi; cat $_": "cat", "ls /tmp; cat $_": "cat",
	// fix round 1, C2: a reference at the start of a word immediately followed by "-" injects, since
	// it can expand to nothing
	"find . ${X:-}-delete": "find", "find . ${X-}-delete": "find", "find . ${X:+}-delete": "find",
	"find . ${X:+a}-delete": "find", "find . $X-delete": "find",
	// fix round 1, C3: an assignment-only segment's own value is checked too; caught at the assignment
	// (x=...) before "find" is even reached, so the neighbour names the exact expansion word (round 2:
	// "x" was vacuous, since every expansion reason contains "expands", which contains "x")
	"x=${y:=-delete}; find . $y": "${y:=-delete}", "x=${y=-delete}; find . $y": "${y=-delete}",
	// fix round 1, C4: note never downgrades a variable to a less dangerous kind
	"x=-delete; x=1 true; find . $x": "find", "x=-delete; for x in; do true; done; find . $x": "find",
	"x=-delete; for x; do true; done; find . $x": "find",
	// fix round 1, I2, and fix round 2, minor item 5: more variables that smuggle an option into an
	// allowlisted reader (MORE, PYTHONHOME were already in programVars; these pin them with a command)
	"echo x | LESS=-O/tmp/x less": "LESS", "PYTHONUSERBASE=/tmp/x aws s3 ls": "PYTHONUSERBASE",
	"GNUPGHOME=/tmp/x git log --show-signature": "GNUPGHOME", "WGETRC=/tmp/x wget -qO- https://example.com": "WGETRC",
	"MORE=-x more f": "MORE", "PYTHONHOME=/tmp/x ls": "PYTHONHOME", "PYTHONWARNINGS=x ls": "PYTHONWARNINGS",
	// fix round 2, P3-R15 (extends I2/P3-R11): openssl reads OPENSSL_CONF for its engines and providers
	"OPENSSL_CONF=/tmp/x.cnf curl https://example.com": "OPENSSL_CONF",
	// substitution loops (Task 6, ruling R3): a body that writes, runs or names a cluster, a backtick,
	// and a substitution feeding any program that is not a for-list or an option-harmless one
	"for p in $(ls); do cat $p; done": "cat", "cat $(ls)": "cat", "for p in $(rm -rf x); do echo $p; done": "rm",
	"echo $(kubectl delete pod x)": "kubectl", "echo `date`": "substitution", "kubectl get pods -n $(cat ns)": "kubectl",
	"echo $(cat f > g)": "redirect",
	// Task 6 fix round 1, ruling P3-R17: a case arm's ) has no matching (, so a quoted $(...) body with a
	// case is captured to the end (the outer quote goes with it), fails to parse and stays a mutation.
	// The critical fail-opens the safety review found; the last one was over-blocked before the capture
	// change. Wants that name a parse-error over-capture use "substitution" (its reason prefix), since
	// the trailing outer quote breaks parsing before the redirect or program is classified.
	"echo \"$(case a in a) rm x;; esac)\"":                       "rm",
	"for p in \"$(case a in a) rm x;; esac)\"; do echo $p; done": "rm",
	"echo \"$(case a in a) cat f > g;; esac)\"":                  "substitution",
	"echo \"$(case $y in x) rm -rf pwned ;; *) : ;; esac)\"":     "rm",
	"echo \"$(case $x in a) echo 1;; esac)\"":                    "substitution",
	// Task 6 fix round 2, ruling P3-R23: a # glued after a subshell's ) starts a comment; the ) it then
	// contains must not close a quoted substitution early and hide the command after the newline
	"echo \"$( (true)#x )\nrm -rf /tmp/x)\"": "rm",
	// Task 6 fix round 2, minor: pin the unquoted case forms too (both gates catch the tail here)
	"echo $(case a in a) rm x;; esac)":       "rm",
	"x=$(case a in a) rm x;; esac); echo $x": "rm",
	// fix round 2, C2/P3-R14(b): bash brace-expands textually before $X is read, so an alternative
	// with a reference (checked leaf by leaf in braceOption, not injects: see leafDanger) can still
	// become an option if the reference resolves to nothing
	"find . {a,$X}-delete": "find", "find . {x,${X:+}}-delete": "find", "find . {a,${X:-}}-delete": "find",
	// fix round 3, N1/P3-R20: the dash rule walks the whole leading run of references (a chain, or a
	// substitution marker), not just the first one, since every reference in the run can expand to
	// nothing; applied to a brace leaf too (leafDanger)
	"find . $@$@-delete": "find", "o=; find . $o$o-delete": "find", "find . $X$Y-delete": "find",
	"find . ${X:+}$Y-delete": "find", "find . $X${Y:-}-delete": "find", `find . "$X""$Y"-delete`: "find",
	"sort $X$Y-o out f": "sort", "find . {a,$X$Y}-delete": "find",
	"find . $(true)-delete": "find", "find . $(true)$(true)-delete": "find",
	// fix round 3, O1/P3-R21: brace expansion happens before parameter expansion, so a brace
	// alternative can rebuild the name of a variable the line set; braceInjects checks every leaf the
	// way injects checks a plain word, so the reason names the rebuilt variable
	"ab=-o; sort $a{b,x} out f": "ab", "ab=-o; sort {$a,x}b out f": "ab",
	"Xdelete=-delete; find . {$X,a}delete": "Xdelete",
	// fix round 4, O1 remainder/P3-R31: braceInjects enumerates every member of a {x..y}, {x..y..step}
	// or single-character sequence, so $a{1..9} rebuilds $a5 (not only its ends $a1 and $a9), and a
	// sequence over the braceLimit is one option (fail closed)
	"a5=-o; sort $a{1..9} out f": "a5", "ac=-o; sort $a{b..d} out f": "ac",
	"a1=-o; sort $a{1..9} out f": "a1", "a9=-o; sort $a{1..9} out f": "a9",
	"sort {1..1000}": "sort",
	// fix round 4, OOS-1/P3-R32: shellwords strips the quote or escape around a "}", so ${X:+"}"},
	// ${X:+\}}, ${X:+'}'} and ${X:+"a}b"} all reach the classifier as ${X:+}}, whose unmatched "}" is
	// the trace of the brace a quote hid; every shell expands the word to -delete. echo is
	// option-harmless, but an opaque ${...} form is a mutation for every program.
	`find . ${X:+"}"}-delete`: "find", `find . ${X:+\}}-delete`: "find",
	`find . ${X:+'}'}-delete`: "find", `find . ${X:+"a}b"}-delete`: "find",
	`echo ${X:+"}"}`: "echo",
	// fix round 5, P3-R40: a ${...} whose operator word holds a brace after quote stripping is opaque,
	// since a stripped quote may have hidden the real end of the expansion, so reference() closed it at
	// the wrong "}". find . ${X:+{"}"}-delete reduces to the balanced ${X:+{}}-delete (strippedBrace
	// cannot see it); every shell can still expand it to an option. Opaque for every program, echo too.
	`find . ${X:+{"}"}-delete`: "find", "find . ${X:+{}}-delete": "find",
	"find . ${X:-a{b}}-delete": "find", "echo ${X:+{}}": "echo",
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
