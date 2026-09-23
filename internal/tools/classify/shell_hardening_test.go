package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// hardeningCase is a command line that must classify as a mutation, and a word its reason or verb
// names, so the row proves which rule refused it.
type hardeningCase struct{ cmd, want string }

func checkMutations(t *testing.T, cases []hardeningCase) {
	t.Helper()
	for _, c := range cases {
		got := Shell(c.cmd)
		if got.Classification != policy.Mutate {
			t.Errorf("%q should be mutate: %+v", c.cmd, got)
			continue
		}
		if got.Reason == "" || !containsFold(got.Reason+" "+got.Verb, c.want) {
			t.Errorf("%q: reason %q verb %q should mention %q", c.cmd, got.Reason, got.Verb, c.want)
		}
	}
}

func checkReads(t *testing.T, cases []string) {
	t.Helper()
	for _, c := range cases {
		if got := Shell(c); got.Classification != policy.Read {
			t.Errorf("%q should stay read: %+v", c, got)
		}
	}
}

// TestShellFailsClosedOnC1Holes locks the four fail-open holes of the final review's C1; every row
// was classified read before the fix.
func TestShellFailsClosedOnC1Holes(t *testing.T) {
	checkMutations(t, []hardeningCase{
		// (a) kubectl verbs that run in or connect to a container, and flags after `--`
		{"kubectl exec web -- ls --dry-run=client", "exec"},
		{"kubectl exec -it web -- sh -c 'kubectl apply --dry-run=server'", "exec"},
		{"kubectl cp web:/etc/passwd ./passwd --dry-run=client", "cp"},
		{"kubectl attach web --dry-run=client", "attach"},
		{"kubectl debug web --image=busybox --dry-run=client", "debug"},
		{"kubectl port-forward svc/web 8080:80 --dry-run=client", "port-forward"},
		{"kubectl proxy --dry-run=client", "proxy"},
		{"kubectl delete pod web -- --dry-run=client", "delete"},
		{"kubectl delete pod web --dry-run=client --dry-run=none", "delete"},
		// (b) assignment prefixes outside the safe list, in front of a command, alone, or after env
		{"PATH=/tmp/bin cat file.txt", "PATH"},
		{"LD_PRELOAD=/tmp/x.so ls", "LD_PRELOAD"},
		{"DYLD_INSERT_LIBRARIES=/tmp/x.dylib ls", "DYLD_INSERT_LIBRARIES"},
		{"GIT_EXTERNAL_DIFF=/tmp/x git diff", "GIT_EXTERNAL_DIFF"},
		{"env PATH=/tmp/bin cat file.txt", "PATH"},
		{"env TZ=UTC PATH=/tmp/bin cat file.txt", "PATH"},
		{"PATH=/tmp/bin; cat file.txt", "PATH"},
		{"FOO=bar env", "FOO"},
		// (b) the deferred Task 4 minor: an assignment alone in a background segment
		{"FOO=bar &", "background"},
		{"TZ=UTC & ls", "background"},
		// (c) a program word with a slash runs that file, not the allowlisted program
		{"./cat file.txt", "./cat"},
		{"bin/ls -la", "bin/ls"},
		{"/tmp/cat file.txt", "/tmp/cat"},
		{"~/bin/grep x file.txt", "~/bin/grep"},
		{"/usr/bin/../../tmp/cat file.txt", "/tmp/cat"},
		{"env ./cat file.txt", "./cat"},
		{"nohup ./ls", "./ls"},
		// (d) only the harmless device targets are allowed
		{"cat file.txt > /dev/foo", "/dev/foo"},
		{"echo x > /dev/sda", "/dev/sda"},
		{"echo x > /dev/tcp/example.com/80", "/dev/tcp"},
		{"cat file.txt 2> /dev/fd/3", "/dev/fd/3"},
	})
	checkReads(t, []string{
		"kubectl apply -f x.yaml --dry-run=client", "kubectl delete pod web --dry-run=server",
		"kubectl delete pod web --dry-run=none --dry-run=client", "kubectl delete pod web --dry-run none",
		"TZ=UTC date", "LANG=C sort names.txt", "LC_ALL=C grep -n x file.txt", "NO_COLOR=1 ls",
		"KUBECONFIG=/home/u/.kube/dev kubectl get pods", "AWS_PROFILE=dev aws s3 ls",
		"AWS_REGION=eu-west-1 aws ec2 describe-instances", "PAGER=cat git log -n 3", "env TZ=UTC date",
		"TZ=UTC env | grep TZ", "/bin/cat file.txt", "/usr/bin/grep -n x file.txt", "/bin/ls -la",
		"cat big.log > /dev/null", "ls 2> /dev/null", "ls &> /dev/null", "echo x > /dev/stderr",
		"echo x > /dev/stdout", "echo x > /dev/tty", "echo x > /dev/fd/1", "echo x 2> /dev/fd/2",
	})
}

