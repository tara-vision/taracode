package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

type verbCase struct {
	in   string
	want policy.Classification
	verb string
}

func check(t *testing.T, name string, fn func(tokens []string) Result, cases []verbCase) {
	t.Helper()
	for _, c := range cases {
		got := fn(strings.Fields(c.in))
		if got.Classification != c.want || (c.verb != "" && got.Verb != c.verb) {
			t.Errorf("%s %q: got %+v, want %s %s", name, c.in, got, c.want, c.verb)
		}
		if got.Classification == policy.Mutate && got.Reason == "" {
			t.Errorf("%s %q: mutate needs a reason", name, c.in)
		}
	}
}

func TestGit(t *testing.T) {
	check(t, "git", Git, []verbCase{
		{"status", policy.Read, "status"}, {"diff --stat HEAD~1", policy.Read, "diff"}, {"log -n 5 --oneline", policy.Read, "log"},
		{"branch", policy.Read, "branch"}, {"branch -a", policy.Read, "branch"}, {"branch --show-current", policy.Read, "branch"},
		{"branch feature", policy.Mutate, "branch"}, {"branch -d feature", policy.Mutate, "branch"},
		{"tag", policy.Read, "tag"}, {"tag -l v*", policy.Read, "tag"}, {"tag v1.0", policy.Mutate, "tag"},
		{"remote -v", policy.Read, "remote"}, {"remote add origin x", policy.Mutate, "remote"},
		{"config --get user.name", policy.Read, "config"}, {"config user.name x", policy.Mutate, "config"},
		{"stash list", policy.Read, "stash"}, {"stash", policy.Mutate, "stash"}, {"stash pop", policy.Mutate, "stash"},
		{"blame main.go", policy.Read, "blame"}, {"rev-parse HEAD", policy.Read, ""}, {"ls-files", policy.Read, ""},
		{"-C /tmp/x status", policy.Read, "status"}, {"--no-pager log", policy.Read, "log"},
		{"add .", policy.Mutate, "add"}, {"commit -m x", policy.Mutate, "commit"}, {"push", policy.Mutate, "push"},
		{"checkout -b x", policy.Mutate, "checkout"}, {"reset --hard", policy.Mutate, "reset"}, {"fetch", policy.Mutate, "fetch"},
		{"worktree list", policy.Read, ""}, {"worktree add x", policy.Mutate, ""},
	})
}

func TestKubectl(t *testing.T) {
	cases := []struct {
		verb, args string
		want       policy.Classification
	}{
		{"get", "pods -n apps", policy.Read}, {"describe", "deploy x", policy.Read}, {"logs", "pod/x --tail=50", policy.Read},
		{"events", "", policy.Read}, {"top", "pods", policy.Read}, {"explain", "deploy.spec", policy.Read},
		{"diff", "-f x.yaml", policy.Read}, {"api-resources", "", policy.Read}, {"version", "", policy.Read},
		{"apply", "-f x.yaml --dry-run=server", policy.Read}, {"apply", "-f x.yaml --dry-run=client", policy.Read},
		{"apply", "-f x.yaml --dry-run=none", policy.Mutate}, {"apply", "-f x.yaml", policy.Mutate},
		{"delete", "pod x", policy.Mutate}, {"patch", "deploy x -p {}", policy.Mutate}, {"exec", "x -- sh", policy.Mutate},
		{"rollout", "status deploy/x", policy.Read}, {"rollout", "history deploy/x", policy.Read}, {"rollout", "restart deploy/x", policy.Mutate},
		{"config", "current-context", policy.Read}, {"config", "get-contexts", policy.Read}, {"config", "use-context prod", policy.Mutate},
		{"auth", "can-i delete pods", policy.Read}, {"scale", "deploy x --replicas=3", policy.Mutate},
		{"cp", "x:/a ./a", policy.Mutate}, {"drain", "node1", policy.Mutate}, {"wait", "--for=condition=ready pod/x", policy.Read},
	}
	for _, c := range cases {
		got := Kubectl(c.verb, strings.Fields(c.args))
		if got.Classification != c.want {
			t.Errorf("kubectl %s %s: %+v", c.verb, c.args, got)
		}
	}
	ctx, ns := KubeTargets(strings.Fields("get pods --context gke-prod -n kube-system"))
	if ctx != "gke-prod" || ns != "kube-system" {
		t.Errorf("targets %q %q", ctx, ns)
	}
	ctx, ns = KubeTargets(strings.Fields("get pods --context=dev --namespace=apps"))
	if ctx != "dev" || ns != "apps" {
		t.Errorf("targets %q %q", ctx, ns)
	}
	if _, ns = KubeTargets(strings.Fields("get pods -A")); ns != "*" {
		t.Errorf("all namespaces %q", ns)
	}
}

