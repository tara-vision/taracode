//go:build classifydiff

package classify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestDifferential runs every read-classified table command under /bin/sh in a sentinel tree, with
// the programs that could reach a cluster, a cloud, the network or a package index replaced by
// shims that log their argv, and asserts that nothing in the tree changed and that every shim
// invocation of an infrastructure CLI is a read by that CLI's own classifier. A relaxation of the
// shell classifier that reopens a hole fails here. Run with: make classify-diff
func TestDifferential(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	shims := filepath.Join(root, "shims")
	logPath := filepath.Join(root, "shims.log")
	makeSentinelTree(t, tree)
	makeShims(t, shims)
	env := append(os.Environ(),
		"PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+root, "CLASSIFYDIFF_LOG="+logPath, "KUBECONFIG="+filepath.Join(root, "no-kubeconfig"))
	before := snapshot(t, tree)
	for _, command := range differentialCommands() {
		if res := Shell(command); res.Classification != policy.Read {
			t.Errorf("%q is classified %s (%s); the harness only runs reads", command, res.Classification, res.Reason)
			continue
		}
		runInTree(t, tree, env, command)
		if after := snapshot(t, tree); after != before {
			t.Errorf("%q changed the sentinel tree or the repository", command)
			before = after
		}
	}
	checkShimLog(t, logPath)
}

// shimPrograms are replaced on PATH by a script that logs its argv and exits 0.
var shimPrograms = []string{
	"kubectl", "helm", "terraform", "tofu", "docker", "podman", "nerdctl", "aws", "az", "gcloud", "gh",
	"microk8s", "k3s", "k0s", "minikube", "kubectx", "kubens",
	"curl", "wget", "dig", "nslookup", "host", "ping", "traceroute", "tracepath", "mtr", "ssh", "scp",
	"brew", "apt", "apt-get", "apt-cache", "yum", "dnf", "pip", "pip3", "npm", "pnpm", "yarn", "cargo",
	"systemctl", "journalctl", "launchctl", "crontab", "ip", "openssl",
}

// neverInvoked are shims a read must not call at all: they switch the kube context.
var neverInvoked = map[string]bool{"kubectx": true, "kubens": true}

const shimScript = `#!/bin/sh
{ printf '%s' "$(basename "$0")"; for a in "$@"; do printf '\t%s' "$a"; done; printf '\n'; } >> "$CLASSIFYDIFF_LOG"
exit 0
`

// differentialCommands are the read tables the package locks, deduplicated.
func differentialCommands() []string {
	seen := map[string]bool{}
	var out []string
	tables := [][]string{
		shellReadCases, everydayReads, relaxedReads, distroKubectlWrapperReads,
		c1HolesReads, i5CommandsReads, sameClassHolesReads, assignmentOnlySegmentReads,
		sedBracketsReads, sedScriptFileReads, variableExpansionReads,
		controlWordsReads, leadingGlobalFlagsReads, namespaceObjectReads,
	}
	for _, list := range tables {
		for _, c := range list {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// runInTree runs one command with empty stdin and a 10 s limit. The exit status is irrelevant: many
// table commands fail on the sentinel files, and a failure is not a mutation.
func runInTree(t *testing.T, tree string, env []string, command string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = tree
	cmd.Env = env
	cmd.Stdin = strings.NewReader("")
	_ = cmd.Run()
}

func makeShims(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range shimPrograms {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(shimScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// makeSentinelTree creates the files the table commands name, a tarball and a git repository with
// one commit, so reads have something to read and a write has something to change.
func makeSentinelTree(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"file.txt": "hello\n", "a": "a\n", "b": "b\n", "f": "line1\nline2\n", "x": "x\n", "x.txt": "x\n",
		"main.go": "package main\n", "access.log": "1.2.3.4 GET /\n", "app.log": "error: boom\nwarn: meh\n",
		"out.json": "{\"items\":[{\"metadata\":{\"name\":\"a\"}}]}\n", "data.txt": "1 2\n", "n": "1\n",
		"Makefile": "build:\n\t@echo building\n", "hello.txt": "hi\n", "big.log": "big\n", "ns": "shop\n",
		"sub/inner.txt": "inner\n", "infra/main.tf": "resource \"null_resource\" \"a\" {}\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tar := exec.Command("tar", "-czf", "release.tgz", "file.txt")
	tar.Dir = dir
	if out, err := tar.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.name=harness", "-c", "user.email=harness@example.com", "commit", "-qm", "sentinel"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

// snapshot hashes every path and content outside .git plus the repository's HEAD, stash list and
// porcelain status: a file write, a commit and a stash all show up; git's own index refresh on a
// read does not.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == ".git" {
			return filepath.SkipDir
		}
		_, _ = h.Write([]byte(rel + "\n"))
		if !d.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"rev-parse", "HEAD"}, {"stash", "list"}, {"status", "--porcelain"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, _ := cmd.Output()
		_, _ = h.Write(out)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// checkShimLog asserts every logged infrastructure invocation is a read by the dedicated
// classifier the tools use, and the context switchers never ran. Network and package shims are
// logged only: the classifier counts them as reads by design (spec 13.1).
func checkShimLog(t *testing.T, logPath string) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		argv := strings.Split(line, "\t")
		prog, rest := argv[0], argv[1:]
		if neverInvoked[prog] {
			t.Errorf("a read ran %s %v", prog, rest)
			continue
		}
		var res Result
		switch prog {
		case "microk8s", "k3s", "k0s", "minikube":
			args, ok := distroKubectl(prog, rest)
			if !ok {
				continue
			}
			res = Kubectl(first(args), tail(args))
		case "kubectl":
			res = Kubectl(first(rest), tail(rest))
		case "helm":
			res = Helm(rest)
		case "terraform", "tofu":
			res = Terraform(first(stripChdir(rest)), tail(stripChdir(rest)))
		case "docker", "podman", "nerdctl":
			res = Docker(rest)
		case "aws", "az", "gcloud":
			res = Cloud(prog, rest)
		default:
			continue
		}
		if res.Classification != policy.Read {
			t.Errorf("a read invoked %s %v, which the %s classifier calls a mutation (%s)", prog, rest, prog, res.Reason)
		}
	}
}
