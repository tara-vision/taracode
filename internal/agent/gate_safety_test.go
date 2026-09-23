package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/redact"
	"github.com/tara-vision/taracode/internal/ui"
)

// toolMessages returns the tool results of the last chat request, oldest first.
func toolMessages(t *testing.T, srv *ollamatest.Server) []string {
	t.Helper()
	messages, _ := lastChatBody(t, srv)["messages"].([]any)
	var out []string
	for _, m := range messages {
		if msg, ok := m.(map[string]any); ok && msg["role"] == "tool" {
			content, _ := msg["content"].(string)
			out = append(out, content)
		}
	}
	return out
}

// TestHiddenToolsCannotRun: the loop refuses a call to a tool the model was not offered (final
// review I3). With offline set, a repeated or invented call to a web tool never runs; in
// investigate mode an operate-only tool is refused with the mode message, whatever its arguments.
func TestHiddenToolsCannotRun(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeOperate,
		toolCall("web_fetch", map[string]any{"url": "https://example.com"}),
		toolCall("fetch_probe", map[string]any{}), ollamatest.Turn{Content: "ok"})
	red, _ := redact.New(redact.Options{})
	a.toolRegistry = tools.NewBuiltinRegistry(tools.Options{Offline: true, Redactor: red}, tools.Config{})
	ran := false
	a.toolRegistry.Register(&tools.Tool{Name: "fetch_probe", Description: "probe", ReadForm: true, External: true,
		Classify: func(map[string]any, string) policy.Invocation {
			return policy.Invocation{Tool: "fetch_probe", Classification: policy.Read}
		},
		Run: func(context.Context, map[string]any, string) (string, error) { ran = true; return "fetched", nil }})
	a.refreshTools()
	out := captureStdout(t, func() { _ = a.ProcessMessage("fetch") })
	if ran {
		t.Fatal("a hidden external tool must not run")
	}
	msgs := toolMessages(t, srv)
	if len(msgs) != 2 {
		t.Fatalf("every call gets a result: %q", msgs)
	}
	for _, msg := range msgs {
		if !strings.Contains(msg, "not available") || !strings.Contains(msg, "offline") {
			t.Errorf("refusal %q", msg)
		}
	}
	if !strings.Contains(out, "not available") {
		t.Errorf("the refusal is shown:\n%s", out)
	}
	for _, def := range a.toolDefs {
		if def.Function.Name == "web_fetch" || def.Function.Name == "fetch_probe" {
			t.Fatalf("%s must not be offered offline", def.Function.Name)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, srv2, _ := gateAssistant(t, policy.ModeInvestigate,
		toolCall("edit_file", map[string]any{"path": filepath.Join(dir, "a.txt"), "old": "one", "new": "1", "preview": true}),
		ollamatest.Turn{Content: "ok"})
	_ = captureStdout(t, func() { _ = b.ProcessMessage("preview") })
	if msg := messageContent(t, lastChatBody(t, srv2), 0); !strings.Contains(msg, "investigate mode is read-only") ||
		strings.Contains(msg, "+1") {
		t.Fatalf("an operate-only tool is refused in investigate mode: %q", msg)
	}
	if recs, _ := b.storage.ReadAudit(""); len(recs) != 1 || recs[0].Rule != "mode" {
		t.Fatalf("audit %+v", recs)
	}
}

// TestShellWritesToProtectedPathsAreDenied: with /permissions allow on the shell, a shell write to
// the policy file or a state file is still a hard deny, and nothing is written (final review I1).
func TestShellWritesToProtectedPathsAreDenied(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeOperate,
		toolCall("shell", map[string]any{"command": "echo 'mode: operate' > .taracode/policy.yaml"}),
		toolCall("shell", map[string]any{"command": "mkdir -p infra && cd infra && echo '{}' > terraform.tfstate"}),
		ollamatest.Turn{Content: "ok"})
	home := t.TempDir()
	t.Setenv("HOME", home)
	pol, _, err := policy.Load(dir, home)
	if err != nil {
		t.Fatal(err)
	}
	a.pol = pol
	_ = captureStdout(t, func() { _ = a.ProcessMessage("go") })
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 2 || recs[0].Rule != "protected.paths" || recs[1].Rule != "protected.paths" {
		t.Fatalf("audit %+v", recs)
	}
	for _, msg := range toolMessages(t, srv) {
		if !strings.Contains(msg, "protected path") {
			t.Errorf("refusal %q", msg)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".taracode", "policy.yaml")); !os.IsNotExist(err) {
		t.Fatalf("the policy file must not be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "infra")); !os.IsNotExist(err) {
		t.Fatalf("a denied command must not run at all: %v", err)
	}
}

// TestAllNamespaceDeleteIsDenied: kubectl delete --all -A touches kube-system, so the protected
// namespace denies it in operate mode (final review I2).
func TestAllNamespaceDeleteIsDenied(t *testing.T) {
	fakeKubectl := "#!/bin/sh\nif [ \"$1\" = config ]; then echo dev; exit 0; fi\n" +
		"touch \"$KUBECTL_RAN\"\necho \"kubectl $@\"\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	ranMarker := filepath.Join(t.TempDir(), "ran")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECTL_RAN", ranMarker)
	a, srv, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("kubectl", map[string]any{"verb": "delete", "resource": "pods", "args": "--all -A"}),
		ollamatest.Turn{Content: "ok"})
	_ = captureStdout(t, func() { _ = a.ProcessMessage("clean up") })
	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "every namespace") {
		t.Fatalf("%q", msg)
	}
	if recs, _ := a.storage.ReadAudit(""); len(recs) != 1 || recs[0].Rule != "protected.kube_namespaces" {
		t.Fatalf("audit %+v", recs)
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("the delete must not run")
	}
}

