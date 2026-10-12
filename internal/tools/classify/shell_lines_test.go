package classify

import (
	"slices"
	"strings"
	"testing"
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
