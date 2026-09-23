package tools

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

// loggingKubectl answers the resolution like fakeKubeconfigKubectl, names the namespace of an
// explicit --context ns-<context>, and appends every invocation to $KUBECTL_LOG.
const loggingKubectl = `#!/bin/sh
echo "$@" >> "$KUBECTL_LOG"
if [ "$1 $2" = "config current-context" ]; then
  if [ -n "$KUBECONFIG" ]; then echo "ctx-$(basename "$KUBECONFIG")"; else echo kind-dev; fi
  exit 0
fi
if [ "$1 $2" = "config view" ]; then
  for a in "$@"; do if [ "$prev" = "--context" ]; then echo "ns-$a"; exit 0; fi; prev="$a"; done
  echo team-a; exit 0
fi
echo "kubectl $@"
`

// kubectlLog installs loggingKubectl and returns a function that reads what it was asked to run.
func kubectlLog(t *testing.T) func() []string {
	t.Helper()
	fakeBin(t, "kubectl", loggingKubectl)
	log := filepath.Join(t.TempDir(), "kubectl.log")
	t.Setenv("KUBECTL_LOG", log)
	processKubeconfig(t, false)
	return func() []string {
		data, _ := os.ReadFile(log) //nolint:gosec // the test's own log
		return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), " ", "_"))
	}
}

// kubeTargetsOf classifies one call and returns its kube context and namespace.
func kubeTargetsOf(tool *Tool, args map[string]any, dir string) string {
	inv := tool.Classify(args, dir)
	return inv.Targets.KubeContext + " " + inv.Targets.KubeNamespace
}

// TestANamedKubeconfigMustBeASmallRegularFile (pre-tag round G): a kubeconfig the command names
// that is not a regular file under 1 MiB (a device such as /dev/zero, a FIFO, a directory, a
// missing or an oversized file) gives "*" for the context and namespace, and kubectl is never run
// to read it.
func TestANamedKubeconfigMustBeASmallRegularFile(t *testing.T) {
	ran := kubectlLog(t)
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.yaml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.yaml")
	if err := os.WriteFile(big, make([]byte, 1<<20+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"/dev/zero", fifo, dir, filepath.Join(dir, "missing.yaml"), big} {
		for _, c := range []struct {
			tool *Tool
			args map[string]any
		}{
			{ShellTool(nil), map[string]any{"command": "kubectl delete pod web --kubeconfig " + bad}},
			{ShellTool(nil), map[string]any{"command": "KUBECONFIG=" + bad + " helm uninstall web"}},
			{KubectlTool(), map[string]any{"verb": "delete", "resource": "pod", "name": "web", "args": "--kubeconfig " + bad}},
			{HelmTool(), map[string]any{"args": "uninstall web --kubeconfig=" + bad}},
		} {
			if got := kubeTargetsOf(c.tool, c.args, dir); got != "* *" {
				t.Errorf("%s %v: targets %q, want * *", c.tool.Name, c.args, got)
			}
		}
	}
	if calls := ran(); len(calls) != 0 {
		t.Fatalf("kubectl must not run for a kubeconfig that is not a small regular file: %q", calls)
	}
	good := writeKubeconfig(t, dir, "good.yaml")
	if got := kubeTargetsOf(ShellTool(nil), map[string]any{"command": "kubectl delete pod web --kubeconfig " + good}, dir); got != "ctx-good.yaml team-a" {
		t.Errorf("a small regular file is read: %q", got)
	}
}

// TestAFailedResolutionIsEveryContext (pre-tag round G): when kubectl is missing, fails to load the
// kubeconfig or times out, the context and namespace a command does not name are "*" on the shell
// path and on the dedicated kubectl and helm tools, never an empty context that no protected
// pattern matches.
func TestAFailedResolutionIsEveryContext(t *testing.T) {
	calls := []struct {
		tool *Tool
		args map[string]any
	}{
		{ShellTool(nil), map[string]any{"command": "kubectl delete pod web"}},
		{ShellTool(nil), map[string]any{"command": "helm uninstall web"}},
		{KubectlTool(), map[string]any{"verb": "delete", "resource": "pod", "name": "web"}},
		{HelmTool(), map[string]any{"args": "uninstall web"}},
	}
	check := func(t *testing.T, want string, n int) {
		t.Helper()
		for _, c := range calls[:n] {
			inv := c.tool.Classify(c.args, t.TempDir())
			if got := inv.Targets.KubeContext + " " + inv.Targets.KubeNamespace; got != want {
				t.Errorf("%s %v: targets %q, want %q", c.tool.Name, c.args, got, want)
			}
			if v := policy.Default().Evaluate(policy.ModeOperate, inv); v.Allow {
				t.Errorf("%s %v: an unresolved target must hit the protected lists: %+v", c.tool.Name, c.args, v)
			}
		}
	}
	processKubeconfig(t, false)
	t.Run("kubeconfig does not load", func(t *testing.T) {
		fakeBin(t, "kubectl", "#!/bin/sh\necho 'error: invalid configuration' >&2\nexit 1\n")
		check(t, "* *", len(calls))
	})
	t.Run("kubectl missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		check(t, "* *", len(calls))
	})
	t.Run("timeout", func(t *testing.T) {
		fakeBin(t, "kubectl", "#!/bin/sh\nexec sleep 10\n")
		start := time.Now()
		check(t, "* *", 2) // a timeout costs kubeResolveTimeout each: the shell and the kubectl tool
		if elapsed := time.Since(start); elapsed > 15*time.Second {
			t.Errorf("each resolution is bounded by its timeout: %s", elapsed)
		}
	})
	t.Run("no current context", func(t *testing.T) {
		fakeBin(t, "kubectl", "#!/bin/sh\nif [ \"$2\" = current-context ]; then echo 'error: current-context is not set' >&2; exit 1; fi\necho team-a\n")
		check(t, "* *", len(calls))
	})
}