// TestInvestigateModeRefusesTheC1Holes: the four fail-open holes of the final review's C1, driven
// through the loop in investigate mode, are refused with the mode message and never run.
func TestInvestigateModeRefusesTheC1Holes(t *testing.T) {
	calls := []ollamatest.Turn{
		toolCall("kubectl", map[string]any{"verb": "exec", "name": "web", "args": "-- ls --dry-run=client"}),
		toolCall("shell", map[string]any{"command": "PATH=$PWD cat file.txt"}),
		toolCall("shell", map[string]any{"command": "./cat file.txt"}),
		toolCall("shell", map[string]any{"command": "cat file.txt > ./dev-out"}),
		toolCall("shell", map[string]any{"command": "cat file.txt > /dev/fd/9"}),
	}
	fakeKubectl := "#!/bin/sh\nif [ \"$1\" = config ]; then echo dev; exit 0; fi\ntouch \"$KUBECTL_RAN\"\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	kubectlRan := filepath.Join(t.TempDir(), "kubectl-ran")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECTL_RAN", kubectlRan)
	a, srv, dir := gateAssistant(t, policy.ModeInvestigate, append(calls, ollamatest.Turn{Content: "ok"})...)
	marker := filepath.Join(dir, "ran")
	if err := os.WriteFile(filepath.Join(dir, "cat"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { _ = a.ProcessMessage("look") })
	msgs := toolMessages(t, srv)
	if len(msgs) != len(calls) {
		t.Fatalf("every call gets a result: %q", msgs)
	}
	for i, msg := range msgs {
		if !strings.Contains(msg, "investigate mode is read-only") {
			t.Errorf("call %d: %q", i, msg)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a local program named after an allowlisted one must not run")
	}
	if _, err := os.Stat(kubectlRan); !os.IsNotExist(err) {
		t.Fatal("kubectl exec must not run")
	}
	if _, err := os.Stat(filepath.Join(dir, "dev-out")); !os.IsNotExist(err) {
		t.Fatal("the redirect must not run")
	}
}

// TestAClassifierPanicIsARefusalNotACrash: a classifier that panics must never take the session
// down; the loop refuses the call, with the panic in the reason, and audits the refusal.
func TestAClassifierPanicIsARefusalNotACrash(t *testing.T) {
	a, srv, _ := gateAssistant(t, policy.ModeOperate, toolCall("broken_probe", map[string]any{}),
		ollamatest.Turn{Content: "ok"})
	ran := false
	a.toolRegistry.Register(&tools.Tool{Name: "broken_probe", Description: "probe", ReadForm: true,
		Classify: func(map[string]any, string) policy.Invocation { panic("index out of range [1] with length 0") },
		Run:      func(context.Context, map[string]any, string) (string, error) { ran = true; return "ran", nil }})
	a.refreshTools()
	out := captureStdout(t, func() { _ = a.ProcessMessage("go") })
	if ran {
		t.Fatal("a call whose classifier panicked must not run")
	}
	msg := messageContent(t, lastChatBody(t, srv), 0)
	if !strings.Contains(msg, "classifier failed") || !strings.Contains(msg, "index out of range") {
		t.Fatalf("the refusal names the panic: %q", msg)
	}
	if !strings.Contains(out, "classifier failed") {
		t.Fatalf("the refusal is shown:\n%s", out)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 1 || recs[0].Decision != "deny" || recs[0].Rule != "classifier" || recs[0].Classification != "mutate" {
		t.Fatalf("audit %+v", recs)
	}
}

// fakeKubeTools puts a kubectl and a helm on PATH that answer the target resolution with kind-dev,
// print "pods" for a get, and otherwise touch the returned marker: a denied call must never make it.
func fakeKubeTools(t *testing.T) (marker string) {
	t.Helper()
	fake := "#!/bin/sh\nif [ \"$1\" = config ]; then echo kind-dev; exit 0; fi\n" +
		"for a in \"$@\"; do if [ \"$a\" = get ]; then echo pods; exit 0; fi; done\ntouch \"$KUBE_RAN\"\n"
	bin := t.TempDir()
	for _, name := range []string{"kubectl", "helm"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fake), 0o755); err != nil { //nolint:gosec // test binary
			t.Fatal(err)
		}
	}
	marker = filepath.Join(t.TempDir(), "ran")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBE_RAN", marker)
	return marker
}