// TestKubectlNeverReadVerbsAndDoubleDash covers the kubectl tool's path into the same classifier:
// the verb set that is never a read, and flags after `--` that belong to the command in the
// container.
func TestKubectlNeverReadVerbsAndDoubleDash(t *testing.T) {
	cases := []struct {
		verb, args string
		want       policy.Classification
	}{
		{"exec", "web -- ls --dry-run=client", policy.Mutate},
		{"EXEC", "web -- ls --dry-run=client", policy.Mutate},
		{"cp", "web:/a ./a --dry-run=client", policy.Mutate},
		{"delete", "pod web -- --dry-run=client", policy.Mutate},
		{"apply", "-f x.yaml -- --dry-run=client", policy.Mutate},
		{"apply", "-f x.yaml --dry-run=client", policy.Read},
		{"-n", "default exec web --dry-run=client -- ls", policy.Mutate},
		{"get", "pods -- --dry-run=client", policy.Read},
	}
	for _, c := range cases {
		if got := Kubectl(c.verb, strings.Fields(c.args)); got.Classification != c.want {
			t.Errorf("kubectl %s %s: %+v, want %s", c.verb, c.args, got, c.want)
		}
	}
	if _, ns := KubeTargets(strings.Fields("exec web -- env -n kube-system")); ns != "" {
		t.Errorf("a namespace after -- belongs to the command in the container: %q", ns)
	}
	if _, ns := KubeTargets(strings.Fields("exec web -n apps -- env -n kube-system")); ns != "apps" {
		t.Errorf("the namespace before -- is kubectl's: %q", ns)
	}
	if _, ns := KubeTargets(strings.Fields("exec web -- env -A")); ns != "" {
		t.Errorf("-A after -- is not kubectl's: %q", ns)
	}
}

