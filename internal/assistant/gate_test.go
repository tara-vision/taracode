package assistant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

func gateAssistant(t *testing.T, mode policy.Mode, turns ...ollamatest.Turn) (*Assistant, *ollamatest.Server, string) {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = turns
	dir := t.TempDir()
	a := newForTest(dir, "gemma4:12b", srv.URL, false)
	st, err := storage.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.storage = st
	a.session, _ = st.CreateSession("")
	a.pol = policy.Default()
	a.permissions = policy.AllowAll()
	if err := a.SetMode(mode); err != nil {
		t.Fatal(err)
	}
	return a, srv, dir
}

func toolCall(name string, args map[string]any) ollamatest.Turn {
	return ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: name, Args: args}}}
}

// messageContent is the text of the message offsetFromEnd places from the end of a recorded chat
// request; offset 0 is the last tool result the model was sent.
func messageContent(t *testing.T, body map[string]any, offsetFromEnd int) string {
	t.Helper()
	content, _ := lastMessage(t, body, offsetFromEnd)["content"].(string)
	return content
}

func TestInvestigateModeBlocksMutationsWithoutRunningThem(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeInvestigate,
		toolCall("write_file", map[string]any{"path": "x.txt", "content": "hi"}), ollamatest.Turn{Content: "ok"})
	_ = captureStdout(t, func() { _ = a.ProcessMessage("write") })
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("the file must not be written")
	}
	body := lastChatBody(t, srv)
	if msg := messageContent(t, body, 0); !strings.Contains(msg, "investigate mode is read-only") || !strings.Contains(msg, "/mode operate") {
		t.Fatalf("tool message %q", msg)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 1 || recs[0].Decision != "deny" || recs[0].Rule != "mode" || recs[0].Tool != "write_file" {
		t.Fatalf("audit %+v", recs)
	}
	tools, _ := body["tools"].([]any)
	for _, tl := range tools {
		fn := tl.(map[string]any)["function"].(map[string]any)
		if fn["name"] == "write_file" {
			t.Fatal("write_file must not be offered in investigate mode")
		}
	}
}

func TestOperateModeDeniesProtectedPathsAndDenyPatterns(t *testing.T) {
	a, srv, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("write_file", map[string]any{"path": "infra/terraform.tfstate", "content": "{}"}),
		toolCall("shell", map[string]any{"command": "terraform destroy -auto-approve"}), ollamatest.Turn{Content: "ok"})
	_ = captureStdout(t, func() { _ = a.ProcessMessage("go") })
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 2 || recs[0].Rule != "protected.paths" || recs[1].Rule != "deny.commands" {
		t.Fatalf("audit %+v", recs)
	}
	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "deny pattern") {
		t.Fatalf("%q", msg)
	}
}

func TestOperateModeAsksAndRemembersTheAnswer(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeOperate,
		toolCall("write_file", map[string]any{"path": "a.txt", "content": "1"}),
		toolCall("write_file", map[string]any{"path": "b.txt", "content": "2"}), ollamatest.Turn{Content: "ok"})
	a.permissions, _, _ = policy.LoadPermissions(filepath.Join(dir, ".taracode", "permissions.json"))
	asked := 0
	a.confirmPermission = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		asked++
		if inv.Tool != "write_file" || len(inv.Targets.Paths) != 1 {
			t.Errorf("prompt got %+v", inv)
		}
		return ui.PermissionChoice{Allowed: asked > 1, Remember: asked > 1}
	}
	_ = captureStdout(t, func() { _ = a.ProcessMessage("go") })
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatal("a denied write must not happen")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("an approved write must happen")
	}
	if a.permissions.For("write_file") != policy.Allow {
		t.Fatal("remember must save the rule")
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 2 || recs[0].Rule != "user" || recs[0].Decision != "deny" || recs[1].Decision != "allow" {
		t.Fatalf("audit %+v", recs)
	}
	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "Wrote 1 bytes") {
		t.Fatalf("%q", msg)
	}
}

func TestDryRunRunsBeforeThePromptAndReadsNeverAsk(t *testing.T) {
	fakeKubectl := "#!/bin/sh\nif [ \"$1\" = diff ]; then echo '+ replicas: 2'; exit 1; fi\nif [ \"$1 $2\" = 'config current-context' ]; then echo dev; exit 0; fi\nif [ \"$1 $2\" = 'config view' ]; then echo apps; exit 0; fi\necho \"kubectl $@\"\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a, srv, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("kubectl", map[string]any{"verb": "get", "resource": "pods"}),
		toolCall("kubectl", map[string]any{"verb": "apply", "args": "-f x.yaml"}), ollamatest.Turn{Content: "ok"})
	var seen []string
	a.confirmPermission = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		seen = append(seen, inv.Verb)
		return ui.PermissionChoice{Allowed: true}
	}
	a.permissions, _, _ = policy.LoadPermissions("")
	out := captureStdout(t, func() { _ = a.ProcessMessage("go") })
	if strings.Join(seen, ",") != "apply" {
		t.Fatalf("only the mutation asks: %v", seen)
	}
	if !strings.Contains(out, "replicas: 2") {
		t.Fatalf("the dry run output must be shown before the prompt:\n%s", out)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 1 || !recs[0].DryRun || recs[0].Targets["kube_context"] != "dev" {
		t.Fatalf("audit %+v", recs)
	}
	if msg := messageContent(t, lastChatBody(t, srv), 0); !strings.Contains(msg, "kubectl apply -f x.yaml") {
		t.Fatalf("%q", msg)
	}
}

func TestToolOutputIsRedactedBeforeTheModelSeesIt(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeInvestigate,
		toolCall("read_file", map[string]any{"path": "creds.txt"}), ollamatest.Turn{Content: "ok"})
	if err := os.WriteFile(filepath.Join(dir, "creds.txt"), []byte("key=AKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = captureStdout(t, func() { _ = a.ProcessMessage("read") })
	if msg := messageContent(t, lastChatBody(t, srv), 0); strings.Contains(msg, "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(msg, "[redacted:aws-access-key]") {
		t.Fatalf("%q", msg)
	}
	if a.Redactions() != 1 {
		t.Fatalf("redactions %d", a.Redactions())
	}
}
