package classify

import (
	"fmt"
	"slices"
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
		{"find -x . -delete", ".", "", ""},
		{"find -sx . -type f -delete", ".", "", ""},
		{"find -E . -regex '.*config' -delete", "", ".|.*config", ""},
		{"find -f . -f sub -name config -delete", "", ".|sub|config", ""},
		{"find -name config -delete", "", ".|config", ""},
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
		{"tar -xf x.tar -C .git", "", ".git|.git/x.tar", ""},
		{"tar -xf x.tar terraform.tfstate", "", "x.tar|terraform.tfstate", ""},
		{"tar -x -f x.tar --directory=infra m", "", "infra|infra/m", ""},
		{"tar xzf x.tgz --directory=infra", "", "infra|infra/xzf|infra/x.tgz", ""},
		{"tar -czf out.tgz -C src .", "", "", ""},
		{"unzip -o x.zip -d .git", "", ".git|.git/x.zip", ""},
		{"unzip -o x.zip terraform.tfstate", "", "x.zip|terraform.tfstate", ""},
		{"unzip x.zip a -d out -x b", "", "out|out/x.zip|out/a", ""},
		{"unzip x.zip -x b -d .git", "", ".git|.git/x.zip", ""},
		{"cp -al .git g2", ".git", "g2", "g2/.git"},
		{"cp --symbolic-link /etc/hosts h", "/etc/hosts", "h", "h/hosts"},
		{"install -s app bin/", "", "bin/", "bin/app"},
		{"rsync -a --delete empty/ ./", "./", "", ""},
		{"rsync -e ssh -av src/ host:/backup/", "", "", ""},
		{"rsync -e ssh -a src dst", "dst", "", ""},
		{"busybox rm -rf .git", ".git", "", ""},
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
		{"find -sL . -name x -delete", "{. . [{-name x}] true}"},
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

// TestShellReadsTheCommandsAFindRuns: the command after -exec, -execdir, -ok or -okdir is read as a command of
// its own, for its targets, its deny lines and the strings it runs; an operand that leads from {} to another
// path (rm -rf {}/..) makes the find act on all its start points hold.
func TestShellReadsTheCommandsAFindRuns(t *testing.T) {
	paths := []struct{ cmd, want string }{
		{`find . -name '*.pyc' -exec rm -rf infra \;`, "infra"},
		{`cd sub && find . -name x -ok rm -rf infra \;`, "sub/infra"},
		{`find . -name x -execdir sh -c 'rm -rf infra' \;`, "infra"},
		{`find . -name x -exec rm -rf {}/.. \;`, "."},
		{`find . -name x -exec rm -rf infra`, "infra"},
		{`find . -name x -exec sh -c 'rm -rf "$(dirname "$1")"' _ {} \;`, "."},
		{`find . -name '*.py' -exec python3 fix.py {} \;`, "."},
		{`find . -name x -exec ./cleanup.sh {} \;`, "."},
		{`find . -name x -exec {} \;`, "."},
		{`find . -name x -execdir rm -rf infra \;`, "."},
	}
	for _, c := range paths {
		if got := Shell(c.cmd).Paths; !slices.Contains(got, c.want) {
			t.Errorf("%s: paths %q, want %q among them", c.cmd, got, c.want)
		}
	}
	lines := []struct{ cmd, want string }{
		{`find . -name '*.tf' -exec terraform destroy \;`, "terraform destroy"},
		{`find . -name x -exec sh -c 'terraform destroy' {} +`, "terraform destroy"},
	}
	for _, c := range lines {
		if got := Shell(c.cmd).Lines; !slices.Contains(got, c.want) {
			t.Errorf("%s: lines %q, want %q among them", c.cmd, got, c.want)
		}
	}
	for _, cmd := range []string{`find . -name '*.py' -exec sed -i 's/a/b/' {} +`, `find . -name '*.go' -exec gofmt -w {} \;`,
		`find . -name '*.log' -exec mv {} {}.bak \;`, `find . -name '*.py' -execdir sed -i 's/a/b/' {} +`} {
		if got := Shell(cmd).Paths; len(got) != 0 {
			t.Errorf("%s: paths %q, want none beyond the matches", cmd, got)
		}
	}
	if got := kubeOf(`find . -name x -exec sh -c "kube''ctl --context prod delete ns x" \;`); got != "{* * }" {
		t.Errorf("a kubectl in a string find runs: %q", got)
	}
}

// TestShellExpandsBracesBeforeReadingOperands: a command's words are brace-expanded as the shell expands them
// before its operands are read; an opaque line's whole words, so expanded, are handed on for the caller to glob.
func TestShellExpandsBracesBeforeReadingOperands(t *testing.T) {
	cases := []struct{ cmd, paths, landings string }{
		{"mv terraform.tfstate{,.bak}", "terraform.tfstate", "terraform.tfstate.bak/terraform.tfstate"},
		{"rm -rf .gi{t,}", ".git|.gi", ""},
		{"cp src/{a,b} dest/", "", "dest/a|dest/b"},
		{"rm -rf x{1..3}", "x1|x2|x3", ""},
	}
	for _, c := range cases {
		res := Shell(c.cmd)
		if got := strings.Join(res.Paths, "|"); got != c.paths {
			t.Errorf("%q: paths %q, want %q", c.cmd, got, c.paths)
		}
		if got := strings.Join(res.Landings, "|"); got != c.landings {
			t.Errorf("%q: landings %q, want %q", c.cmd, got, c.landings)
		}
	}
	if got := braceWords("x{" + strings.Repeat("a,", 70) + "b}"); !slices.Equal(got, []string{"x*"}) {
		t.Errorf("a brace past the limit: %q, want the brace read as *", got)
	}
	if words := Shell("echo .g{i,o}?t | xargs rm -rf").Words; !slices.Contains(words, ".gi?t") ||
		slices.Contains(words, "-rf") {
		t.Errorf("an opaque line's words: %q", words)
	}
	if words := Shell("rm -rf build").Words; len(words) != 0 {
		t.Errorf("a line the classifier reads hands on words: %q", words)
	}
}