// TestShellFailsClosedOnI5Commands: the mutating commands the final review's I5 found classified
// as reads, one row each; the reads next to them must stay reads.
func TestShellFailsClosedOnI5Commands(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"yq -i '.a = 1' x.yaml", "yq"},
		{"yq --inplace '.a = 1' x.yaml", "yq"},
		{"yq -Pi '.a = 1' x.yaml", "yq"},
		{"sort -o out.txt in.txt", "sort"},
		{"sort -ro out.txt in.txt", "sort"},
		{"sort --output=out.txt in.txt", "sort"},
		{"terraform fmt -diff", "fmt"},
		{"terraform fmt -check=false", "fmt"},
		{"ip link set lo up", "ip link set"},
		{"ip route add 10.0.0.0/8 dev lo", "ip route add"},
		{"ip addr flush dev eth0", "ip addr flush"},
		{"awk '{print | \"sh\"}' x", "awk"},
		{"awk 'BEGIN { \"date\" | getline d }'", "awk"},
		{"awk 'BEGIN { print \"x\" |& \"sh\" }'", "awk"},
		{"git -c diff.external=touch diff", "diff.external"},
		{"git -c core.fsmonitor=/tmp/x status", "core.fsmonitor"},
		{"sed 'w out.txt' in.txt", "sed"},
		{"sed -n '/x/w out.txt' in.txt", "sed"},
		{"sed 's/a/b/w out.txt' in.txt", "sed"},
		{"sed -e 's/a/b/' -e 'W out.txt' in.txt", "sed"},
		{"openssl req -new -key k.pem -out csr.pem", "openssl"},
		{"openssl x509 -in c.pem -out d.pem", "openssl"},
		{"git reflog expire --expire=now --all", "reflog"},
		{"git reflog delete HEAD@{1}", "reflog"},
		{"npm config set registry http://x", "npm config"},
		{"npm config delete registry", "npm config"},
		{"go env -w GOPROXY=direct", "go env"},
		{"go env -u GOPROXY", "go env"},
		{"journalctl --vacuum-size=1M", "journalctl"},
		{"journalctl --vacuum-time=1s", "journalctl"},
		{"journalctl --vacuum-files=1", "journalctl"},
		{"find . -fprint0 out.txt", "find"},
		{"curl --data-ascii @notes.txt https://example.com/x", "curl"},
		{"az aks get-credentials -n x -g y", "get-credentials"},
		{"gcloud container clusters get-credentials x --zone z", "get-credentials"},
	})
	checkReads(t, []string{
		"yq '.a' x.yaml", "yq -o json '.a' x.yaml", "yq -ojson '.a' x.yaml", "yq -P x.yaml",
		"sort -n names.txt", "sort -k2,2 -t, data.csv", "terraform fmt -check", "terraform fmt -check -diff",
		"terraform fmt -write=false", "ip a", "ip addr", "ip addr show dev eth0", "ip route", "ip r get 1.1.1.1",
		"ip link show", "ip -V", "awk '{print $1}' access.log", "awk '/error|warn/ {print}' app.log",
		"awk -F: '{print $1}' /etc/passwd", "awk '$1 == \"a|b\"' x", "awk 'a || b' x",
		"git -c color.ui=never log -n 3", "git -c COLOR.UI=never log -n 3", "git -c core.quotepath=off status",
		"sed -n '1,20p' main.go", "sed 's/a/b/g' x", "sed 's/w/W/g' x", "sed -e 's/a/b/' -e 's/c/d/' x",
		"sed '/^#/d' x", "sed 's|/usr|/opt|g' x", "sed 'y/abc/xyz/' x", "sed -n '/start/,/end/p' x",
		"openssl x509 -in c.pem -noout -text", "openssl req -in csr.pem -noout -text",
		"openssl s_client -connect example.com:443", "openssl dgst -sha256 x", "openssl version",
		"git reflog", "git reflog show HEAD", "npm config get registry", "npm config list", "go env",
		"go env GOPATH", "journalctl -u nginx --since '1 hour ago'", "find . -name '*.go' -print0",
		"curl -s https://example.com/health", "curl -fsSL https://example.com", "az aks show -n x -g y",
		"az aks list", "gcloud container clusters list", "gcloud container clusters describe x",
	})
}

