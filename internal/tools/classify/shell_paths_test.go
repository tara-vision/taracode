package classify

import (
	"strings"
	"testing"
)

// TestShellPathsNameTheFilesAWriteTouches: the files a shell line writes or removes reach the
// policy's protected paths (final review I1), as written on the line; the caller resolves them.
func TestShellPathsNameTheFilesAWriteTouches(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"echo x > .taracode/policy.yaml", ".taracode/policy.yaml"},
		{"echo x >> a.log 2> /dev/null", "a.log"},
		{"echo x >& out.txt", "out.txt"},
		{"ls &> all.txt", "all.txt"},
		{"cat a | tee b c", "b|c"},
		{"cat a | sudo tee -a /etc/hosts", "/etc/hosts"},
		{"sed -i 's/a/b/' x.tfstate", "x.tfstate"},
		{"sed -i -e 's/a/b/' -e 's/c/d/' x y", "x|y"},
		{"sed 's/a/b/' x.tfstate", ""},
		{"cp a b dest/", "dest/"},
		{"cp -t dest a b", "dest"},
		{"install -m 644 a /etc/b", "/etc/b"},
		{"mv a b", "a|b"},
		{"rm -rf .git/config x", ".git/config|x"},
		{"sudo rm -f x.tfstate", "x.tfstate"},
		{"env A=1 rm x.tfstate", "x.tfstate"},
		{"ls *.tmp | xargs rm -f", ""},
		{"find . -name '*.tfstate' -delete", ".|*.tfstate"},
		{"find . -fprint0 out.txt", "out.txt"},
		{"touch -d yesterday a", "a"},
		{"truncate -s 0 x.log", "x.log"},
		{"chmod 600 key.pem", "key.pem"},
		{"chmod -w terraform.tfstate", "terraform.tfstate"},
		{"chown -R app:app data", "data"},
		{"ln -s target link", "link"},
		{"ln -s /x/y.tfstate", "y.tfstate"},
		{"dd if=a of=b.tfstate bs=1M", "b.tfstate"},
		{"sort -o out.txt in.txt", "out.txt"},
		{"yq -i '.a = 1' x.yaml", "x.yaml"},
		{"curl -o out.bin https://example.com/x", "out.bin"},
		{"wget -O page.html https://example.com", "page.html"},
		{"cd .taracode && echo x > policy.yaml", "policy.yaml|.taracode/policy.yaml"},
		{"cd infra; cd modules; rm -f a.tfstate", "a.tfstate|infra/a.tfstate|infra/modules/a.tfstate"},
		{"cd ~/.taracode && sed -i 's/a/b/' policy.yaml", "policy.yaml|~/.taracode/policy.yaml"},
		{"cd $DIR && rm x", "x"},
		{"echo $(date) > out.txt", "out.txt"},
		{"kubectl delete pod x", ""},
		{"ls -la", ""},
		{"cat a > /dev/null", ""},
	}
	for _, c := range cases {
		if got := strings.Join(Shell(c.cmd).Paths, "|"); got != c.want {
			t.Errorf("%q: paths %q, want %q", c.cmd, got, c.want)
		}
	}
}
