package classify

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestVerblessAndGlobalOnlyLines: a CLI given only global options, or no command, prints its help
// and changes nothing.
func TestVerblessAndGlobalOnlyLines(t *testing.T) {
	check(t, "git", Git, []verbCase{{"--no-pager", policy.Read, ""}, {"branch -vd feature", policy.Mutate, "branch"}})
	check(t, "docker", Docker, []verbCase{
		{"--context prod", policy.Read, ""}, {"container ls", policy.Read, "container"},
		{"manifest inspect web:1", policy.Read, "manifest"}, {"manifest push web:1", policy.Mutate, "manifest"},
		{"login", policy.Mutate, "login"},
	})
	check(t, "helm", Helm, []verbCase{{"-n kube-system", policy.Read, ""}})
	for _, c := range []struct{ provider, args, verb string }{{"aws", "--version", ""}, {"aws", "help", "help"}, {"az", "--version", ""}} {
		if got := Cloud(c.provider, strings.Fields(c.args)); got.Classification != policy.Read || got.Verb != c.verb {
			t.Errorf("%s %s: %+v", c.provider, c.args, got)
		}
	}
}

func TestTerraformEdges(t *testing.T) {
	if got := Terraform("plan", []string{"infra"}); got.Classification != policy.Read {
		t.Errorf("plan with a directory operand: %+v", got)
	}
	if got := Terraform("providers", []string{"lock"}); got.Classification != policy.Mutate || got.Verb != "providers" {
		t.Errorf("providers lock writes the lock file: %+v", got)
	}
}

func TestShellEdgeReads(t *testing.T) {
	checkReads(t, []string{
		"curl https://api.example.com -X", "date 1200.", "date 1200.ab", "sysctl kern.ostype", "command -p",
		"timeout 5", "gh", "gh api /repos/x/y", "for x in {1..3}; do ls $x; done", "sed -n -- 1p file.txt",
		"sed -n --expression 1p f", "sed 's/a/b/;p' f", "sed --line 40 -n l f", "awk '{print} # done' f",
	})
}

func TestShellEdgeMutations(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"sysctl -w net.ipv4.ip_forward=1", "sysctl"},
		{"go list -toolexec=x ./...", "toolexec"},
		{"curl -w '%output{x}' https://example.com", "%output"},
		{"for x in {-1..2}; do ls $x; done", "options to ls"},
		{"ls {a,b}{a,b}{a,b}{a,b}{a,b}{a,b}", "brace expansion"},
		{"sed -n -- 'w out' f", "sed script"},
		{"sed -n --expr 'w out' f", "sed script"},
		{"sed -n '/[[.x/p' f", "sed"},
		{"sed s", "sed"},
	})
}

func TestShellWrittenPaths(t *testing.T) {
	tests := []struct {
		cmd  string
		want []string
	}{
		{"shred -n 3 secret.key", []string{"secret.key"}},
		{"cp -r", nil},
		{"ln -t dir target", []string{"dir"}},
		{"ln -s", nil},
		{"chown --reference=ref.txt a.txt b.txt", []string{"a.txt", "b.txt"}},
		{"chown", nil},
		{"yq -i --from-file=edit.yq values.yaml", []string{"values.yaml"}},
		{"sudo -- rm x", []string{"x"}},
	}
	for _, tt := range tests {
		got := Shell(tt.cmd)
		if got.Classification != policy.Mutate || !reflect.DeepEqual(got.Paths, tt.want) {
			t.Errorf("%q: %s, paths %q, want %q", tt.cmd, got.Classification, got.Paths, tt.want)
		}
	}
}

// TestKubeTargetsOfIncompleteCommands: a value option with no value names nothing, label without a
// change still names its objects, and a wrapper's "--" leaves the command after it.
func TestKubeTargetsOfIncompleteCommands(t *testing.T) {
	tests := []struct {
		cmd  string
		want KubeTarget
	}{
		{"kubectl delete pod web -n", KubeTarget{}},
		{"kubectl delete pod web --namespace", KubeTarget{}},
		{"kubectl delete", KubeTarget{}},
		{"kubectl label pods web", KubeTarget{}},
		{"kubectl label ns kube-system", KubeTarget{Namespace: "kube-system"}},
		{"sudo -- kubectl delete pod web -n shop", KubeTarget{Context: "*", Namespace: "shop", Cause: causeWrapper}},
	}
	for _, tt := range tests {
		got := Shell(tt.cmd)
		if got.Classification != policy.Mutate || len(got.Kube) != 1 || got.Kube[0] != tt.want {
			t.Errorf("%q: %s %+v, want %+v", tt.cmd, got.Classification, got.Kube, tt.want)
		}
	}
}

func TestReferenceOfAnUnterminatedBrace(t *testing.T) {
	if ref, n := reference("{HOME"); ref != (expansionRef{}) || n != len("{HOME") {
		t.Fatalf("%+v %d", ref, n)
	}
}

// TestBraceSequenceExpandsLikeBash: the members bash gives a sequence, both padded and plain forms
// for zero-padded operands, and opaque for shapes it cannot follow.
func TestBraceSequenceExpandsLikeBash(t *testing.T) {
	tests := []struct {
		body   string
		want   []string
		opaque bool
	}{
		{"1..3", []string{"1", "2", "3"}, false},
		{"3..1", []string{"3", "2", "1"}, false},
		{"1..9..3", []string{"1", "4", "7"}, false},
		{"1..9..-3", []string{"1", "4", "7"}, false},
		{"01..03", []string{"1", "01", "2", "02", "3", "03"}, false},
		{"-07..-06", []string{"-7", "-07", "-6", "-06"}, false},
		{"a..c", []string{"a", "b", "c"}, false},
		{"C..A", []string{"C", "B", "A"}, false},
		{"e..a..2", []string{"e", "c", "a"}, false},
		{"1..9..0", nil, true},
		{"1..2..3..4", nil, true},
		{"1..a", nil, true},
		{"a..Z", nil, true},
		{"1..4294967297", nil, true},
	}
	for _, tt := range tests {
		got, opaque := braceSequence(tt.body)
		if opaque != tt.opaque || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("braceSequence(%q) = %q, %v; want %q, %v", tt.body, got, opaque, tt.want, tt.opaque)
		}
	}
	if ends, opaque := endsOnly("-1..5"); opaque || !reflect.DeepEqual(ends, []string{"-1", "5"}) {
		t.Errorf("endsOnly: %q %v", ends, opaque)
	}
}

// TestTerraformChdirIsNotTheCommand: -chdir=DIR before the command is skipped, so the command after it
// decides.
func TestTerraformChdirIsNotTheCommand(t *testing.T) {
	if got := Shell("terraform -chdir=infra plan"); got.Classification != policy.Read || got.Verb != "plan" {
		t.Errorf("plan: %+v", got.Result)
	}
	if got := Shell("terraform -chdir=infra -chdir=other apply"); got.Classification != policy.Mutate || got.Verb != "apply" {
		t.Errorf("apply: %+v", got.Result)
	}
}

// TestASelectorGlobNamesNoNamespaceObject: a glob in a label selector's value expands as a selector,
// never into an object of the namespace kind.
func TestASelectorGlobNamesNoNamespaceObject(t *testing.T) {
	if expandsIntoNamespaceObject([]string{"delete", "-l", "app=web*"}) {
		t.Fatal("a selector value is not an object")
	}
}