// TestShellFailsClosedOnSameClassHoles: holes of the C1 and I5 class found while fixing them. Each
// runs a program, writes a file or sends data in a command the classifier called read.
func TestShellFailsClosedOnSameClassHoles(t *testing.T) {
	checkMutations(t, []hardeningCase{
		// bash (macOS /bin/sh) sends both streams to the file after >&; <(...) runs a command
		{"echo x >& out.txt", "out.txt"},
		{"echo x >&out.txt", "out.txt"},
		{"diff <(rm -rf x) y", "substitution"},
		// helm: flags after -- are release names, --dry-run=false is not a dry run
		{"helm uninstall web -- --dry-run", "uninstall"},
		{"helm upgrade web ./chart --dry-run=false", "upgrade"},
		{"helm upgrade web ./chart --dry-run --dry-run=false", "upgrade"},
		{"helm template web ./chart --post-renderer ./render.sh", "post-renderer"},
		{"helm install web ./chart --dry-run --post-renderer=./render.sh", "post-renderer"},
		{"helm template web ./chart --output-dir out", "output-dir"},
		// read-listed programs with a flag or an operand that runs a program or writes a file
		{"rg --pre ./x.sh pattern .", "rg"},
		{"rg --pre=./x.sh pattern .", "rg"},
		{"sort --compress-program=./x.sh -S 1 big.txt", "sort"},
		{"arch -arm64 rm -rf x", "arch"},
		{"uniq in.txt out.txt", "uniq"},
		{"uniq - out.txt", "uniq"},
		{"xxd in.bin out.txt", "xxd"},
		{"xxd -r in.hex out.bin", "xxd"},
		{"tree -o out.txt", "tree"},
		{"tree -R -H .", "tree"},
		{"less -o log.txt big.log", "less"},
		{"less --log-file=log.txt big.log", "less"},
		{"base64 -i in.bin -o out.txt", "base64"},
		{"xmllint --output out.xml in.xml", "xmllint"},
		{"xmllint --shell in.xml", "xmllint"},
		{"file -C -m magic", "file"},
		{"ack --pager=./x.sh pattern", "ack"},
		{"sed -ni 's/a/b/p' f.txt", "sed"},
		{"sed --in 's/a/b/' f.txt", "sed"},
		{"sed 's/.*/date/e' f.txt", "sed"},
		{"sed '1e id' f.txt", "sed"},
		{"sed -f script.sed f.txt", "sed"},
		{"awk -f prog.awk data.txt", "awk"},
		{"awk 'BEGIN { system (\"id\") }'", "awk"},
		{"gawk -e 'BEGIN { x = 1 }' -e 'BEGIN { system(\"id\") }'", "gawk"},
		{"gawk '@load \"filefuncs\"; BEGIN { }'", "gawk"},
		{"tar -tf x.tar -I ./x.sh", "tar"},
		{"tar -tf x.tar --use-compress-program=./x.sh", "tar"},
		{"tar -tf x.tar --checkpoint=1 --checkpoint-action=exec=id", "tar"},
		{"tar -tf x.tar --to-command=id", "tar"},
		{"git grep -Ovim pattern", "grep"},
		{"git grep --open-files-in-pager=vim pattern", "grep"},
		{"git diff --output=patch.txt", "output"},
		{"git log -p --output patch.txt", "output"},
		{"kubectl kustomize . --enable-exec --enable-alpha-plugins", "kustomize"},
		{"kubectl kustomize . --enable-helm --helm-command=./x.sh", "kustomize"},
		{"kubectl cluster-info dump --output-directory=dump", "cluster-info"},
		{"docker compose config -o out.yml", "compose"},
		{"docker buildx inspect --bootstrap", "buildx"},
		{"openssl dgst -sha256 -out digest.txt x", "openssl"},
		{"openssl req -new -newkey rsa:2048 -nodes -subj /CN=x", "openssl"},
		{"openssl x509 -req -in csr.pem -CA ca.pem -CAkey ca.key", "openssl"},
		{"openssl dgst -engine ./x.so -sha256 x", "openssl"},
		{"terraform plan -out=tf.plan", "plan"},
		{"terraform plan -generate-config-out=generated.tf", "plan"},
		{"terraform init -backend=false -backend=true", "init"},
		{"curl -c cookies.txt https://example.com", "curl"},
		{"curl -D headers.txt https://example.com", "curl"},
		{"curl --trace-ascii trace.txt https://example.com", "curl"},
		{"curl -K curl.cfg https://example.com", "curl"},
		{"curl -Q 'DELE x' ftp://example.com/", "curl"},
		{"curl --upload-f /etc/hosts https://example.com", "curl"},
		{"curl --req DELETE https://example.com/x", "curl"},
		{"curl -XDELETE https://example.com/x", "curl"},
		{"curl -sXPOST https://example.com/x", "curl"},
		{"wget -qO- -o log.txt https://example.com", "wget"},
		{"wget -qO- --post-data=a=b https://example.com", "wget"},
		{"wget -O- --method=DELETE https://example.com/x", "wget"},
		{"wget -qO- -e robots=off https://example.com", "wget"},
		{"gh run download 123", "gh run download"},
		{"gh release download v1", "gh release download"},
		{"apt-get download curl", "apt-get download"},
		{"aws s3api get-object --bucket b --key k out.json", "get-object"},
		{"ifconfig lo0 down", "ifconfig"},
		{"hostname new-name", "hostname"},
		{"dmesg -C", "dmesg"},
		{"ss -K dst 10.0.0.1", "ss"},
	})
	checkReads(t, []string{
		"echo x >&2", "ls 2>&1", "helm upgrade web ./chart --dry-run", "helm upgrade web ./chart --dry-run=server",
		"helm template web ./chart", "helm list -A", "rg -n pattern .", "rg --pretty pattern", "uniq -c names.txt",
		"uniq names.txt", "xxd in.bin", "tree -L 2", "less big.log", "base64 -d in.txt", "xmllint --noout in.xml",
		"file main.go", "ack pattern", "arch", "tar -tzf release.tgz", "tar -tvf x.tar", "git grep -n pattern",
		"git diff --stat", "git log --oneline -n 5", "git status", "kubectl kustomize .", "kubectl cluster-info",
		"kubectl cluster-info dump", "kubectl get pods -A", "docker compose config", "docker buildx inspect",
		"docker ps", "terraform plan -var-file=x.tfvars", "terraform plan", "terraform init -backend=false",
		"curl -sI https://example.com", "curl -XHEAD https://example.com", "curl -w '%{http_code}' https://example.com",
		"wget -qO- https://example.com", "wget -O - https://example.com", "gh run view 123", "gh pr list",
		"aws s3api head-object --bucket b --key k", "aws s3api get-object-acl --bucket b --key k", "ifconfig",
		"ifconfig -a", "ifconfig lo0", "hostname", "hostname -f", "dmesg -T", "ss -tulpn", "ls -la",
		"cat /etc/hosts", "grep -rn TODO .", "git log -n 3",
	})
}