// TestResolutionRunsOncePerKubeconfig (pre-tag round G): the kubeconfig is read once per
// classification however many kubectl and helm commands of the line use it.
func TestResolutionRunsOncePerKubeconfig(t *testing.T) {
	ran := kubectlLog(t)
	inv := ShellTool(nil).Classify(map[string]any{"command": "kubectl delete pod a; kubectl delete pod b; helm uninstall c"}, t.TempDir())
	if inv.Targets.KubeContext != "kind-dev" || inv.Targets.KubeNamespace != "team-a" {
		t.Fatalf("targets %+v", inv.Targets)
	}
	calls := strings.Join(ran(), " ")
	if strings.Count(calls, "config_current-context") != 1 || strings.Count(calls, "config_view") != 1 {
		t.Fatalf("one current-context and one view per kubeconfig: %q", calls)
	}
}

// TestAnExplicitContextResolvesItsOwnNamespace (pre-tag round G): a command that names a context
// but no namespace acts in the default namespace of that context, not of the current one.
func TestAnExplicitContextResolvesItsOwnNamespace(t *testing.T) {
	kubectlLog(t)
	dir := t.TempDir()
	for _, c := range []struct {
		tool *Tool
		args map[string]any
	}{
		{ShellTool(nil), map[string]any{"command": "kubectl --context staging delete pod web"}},
		{ShellTool(nil), map[string]any{"command": "helm uninstall web --kube-context staging"}},
		{KubectlTool(), map[string]any{"verb": "delete", "resource": "pod", "name": "web", "context": "staging"}},
		{HelmTool(), map[string]any{"args": "uninstall web --kube-context staging"}},
	} {
		if got := kubeTargetsOf(c.tool, c.args, dir); got != "staging ns-staging" {
			t.Errorf("%s %v: targets %q, want staging ns-staging", c.tool.Name, c.args, got)
		}
	}
}

// TestResolutionDoesNotWaitForAChildHoldingThePipe (pre-tag round G): a kubectl that exits but
// leaves a child holding its output open cannot keep the classification waiting: the wait for the
// pipe is bounded (WaitDelay), and the unfinished resolution counts as failed.
func TestResolutionDoesNotWaitForAChildHoldingThePipe(t *testing.T) {
	processKubeconfig(t, false)
	fakeBin(t, "kubectl", "#!/bin/sh\nsleep 8 &\necho kind-dev\n")
	start := time.Now()
	inv := ShellTool(nil).Classify(map[string]any{"command": "kubectl delete pod web"}, t.TempDir())
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("the resolution waited %s for a child that held the pipe", elapsed)
	}
	if inv.Targets.KubeContext != "*" || inv.Targets.KubeNamespace != "*" {
		t.Fatalf("an unfinished resolution is every context: %+v", inv.Targets)
	}
}

// TestHelmNamespaceAndContextFromTheEnvironment (pre-tag round E): helm reads HELM_NAMESPACE and
// HELM_KUBECONTEXT from its environment when no flag names them, so a helm mutation run by the
// helm tool or through the shell acts there; a flag still wins.
func TestHelmNamespaceAndContextFromTheEnvironment(t *testing.T) {
	kubectlLog(t)
	dir := t.TempDir()
	evaluate := func(tool *Tool, args map[string]any) policy.Verdict {
		inv := tool.Classify(args, dir)
		inv.WorkingDir = dir
		return policy.Default().Evaluate(policy.ModeOperate, inv)
	}
	t.Setenv("HELM_NAMESPACE", "kube-system")
	for _, c := range []struct {
		tool *Tool
		args map[string]any
	}{
		{HelmTool(), map[string]any{"args": "uninstall web"}},
		{ShellTool(nil), map[string]any{"command": "helm uninstall web"}},
		{ShellTool(nil), map[string]any{"command": "HELM_NAMESPACE=kube-system helm uninstall web --kube-context kind-dev"}},
	} {
		if v := evaluate(c.tool, c.args); v.Allow || v.Rule != "protected.kube_namespaces" {
			t.Errorf("%s %v: %+v, want protected.kube_namespaces", c.tool.Name, c.args, v)
		}
	}
	if v := evaluate(HelmTool(), map[string]any{"args": "uninstall web -n apps"}); !v.Allow {
		t.Errorf("-n wins over HELM_NAMESPACE: %+v", v)
	}
	if v := evaluate(ShellTool(nil), map[string]any{"command": "kubectl delete pod web"}); !v.Allow {
		t.Errorf("kubectl does not read HELM_NAMESPACE: %+v", v)
	}
	t.Setenv("HELM_NAMESPACE", "")
	t.Setenv("HELM_KUBECONTEXT", "prod-eu")
	for _, c := range []struct {
		tool *Tool
		args map[string]any
	}{
		{HelmTool(), map[string]any{"args": "uninstall web -n apps"}},
		{ShellTool(nil), map[string]any{"command": "helm uninstall web -n apps"}},
	} {
		if v := evaluate(c.tool, c.args); v.Allow || v.Rule != "protected.kube_contexts" {
			t.Errorf("%s %v: %+v, want protected.kube_contexts", c.tool.Name, c.args, v)
		}
	}
}