// setProcessKubeconfig puts taracode's own KUBECONFIG in one of its two states: exported (naming a
// small kubeconfig file) or not set at all.
func setProcessKubeconfig(t *testing.T, exported bool) {
	t.Helper()
	t.Setenv("KUBECONFIG", "") // restores the caller's value after the test
	if !exported {
		if err := os.Unsetenv("KUBECONFIG"); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "env.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", path)
}

// TestShellRunKubectlHitsProtectedNamespaces drives ruling P2-R34 through the loop: with shell on
// allow, a kubectl or helm mutation run through the shell in a protected namespace or context is a
// hard deny and never runs; a shell kubectl read still runs. A plain KUBECONFIG= assignment earlier
// on the line is never trusted, whether or not taracode's own environment exports KUBECONFIG
// (pre-tag round F: both states are driven).
func TestShellRunKubectlHitsProtectedNamespaces(t *testing.T) {
	for _, exported := range []bool{false, true} {
		t.Run(map[bool]string{false: "KUBECONFIG unset", true: "KUBECONFIG exported"}[exported], func(t *testing.T) {
			ranMarker := fakeKubeTools(t)
			setProcessKubeconfig(t, exported)
			dev := filepath.Join(t.TempDir(), "dev.yaml")
			if err := os.WriteFile(dev, []byte("apiVersion: v1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			a, srv, _ := gateAssistant(t, policy.ModeOperate,
				toolCall("shell", map[string]any{"command": "kubectl -n kube-system delete pod coredns"}),
				toolCall("shell", map[string]any{"command": "helm uninstall web --kube-context prod-eu"}),
				toolCall("shell", map[string]any{"command": "KUBECONFIG=" + dev + "; kubectl delete pod web -n apps"}),
				toolCall("shell", map[string]any{"command": "kubectl get pods -n kube-system"}),
				ollamatest.Turn{Content: "ok"})
			_ = captureStdout(t, func() { _ = a.ProcessMessage("clean up") })
			msgs := toolMessages(t, srv)
			if len(msgs) != 4 || !strings.Contains(msgs[0], "kube-system") || !strings.Contains(msgs[1], "prod-eu") ||
				!strings.Contains(msgs[2], "kube context") || !strings.Contains(msgs[3], "pods") {
				t.Fatalf("tool messages %q", msgs)
			}
			recs, _ := a.storage.ReadAudit("")
			if len(recs) != 3 || recs[0].Rule != "protected.kube_namespaces" || recs[1].Rule != "protected.kube_contexts" ||
				recs[2].Rule != "protected.kube_contexts" {
				t.Fatalf("audit %+v", recs)
			}
			if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
				t.Fatal("a denied kubectl or helm mutation must not run")
			}
		})
	}
}

