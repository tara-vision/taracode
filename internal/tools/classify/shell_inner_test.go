package classify

import (
	"slices"
	"testing"
)

// TestShellPathsReadTheLinesAStringRuns: a file that a shell's -c, eval or a $(...) writes or removes is a
// target of the line, as it would be written on the line itself.
func TestShellPathsReadTheLinesAStringRuns(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{`sh -c "rm -rf infra"`, "infra"},
		{`bash -c 'echo {} > terraform.tfstate'`, "terraform.tfstate"},
		{`eval 'echo {} > terraform.tfstate'`, "terraform.tfstate"},
		{`echo "$(rm -f terraform.tfstate)"`, "terraform.tfstate"},
		{`sh -c "sh -c 'rm -rf .git'"`, ".git"},
		{"echo `rm -rf infra`", "infra"},
		{`sh -c -- "rm -rf infra"`, "infra"},
		{`bash -c -x 'rm -rf infra'`, "infra"},
	}
	for _, c := range cases {
		if paths := Shell(c.cmd).Paths; !slices.Contains(paths, c.want) {
			t.Errorf("%s: paths %q, want %q among them", c.cmd, paths, c.want)
		}
	}
}
