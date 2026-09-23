package classify

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestLeadingGlobalFlagsAreNotTheVerb (pre-tag round A): kubectl, helm and docker take global options
// before the verb. The known ones are skipped with their values, so a read stays a read and a
// mutation keeps its verb and targets; an option before the verb that is not a known global stays a
// mutation (it may take a value that hides the verb, or write a file).
func TestLeadingGlobalFlagsAreNotTheVerb(t *testing.T) {
	checkReads(t, []string{
		"kubectl -n kube-system get pods", "kubectl --context prod-eu get pods", "helm -n kube-system list",
		"kubectl --context kind-dev -n kube-system get pods", "docker --context x ps",
		"kubectl --namespace=kube-system get pods", "kubectl -nkube-system get pods", "kubectl -n=kube-system get pods",
		"kubectl --kubeconfig ~/.kube/dev --context dev get pods", "kubectl -v 6 get pods", "kubectl -v=6 describe pod x",
		"kubectl --insecure-skip-tls-verify logs web", "kubectl -n apps rollout status deploy/web",
		"kubectl rollout -n apps history deploy/web", "kubectl --context dev config view",
		"kubectl --request-timeout 5s version", "helm --kube-context dev -n apps status web", "helm --debug list",
		"helm --kubeconfig ~/.kube/dev -n apps get values web", "docker -H unix:///var/run/docker.sock ps",
		"docker --context x compose ps", "docker -c x images", "docker --debug info",
	})
	checkMutations(t, []hardeningCase{
		{"kubectl -n kube-system delete pod x", "delete"},
		{"kubectl --context prod-eu -n apps apply -f x.yaml", "apply"},
		{"kubectl -n apps exec web -- ls", "exec"},
		{"kubectl -n apps rollout restart deploy/web", "restart"},
		{"kubectl --profile-output=x get pods", "--profile-output"},
		{"kubectl --cache-dir /tmp/c get pods", "--cache-dir"},
		{"helm -n apps uninstall web", "uninstall"},
		{"helm --repository-cache /tmp/c show chart repo/x", "--repository-cache"},
		{"docker --config ./cfg compose ps", "--config"},
		{"docker --context x rm web", "rm"},
	})
	for _, c := range []struct {
		name string
		got  Result
		verb string
	}{
		{"kubectl -n kube-system get pods", Kubectl("-n", strings.Fields("kube-system get pods")), "get"},
		{"kubectl --context=dev -n x describe pod y", Kubectl("--context=dev", strings.Fields("-n x describe pod y")), "describe"},
		{"helm -n kube-system list", Helm(strings.Fields("-n kube-system list")), "list"},
		{"docker --context x ps", Docker(strings.Fields("--context x ps")), "ps"},
	} {
		if c.got.Classification != policy.Read || c.got.Verb != c.verb {
			t.Errorf("%s: %+v, want a read of %s", c.name, c.got, c.verb)
		}
	}
	if got := Shell("kubectl -n kube-system delete pod x"); got.Verb != "delete" || len(got.Kube) != 1 ||
		got.Kube[0].Namespace != "kube-system" {
		t.Errorf("a mutation after global flags keeps its verb and namespace: %+v", got)
	}
}

// TestRepeatedAndGluedKubeFlags (pre-tag round D): kubectl and helm (pflag) apply the last value of a
// repeated option, so two different values give "*"; the same value twice is that value. A short
// option glued to its value (-nkube-system, -n=kube-system) or closing a cluster of boolean letters
// (-itn kube-system) names the namespace; an -n inside another option's value (-ojson) does not.
func TestRepeatedAndGluedKubeFlags(t *testing.T) {
	cases := []struct{ args, ctx, ns string }{
		{"delete pod x -n default -n kube-system", "", "*"},
		{"delete pod x -n kube-system --namespace=kube-system", "", "kube-system"},
		{"delete pod x --context kind-dev --context prod-eu", "*", ""},
		{"delete pod x --context=prod-eu --context prod-eu", "prod-eu", ""},
		{"delete pod x -nkube-system", "", "kube-system"},
		{"delete pod x -n=kube-system", "", "kube-system"},
		{"delete pod x --namespace=default -nkube-system", "", "*"},
		{"exec -itn kube-system web -- sh", "", "kube-system"},
		{"delete pods --all -Rn kube-system", "", "kube-system"},
		{"apply -f x.yaml -ojson", "", ""},
		{"apply -f x.yaml -oname -n apps", "", "apps"},
		{"delete pod x -ln kube-system", "", ""},
		{"delete pod x -n apps -A", "", "*"},
	}
	for _, c := range cases {
		if ctx, ns := KubeTargets(strings.Fields(c.args)); ctx != c.ctx || ns != c.ns {
			t.Errorf("KubeTargets(%s) = %q %q, want %q %q", c.args, ctx, ns, c.ctx, c.ns)
		}
	}
	helm := []struct{ args, ctx, ns string }{
		{"uninstall web -n default --namespace kube-system", "", "*"},
		{"upgrade web ./chart --kube-context a --kube-context b", "*", ""},
		{"upgrade -in kube-system web ./chart", "", "kube-system"},
		{"uninstall web -nkube-system", "", "kube-system"},
		{"uninstall web --kube-context=prod-eu -n apps", "prod-eu", "apps"},
	}
	for _, c := range helm {
		if ctx, ns := HelmTargets(strings.Fields(c.args)); ctx != c.ctx || ns != c.ns {
			t.Errorf("HelmTargets(%s) = %q %q, want %q %q", c.args, ctx, ns, c.ctx, c.ns)
		}
	}
}