// TestShellKubeTargetsHoldThroughTheLoop drives the pre-tag round through the loop in operate mode
// with the built-in policy and shell on allow: a kubectl or helm mutation behind a shell keyword or
// a subshell (B), after a context switch (C), with a repeated namespace (D), in HELM_NAMESPACE (E)
// or with a kubeconfig that is not a small regular file (G) is a hard deny and never runs; a read
// with leading global flags (A) runs.
func TestShellKubeTargetsHoldThroughTheLoop(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	denied := []struct{ command, rule string }{
		{"for p in a; do kubectl delete pod $p -n kube-system; done", "protected.kube_namespaces"},
		{"(kubectl -n kube-system delete pod x)", "protected.kube_namespaces"},
		{"if true; then helm uninstall web -n kube-system; fi", "protected.kube_namespaces"},
		{"kubectl config use-context prod-eu && kubectl delete pod web", "protected.kube_contexts"},
		{"kubens kube-system && kubectl delete pod coredns", "protected.kube_contexts"},
		{"kubectl delete pod web -n default -n kube-system", "protected.kube_namespaces"},
		{"kubectl delete pod web -nkube-system", "protected.kube_namespaces"},
		{"HELM_NAMESPACE=kube-system helm uninstall web", "protected.kube_namespaces"},
		{"kubectl delete pod web --kubeconfig /dev/zero", "protected.kube_contexts"},
	}
	turns := make([]ollamatest.Turn, 0, len(denied)+2)
	for _, d := range denied {
		turns = append(turns, toolCall("shell", map[string]any{"command": d.command}))
	}
	turns = append(turns, toolCall("shell", map[string]any{"command": "kubectl --context kind-dev -n kube-system get pods"}),
		ollamatest.Turn{Content: "ok"})
	a, srv, _ := gateAssistant(t, policy.ModeOperate, turns...)
	_ = captureStdout(t, func() { _ = a.ProcessMessage("clean up") })
	msgs := toolMessages(t, srv)
	if len(msgs) != len(denied)+1 || !strings.Contains(msgs[len(denied)], "pods") {
		t.Fatalf("tool messages %q", msgs)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != len(denied) {
		t.Fatalf("audit %+v", recs)
	}
	for i, d := range denied {
		if recs[i].Decision != "deny" || recs[i].Rule != d.rule || !strings.Contains(msgs[i], "Blocked by policy") {
			t.Errorf("%q: audit %+v, message %q, want a %s deny", d.command, recs[i], msgs[i], d.rule)
		}
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("a denied kubectl or helm mutation must not run")
	}
}

// TestHelmPostRendererIsRefusedBeforeThePrompt: in operate mode a helm upgrade with a post-renderer
// fails its required dry run, so the call is refused and audited before any prompt and helm never
// runs the renderer (ruling P2-R34).
func TestHelmPostRendererIsRefusedBeforeThePrompt(t *testing.T) {
	fake := "#!/bin/sh\nif [ \"$1\" = config ]; then echo kind-dev; exit 0; fi\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fake), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	ranMarker := filepath.Join(t.TempDir(), "helm-ran")
	if err := os.WriteFile(filepath.Join(bin, "helm"), []byte("#!/bin/sh\ntouch "+ranMarker+"\n"), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECONFIG", "")
	a, srv, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("helm", map[string]any{"args": "upgrade web ./chart -n apps --post-renderer ./render.sh"}),
		ollamatest.Turn{Content: "ok"})
	asked := false
	a.confirmPermission = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		asked = true
		return ui.PermissionChoice{Allowed: true}
	}
	a.permissions, _, _ = policy.LoadPermissions("")
	_ = captureStdout(t, func() { _ = a.ProcessMessage("upgrade") })
	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "--post-renderer") {
		t.Fatalf("%q", msg)
	}
	if asked {
		t.Fatal("the refusal comes before the prompt")
	}
	if recs, _ := a.storage.ReadAudit(""); len(recs) != 1 || recs[0].Decision != "deny" || recs[0].Rule != "dry_run" {
		t.Fatalf("audit %+v", recs)
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("helm must not run")
	}
}
