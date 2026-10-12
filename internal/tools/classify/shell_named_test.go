package classify

import (
	"strings"
	"testing"
)

// TestShellSplitsWrittenFromNamedPaths: a line writes, moves or removes some paths, so a directory among them
// is checked for what it holds, and acts on others by name only: a destination directory, the start points a
// find narrows by name and those names, an extraction directory, the paths git checks out, restores, moves or
// removes. A directory change (cd -P, a cd to a glob, env -C, sudo -D, git -C) moves the paths with it.
func TestShellSplitsWrittenFromNamedPaths(t *testing.T) {
	cases := []struct{ cmd, paths, named string }{
		{"find . -name '*.pyc' -delete", "", ".|*.pyc"},
		{"find . -type d -name __pycache__ -exec rm -rf {} +", "", ".|__pycache__"},
		{"find . -delete", ".", ""},
		{"find -L . -type f -delete", ".", ""},
		{"find -H -O2 -D tree . -mtime +7 -delete", ".", ""},
		{"find . -name '*' -delete", ".", ""},
		{"find . ! -name '*.go' -delete", ".", ""},
		{"find . -not \\( -name '*.go' \\) -delete", ".", ""},
		{"find . -name '*.o' -o -delete", ".", ""},
		{"find . -name '*.o' , -delete", ".", ""},
		{"find . -type f -o -name '*.o' -delete", ".", ""},
		{"find . -delete -not", ".", ""},
		{"find . ! -type d -name '*.o' -delete", "", ".|*.o"},
		{"find . \\( -name '*.o' -delete", "", ".|*.o"},
		{"find . -type f \\( -name '*.o' -o -name '*.a' \\) -delete", "", ".|*.o|*.a"},
		{"find . \\( -name '*.o' -o -type f \\) -delete", ".", ""},
		{"find . -path ./node_modules -prune -o -name '*.log' -delete", "", ".|./node_modules|*.log"},
		{"find . -name '*.log' -fprint out.txt", "out.txt", ""},
		{"cp x .", "x", "."},
		{"cp -r src dst/", "dst/src", "dst/"},
		{"cp -rT src infra", "infra/src|infra", "infra"},
		{"cp --no-target-directory a b", "b/a|b", "b"},
		{"cp -R src/ infra", "infra/src|infra", "infra"},
		{"mv -T a b", "a|b/a|b", "b"},
		{"install -m 0644 x /usr/local/bin/", "/usr/local/bin/x", "/usr/local/bin/"},
		{"mv main.tf infra/", "main.tf|infra/main.tf", "infra/"},
		{"mv -t /tmp/x .git", ".git|/tmp/x/.git", "/tmp/x"},
		{"mv x", "x", ""},
		{"ln -s .git g", ".git|g/.git", "g"},
		{"ln terraform.tfstate t.json", "terraform.tfstate|t.json/terraform.tfstate", "t.json"},
		{"ln -s ../x .", "../x|x", "."},
		{"ln -s ../shared/.git sub/g", "shared/.git|sub/g/.git", "sub/g"},
		{"ln -sf -t bin ../tool", "tool|bin/tool", "bin"},
		{"cd -P .git && rm config", "config|.git/config", ""},
		{"cd .gi? && rm config", "config|.gi?/config", ""},
		{"cd -- infra && rm x.tfstate", "x.tfstate|infra/x.tfstate", ""},
		{"cd a b && rm x", "x", ""},
		{"env -C infra rm x.tfstate", "infra/x.tfstate", ""},
		{"env --chdir=infra rm x.tfstate", "infra/x.tfstate", ""},
		{"sudo -D infra rm x.tfstate", "infra/x.tfstate", ""},
		{"tar -xf x.tar -C .git", "", ".git"},
		{"tar xzf x.tgz --directory=infra", "", "infra"},
		{"tar -czf out.tgz -C src .", "", ""},
		{"unzip -o x.zip -d .git", "", ".git"},
		{"git rm terraform.tfstate", "", "terraform.tfstate"},
		{"git checkout -- terraform.tfstate", "", "terraform.tfstate"},
		{"git restore --staged x", "", "x"},
		{"git -C infra checkout -- x.tfstate", "", "infra/x.tfstate"},
		{"git -C a -C /b mv x .git/y", "", "/b/x|/b/.git/y"},
		{"git -c core.hooksPath=h checkout x", "", ""},
		{"git commit -m x", "", ""},
		{"git", "", ""},
		{"cd infra && git checkout -- x.tfstate", "", "x.tfstate|infra/x.tfstate"},
		{`sh -c "git checkout -- terraform.tfstate"`, "", "terraform.tfstate"},
		{"rm -rf build", "build", ""},
	}
	for _, c := range cases {
		res := Shell(c.cmd)
		if got := strings.Join(res.Paths, "|"); got != c.paths {
			t.Errorf("%q: paths %q, want %q", c.cmd, got, c.paths)
		}
		if got := strings.Join(res.Named, "|"); got != c.named {
			t.Errorf("%q: named %q, want %q", c.cmd, got, c.named)
		}
	}
}
