package classify

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestShellLinesAreTheFormsADenyPatternReads: besides the whole line, a deny pattern reads each simple
// command, the same without the one-word options before its first operand, and the strings a shell's -c,
// eval or a $(...) runs, read as lines of their own.
func TestShellLinesAreTheFormsADenyPatternReads(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"cd . && terraform destroy", "terraform destroy"},
		{"terraform -chdir=. destroy", "terraform destroy"},
		{"true && kubectl delete namespace a", "kubectl delete namespace a"},
		{`sh -c "terraform destroy"`, "terraform destroy"},
		{`bash -ec 'kubectl delete namespace a'`, "kubectl delete namespace a"},
		{"eval terraform destroy", "terraform destroy"},
		{`echo "$(terraform destroy)"`, "terraform destroy"},
		{`sh -c "sh -c 'terraform destroy'"`, "terraform destroy"},
		{`bash -c -x 'terraform destroy'`, "terraform destroy"},
		{`sh -c -- 'terraform destroy'`, "terraform destroy"},
		{`bash -o pipefail -c 'terraform destroy'`, "terraform destroy"},
		{`bash --norc -c 'terraform destroy'`, "terraform destroy"},
		{`bash -O extglob -c 'terraform destroy'`, "terraform destroy"},
		{`bash +O extglob -c 'terraform destroy'`, "terraform destroy"},
		{`bash --unknown-option value -c 'terraform destroy'`, "terraform destroy"},
		{"eval -- terraform destroy", "terraform destroy"},
		{"watch -n 1 'terraform destroy'", "terraform destroy"},
		{"su -c 'terraform destroy'", "terraform destroy"},
		{"su root -c 'terraform destroy'", "terraform destroy"},
		{"su -lc 'terraform destroy' root", "terraform destroy"},
		{"su --command='terraform destroy'", "terraform destroy"},
		{"fish -c 'terraform destroy'", "terraform destroy"},
		{"busybox sh -c 'terraform destroy'", "terraform destroy"},
		{`sh -c "sh -c \"sh -c 'sh -c \\\"terraform destroy\\\"'\""`, "terraform destroy"},
		{"echo `terraform destroy`", "terraform destroy"},
		{"time terraform destroy", "terraform destroy"},
		{"timeout 600 terraform destroy", "terraform destroy"},
		{"env X=1 terraform destroy", "terraform destroy"},
		{"/opt/homebrew/bin/terraform destroy", "terraform destroy"},
		{"sudo -u ops terraform -chdir=. destroy", "terraform destroy"},
	}
	for _, c := range cases {
		if lines := Shell(c.cmd).Lines; !slices.Contains(lines, c.want) {
			t.Errorf("%s: lines %q, want one to be %q", c.cmd, lines, c.want)
		}
	}
	if lines := Shell(`sh -c`).Lines; !slices.Equal(lines, []string{"sh -c", "sh"}) {
		t.Errorf("sh -c with no string: lines %q", lines)
	}
}

// nested wraps a line in n levels of sh -c, quoting each level for the next.
func nested(line string, n int) string {
	for range n {
		line = "sh -c '" + strings.ReplaceAll(line, "'", `'\''`) + "'"
	}
	return line
}

// TestShellReadsStringsNestedUpToItsDepth: a string run as a line inside another is read down to innerDepth
// levels, for the deny patterns and for the files it writes; deeper than that it is not read.
func TestShellReadsStringsNestedUpToItsDepth(t *testing.T) {
	for n := 1; n <= innerDepth; n++ {
		if res := Shell(nested("rm -rf .git", n)); !slices.Contains(res.Paths, ".git") ||
			!slices.Contains(res.Lines, "rm -rf .git") {
			t.Errorf("%d levels: paths %q", n, res.Paths)
		}
	}
	if res := Shell(nested("rm -rf .git", innerDepth+1)); slices.Contains(res.Paths, ".git") {
		t.Errorf("%d levels read past the depth: %q", innerDepth+1, res.Paths)
	}
}

// TestShellReadsNestedSubstitutionsOnce: each captured substitution is read once, from the outermost one down,
// so a line nested many levels deep classifies quickly.
func TestShellReadsNestedSubstitutionsOnce(t *testing.T) {
	for _, cmd := range []string{"echo " + strings.Repeat("$(echo ", 16) + "x" + strings.Repeat(")", 16),
		"echo `" + strings.Repeat("$(echo ", 16) + "x" + strings.Repeat(")", 16) + "`"} {
		start := time.Now()
		res := Shell(cmd)
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%d bytes nested 16 levels took %s", len(cmd), took)
		}
		if !strings.Contains(cmd, "`") && res.Classification != policy.Read { // a backtick fails closed
			t.Errorf("echo inside echo: %v", res.Classification)
		}
	}
}

// TestShellReadsASubstitutionAtAnyDepth: substitution bodies are read flat, so a command nested more levels deep
// than strings run as lines are read (innerDepth) is still read for its targets and its deny lines.
func TestShellReadsASubstitutionAtAnyDepth(t *testing.T) {
	quoted := "rm -rf infra"
	for range 12 {
		quoted = `echo "$(` + quoted + `)"`
	}
	for _, cmd := range []string{quoted, "echo " + strings.Repeat("$(echo ", 12) + "$(rm -rf infra)" + strings.Repeat(")", 12),
		"cat <<EOF\n" + strings.Repeat("$(echo ", 12) + "$(rm -rf infra)" + strings.Repeat(")", 12) + "\nEOF"} {
		res := Shell(cmd)
		if !slices.Contains(res.Paths, "infra") || !slices.Contains(res.Lines, "rm -rf infra") {
			t.Errorf("%d levels: paths %q", 13, res.Paths)
		}
	}
}
