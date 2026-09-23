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