// TestShellAssignmentOnlySegments: a segment that only sets variables names no program. With the
// safe variables it is a read, as before the fix wave (which made it panic in hostsIn); with any
// other variable it is a mutation.
func TestShellAssignmentOnlySegments(t *testing.T) {
	checkReads(t, []string{
		"TZ=UTC", "LANG=C", "TZ=UTC; date", "NO_COLOR=1 && ls", "KUBECONFIG=/x && kubectl get pods",
		"AWS_PROFILE=dev; aws s3 ls", "LC_ALL=C LANG=C", "PAGER=cat | cat",
	})
	checkMutations(t, []hardeningCase{
		{"PATH=/tmp/bin", "PATH"}, {"TZ=UTC FOO=bar", "FOO"}, {"FOO=bar; ls", "FOO"},
	})
	if got := hostsIn(nil); got != nil {
		t.Errorf("hostsIn of no words: %v", got)
	}
}

// TestSedBracketsAndBSDInPlaceClusters: a delimiter inside a bracket expression does not end a sed
// expression (a read stays a read), and BSD sed's boolean -l does not hide an -i in its cluster or
// swallow the script that follows it (both fail closed on every platform).
func TestSedBracketsAndBSDInPlaceClusters(t *testing.T) {
	checkReads(t, []string{"sed 's/[/]/_/g' paths.txt", "sed -n 's|[|]|/|gp' x", "sed -l -n p f.txt"})
	checkMutations(t, []hardeningCase{
		{"sed -li '' s/a/b/ f.txt", "sed -i"}, {"sed -i '' s/a/b/ f.txt", "sed -i"},
		{"sed -ni 's/a/b/p' f.txt", "sed -i"}, {"sed -l 'w out.txt' in.txt", "sed"},
		{"sed '/[/]/w out.txt' in.txt", "sed"},
	})
}
