package classify

import (
	"slices"
	"testing"
)

// TestShellReadsTheScriptAShellTakesOnItsInput: a here-document or here-string that a shell reads as its script
// is read as lines of their own, for the files they write and for the deny patterns.
func TestShellReadsTheScriptAShellTakesOnItsInput(t *testing.T) {
	cases := []struct{ cmd, path, line string }{
		{"sh <<'EOF'\nrm -rf infra\nEOF", "infra", "rm -rf infra"},
		{"bash -s <<EOF\nterraform destroy\nEOF", "", "terraform destroy"},
		{"bash -x -o pipefail <<'EOF'\necho {} > terraform.tfstate\nEOF", "terraform.tfstate", "echo {}"},
		{`sh <<< "rm -rf infra"`, "infra", "rm -rf infra"},
		{"cat <<EOF\nnow $(rm -rf infra)\nEOF", "infra", "rm -rf infra"},
	}
	for _, c := range cases {
		res := Shell(c.cmd)
		if c.path != "" && !slices.Contains(res.Paths, c.path) {
			t.Errorf("%q: paths %q, want %q among them", c.cmd, res.Paths, c.path)
		}
		if !slices.Contains(res.Lines, c.line) {
			t.Errorf("%q: lines %q, want one to be %q", c.cmd, res.Lines, c.line)
		}
	}
}

// TestShellTakesAHereDocumentAsData: the body of a here-document that a program reads as data (a commit message,
// a file's content, a script file's input) names nothing the line acts on.
func TestShellTakesAHereDocumentAsData(t *testing.T) {
	cases := []struct {
		cmd   string
		paths []string
	}{
		{"git commit -F - <<'EOF'\nbump terraform.tfstate handling\nEOF", nil},
		{"cat > notes.md <<'EOF'\nDon't touch terraform.tfstate\nrm -rf infra\nEOF", []string{"notes.md"}},
		{"bash install.sh <<'EOF'\nrm -rf infra\nEOF", nil},
	}
	for _, c := range cases {
		res := Shell(c.cmd)
		if !slices.Equal(res.Paths, c.paths) {
			t.Errorf("%q: paths %q, want %q", c.cmd, res.Paths, c.paths)
		}
		if slices.Contains(res.Lines, "rm -rf infra") {
			t.Errorf("%q: the body became a line: %q", c.cmd, res.Lines)
		}
	}
	if Shell("git commit -F - <<'EOF'\nbump terraform.tfstate handling\nEOF").Opaque {
		t.Error("a commit message made the line opaque")
	}
	if !Shell("python3 <<'EOF'\nopen('terraform.tfstate', 'w')\nEOF").Opaque {
		t.Error("python3 reading its program from a here-document is not opaque")
	}
}

// TestShellKubeCountsAKubectlInAHereDocument: on an opaque line, a kubectl or helm named in a here-document acts
// on a cluster the classifier cannot tell; on a line it can read, a body that mentions one is data.
func TestShellKubeCountsAKubectlInAHereDocument(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"sh <<'EOF'\nkubectl --context prod delete ns x\nEOF", "{* * }"},
		{"python3 <<'EOF'\nsubprocess.run(['kubectl', 'delete', 'ns', 'x'])\nEOF", "{* * }"},
		{"git commit -F - <<'EOF'\nbump the helm chart\nEOF", ""},
	}
	for _, c := range cases {
		if got := kubeOf(c.cmd); got != c.want {
			t.Errorf("%q: kube targets %q, want %q", c.cmd, got, c.want)
		}
	}
}