func TestHelmTerraformDocker(t *testing.T) {
	check(t, "helm", Helm, []verbCase{
		{"list -A", policy.Read, "list"}, {"status x", policy.Read, "status"}, {"get values x", policy.Read, "get"},
		{"history x", policy.Read, "history"}, {"show chart repo/x", policy.Read, "show"}, {"template x repo/x", policy.Read, "template"},
		{"lint .", policy.Read, "lint"}, {"diff upgrade x repo/x", policy.Read, "diff"}, {"repo list", policy.Read, "repo"},
		{"repo add x https://x", policy.Mutate, "repo"}, {"install x repo/x", policy.Mutate, "install"},
		{"install x repo/x --dry-run", policy.Read, "install"}, {"upgrade x repo/x", policy.Mutate, "upgrade"},
		{"rollback x 1", policy.Mutate, "rollback"}, {"uninstall x", policy.Mutate, "uninstall"}, {"dependency list", policy.Read, "dependency"},
	})
	tf := []struct {
		cmd, args string
		want      policy.Classification
	}{
		{"init", "-backend=false", policy.Read}, {"init", "", policy.Mutate}, {"validate", "", policy.Read},
		{"fmt", "-check", policy.Read}, {"fmt", "", policy.Mutate}, {"plan", "-var-file=x", policy.Read},
		{"show", "", policy.Read}, {"state", "list", policy.Read}, {"state", "rm x", policy.Mutate},
		{"output", "-json", policy.Read}, {"graph", "", policy.Read}, {"apply", "", policy.Mutate},
		{"destroy", "", policy.Mutate}, {"import", "a b", policy.Mutate}, {"taint", "x", policy.Mutate},
		{"workspace", "list", policy.Read}, {"workspace", "delete x", policy.Mutate}, {"providers", "", policy.Read},
	}
	for _, c := range tf {
		if got := Terraform(c.cmd, strings.Fields(c.args)); got.Classification != c.want {
			t.Errorf("terraform %s %s: %+v", c.cmd, c.args, got)
		}
	}
	check(t, "docker", Docker, []verbCase{
		{"ps -a", policy.Read, "ps"}, {"images", policy.Read, "images"}, {"logs x --tail 50", policy.Read, "logs"},
		{"inspect x", policy.Read, "inspect"}, {"stats", policy.Read, "stats"}, {"compose ps", policy.Read, "compose"},
		{"compose -f x.yml config", policy.Read, "compose"}, {"compose logs web", policy.Read, "compose"},
		{"compose up -d", policy.Mutate, "compose"}, {"build -t x .", policy.Mutate, "build"}, {"run x", policy.Mutate, "run"},
		{"rm x", policy.Mutate, "rm"}, {"exec x sh", policy.Mutate, "exec"}, {"push x", policy.Mutate, "push"},
		{"network ls", policy.Read, "network"}, {"volume rm x", policy.Mutate, "volume"}, {"system df", policy.Read, "system"},
		{"system prune", policy.Mutate, "system"}, {"image ls", policy.Read, "image"}, {"version", policy.Read, "version"},
	})
}

func TestCloud(t *testing.T) {
	cases := []struct {
		provider, args string
		want           policy.Classification
		account        string
	}{
		{"aws", "ec2 describe-instances --profile prod-admin", policy.Read, "prod-admin"},
		{"aws", "s3 ls", policy.Read, ""}, {"aws", "sts get-caller-identity", policy.Read, ""},
		{"aws", "logs tail /aws/x --since 1h", policy.Read, ""}, {"aws", "ec2 wait instance-running", policy.Read, ""},
		{"aws", "s3 cp a s3://b", policy.Mutate, ""}, {"aws", "ec2 terminate-instances --instance-ids i-1", policy.Mutate, ""},
		{"aws", "configure list", policy.Read, ""}, {"aws", "configure set region x", policy.Mutate, ""},
		{"az", "group list", policy.Read, ""}, {"az", "vm show -n x -g y --subscription sub-1", policy.Read, "sub-1"},
		{"az", "aks get-credentials -n x -g y", policy.Read, ""}, {"az", "vm delete -n x -g y", policy.Mutate, ""},
		{"az", "account set -s x", policy.Mutate, ""}, {"az", "login", policy.Mutate, ""},
		{"gcloud", "compute instances list --project my-proj", policy.Read, "my-proj"},
		{"gcloud", "projects describe my-proj", policy.Read, ""}, {"gcloud", "config list", policy.Read, ""},
		{"gcloud", "container clusters get-credentials x --zone z", policy.Read, ""},
		{"gcloud", "compute instances delete vm --project=p", policy.Mutate, "p"}, {"gcloud", "auth login", policy.Mutate, ""},
		{"nope", "x", policy.Mutate, ""},
	}
	for _, c := range cases {
		tokens := strings.Fields(c.args)
		if got := Cloud(c.provider, tokens); got.Classification != c.want {
			t.Errorf("%s %s: %+v", c.provider, c.args, got)
		}
		if got := CloudAccount(c.provider, tokens); got != c.account {
			t.Errorf("%s %s: account %q", c.provider, c.args, got)
		}
	}
}
