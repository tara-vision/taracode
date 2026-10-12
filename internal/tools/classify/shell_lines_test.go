package classify

import (
	"slices"
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
