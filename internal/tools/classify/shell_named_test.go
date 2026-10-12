package classify

import (
	"fmt"
	"strings"
	"testing"
)

// TestShellSplitsWrittenFromNamedPaths: a line writes, moves or removes some paths, so a directory among them
// is checked for what it holds, and acts on others by name only: a destination directory, the start points a
// find narrows by name and those names, an extraction directory, the paths git checks out, restores, moves or
// removes. A directory change (cd -P, a cd to a glob, env -C, sudo -D, git -C) moves the paths with it.
func TestShellSplitsWrittenFromNamedPaths(t *testing.T) {
	cases := []struct{ cmd, paths, named, landings string }{
		{"find . -name '*.pyc' -delete", "", ".|*.pyc", ""},
		{"find . -type d -name __pycache__ -exec rm -rf {} +", "", ".|__pycache__", ""},
		{"find . -delete", ".", "", ""},
		{"find -L . -type f -delete", ".", "", ""},
		{"find -H -O2 -D tree . -mtime +7 -delete", ".", "", ""},
		{"find . -name '*' -delete", ".", "", ""},
		{"find . ! -name '*.go' -delete", ".", "", ""},
		{"find . -not \\( -name '*.go' \\) -delete", ".", "", ""},
		{"find . -name '*.o' -o -delete", ".", "", ""},
		{"find . -name '*.o' , -delete", ".", "", ""},
		{"find . -type f -o -name '*.o' -delete", ".", "", ""},
		{"find . -delete -not", ".", "", ""},
		{"find . ! -type d -name '*.o' -delete", "", ".|*.o", ""},
		{"find . \\( -name '*.o' -delete", "", ".|*.o", ""},
		{"find . -type f \\( -name '*.o' -o -name '*.a' \\) -delete", "", ".|*.o|*.a", ""},
		{"find . \\( -name '*.o' -o -type f \\) -delete", ".", "", ""},
		{"find . -path ./node_modules -prune -o -name '*.log' -delete", "", ".|./node_modules|*.log", ""},
		{"find . -name '*.log' -fprint out.txt", "out.txt", "", ""},
		{"cp x .", "", ".", "x"},
		{"cp -r src dst/", "", "dst/", "dst/src"},
		{"cp -rT src infra", "infra", "infra", "infra/src"},
		{"cp --no-target-directory a b", "b", "b", "b/a"},
		{"cp -R src/ infra", "infra", "infra", "infra/src"},
		{"mv -T a b", "a|b", "b", "b/a"},
		{"install -m 0644 x /usr/local/bin/", "", "/usr/local/bin/", "/usr/local/bin/x"},
		{"mv main.tf infra/", "main.tf", "infra/", "infra/main.tf"},
		{"mv -t /tmp/x .git", ".git", "/tmp/x", "/tmp/x/.git"},
		{"mv x", "x", "", ""},
		{"ln -s .git g", ".git", "g", "g/.git"},
		{"ln terraform.tfstate t.json", "terraform.tfstate", "t.json", "t.json/terraform.tfstate"},
		{"ln -s ../x .", "../x", ".", "x"},
		{"ln -s ../shared/.git sub/g", "shared/.git", "sub/g", "sub/g/.git"},
		{"ln -sf -t bin ../tool", "tool", "bin", "bin/tool"},
		{"cd -P .git && rm config", "config|.git/config", "", ""},
		{"cd .gi? && rm config", "config|.gi?/config", "", ""},
		{"cd -- infra && rm x.tfstate", "x.tfstate|infra/x.tfstate", "", ""},
		{"cd a b && rm x", "x", "", ""},
		{"env -C infra rm x.tfstate", "infra/x.tfstate", "", ""},
		{"env --chdir=infra rm x.tfstate", "infra/x.tfstate", "", ""},
		{"sudo -D infra rm x.tfstate", "infra/x.tfstate", "", ""},
		{"tar -xf x.tar -C .git", "", ".git", ""},
		{"tar xzf x.tgz --directory=infra", "", "infra", ""},
		{"tar -czf out.tgz -C src .", "", "", ""},
		{"unzip -o x.zip -d .git", "", ".git", ""},
		{"git rm terraform.tfstate", "", "terraform.tfstate", ""},
		{"git checkout -- terraform.tfstate", "", "terraform.tfstate", ""},
		{"git restore --staged x", "", "x", ""},
		{"git -C infra checkout -- x.tfstate", "", "infra/x.tfstate", ""},
		{"git -C a -C /b mv x .git/y", "", "/b/x|/b/.git/y", ""},
		{"git -c core.hooksPath=h checkout x", "", "", ""},
		{"git commit -m x", "", "", ""},
		{"git", "", "", ""},
		{"cd infra && git checkout -- x.tfstate", "", "x.tfstate|infra/x.tfstate", ""},
		{`sh -c "git checkout -- terraform.tfstate"`, "", "terraform.tfstate", ""},
		{"rm -rf build", "build", "", ""},
	}
	for _, c := range cases {
		res := Shell(c.cmd)
		if got := strings.Join(res.Paths, "|"); got != c.paths {
			t.Errorf("%q: paths %q, want %q", c.cmd, got, c.paths)
		}
		if got := strings.Join(res.Named, "|"); got != c.named {
			t.Errorf("%q: named %q, want %q", c.cmd, got, c.named)
		}
		if got := strings.Join(res.Landings, "|"); got != c.landings {
			t.Errorf("%q: landings %q, want %q", c.cmd, got, c.landings)
		}
	}
}

// TestShellFindsAreTheFindsNarrowedByName: a find that narrows by name what it deletes or runs a command on
// reaches the policy with its start points and name tests, read from the directories the line moves to.
func TestShellFindsAreTheFindsNarrowedByName(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"find . -name config -delete", "{. . [{-name config}] false}"},
		{"cd sub && find . -iname '*.pyc' -exec rm {} +", "{. . [{-iname *.pyc}] false}|{sub . [{-iname *.pyc}] false}"},
		{"env -C infra find . -path '*/x/*' -delete", "{infra . [{-path */x/*}] false}"},
		{"find a b -regex '.*x' -o -name y -delete",
			"{a a [{-regex .*x} {-name y}] false}|{b b [{-regex .*x} {-name y}] false}"},
		{"find -L . -name config -delete", "{. . [{-name config}] true}"},
		{"find . -follow -name x -delete", "{. . [{-name x}] true}"},
		{"find -L -P . -name x -delete", "{. . [{-name x}] false}"},
		{"find -L . -name x -exec chmod -P 600 {} +", "{. . [{-name x}] true}"},
		{"find . -name -L -delete", "{. . [{-name -L}] false}"},
		{"find -L . -delete", "{. . [] true}"},
		{"find . -delete", ""},
		{"find . -name x", ""},
		{"find . -lname '*x*' -delete", ""},
	}
	for _, c := range cases {
		var got []string
		for _, f := range Shell(c.cmd).Finds {
			got = append(got, fmt.Sprintf("%v", f))
		}
		if strings.Join(got, "|") != c.want {
			t.Errorf("%q: finds %q, want %q", c.cmd, got, c.want)
		}
	}
	if paths := Shell("find . -lname '*x*' -delete").Paths; strings.Join(paths, "|") != "." {
		t.Errorf("a find narrowed by a link test only: paths %q", paths)
	}
}
