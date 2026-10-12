package classify

import "testing"

// TestShellOpaqueMarksALineTheClassifierCannotSeeInto: a line whose effects the classifier cannot read from
// its words is opaque, so the protected targets it names apply to it.
func TestShellOpaqueMarksALineTheClassifierCannotSeeInto(t *testing.T) {
	opaque := []string{
		`python3 -c "print(1)"`,
		`perl -e 'print 1'`,
		`sh -c "echo hi"`,
		`bash -c "ls"`,
		`eval 'echo hi'`,
		`echo $(date)`,
		"echo `date`",
		`f() { :; }; f`,
		`echo x | xargs rm`,
		`printf 'a\n' | parallel echo`,
		`awk 'BEGIN { print "x" > "f" }'`,
		`sed -i 's/a/b/' f`,
		`make deploy`,
		`./deploy.sh`,
		`find . -name x -exec rm {} \;`,
		`docker run alpine sh -c "x"`,
		`env -S "a b"`,
		`echo "unterminated`,
	}
	for _, cmd := range opaque {
		if !Shell(cmd).Opaque {
			t.Errorf("%s: not opaque", cmd)
		}
	}
	clear := []string{
		`ls -la`,
		`rm -rf build`,
		`cp a b`,
		`echo hi > out.txt`,
		`kubectl delete pod x`,
		`terraform apply`,
		`cat terraform.tfstate`,
		`awk '{ print $1 }' f`,
		`sed 's/a/b/' f`,
	}
	for _, cmd := range clear {
		if Shell(cmd).Opaque {
			t.Errorf("%s: opaque", cmd)
		}
	}
}

// TestShellKubeCountsAKubectlNamedInsideAWord: on an opaque line, a kubectl or helm named inside a string an
// interpreter runs acts on a cluster the classifier cannot tell, so its context and namespace are "*".
func TestShellKubeCountsAKubectlNamedInsideAWord(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{`sh -c "kubectl --context prod delete ns x"`, "{* * }"},
		{`python3 -c "import subprocess; subprocess.run(['kubectl', 'delete', 'ns', 'x'])"`, "{* * }"},
		{`bash -c 'helm uninstall web'`, "{* * }"},
		{`echo "kubectl delete ns x"`, ""},
		{`git commit -m "bump the helm chart"`, ""},
		{`sh -c "kubectl-foo run"`, ""},
	}
	for _, c := range cases {
		if got := kubeOf(c.cmd); got != c.want {
			t.Errorf("%s: kube targets %q, want %q", c.cmd, got, c.want)
		}
	}
}
