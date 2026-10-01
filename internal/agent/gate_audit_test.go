package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
)

func TestCallSummaryNamesARefusedCall(t *testing.T) {
	long := strings.Repeat("x", maxCallSummary+10)
	tests := []struct {
		call *ToolCall
		want string
	}{
		{&ToolCall{Tool: "shell", Params: map[string]any{"command": "ls -la"}}, "ls -la"},
		{&ToolCall{Tool: "shell", Params: map[string]any{"command": long}}, long[:maxCallSummary] + "..."},
		{&ToolCall{Tool: "kubectl", Params: map[string]any{"verb": "get"}}, `kubectl {"verb":"get"}`},
		{&ToolCall{Tool: "odd", Params: map[string]any{"c": make(chan int)}}, "odd"},
	}
	for _, tt := range tests {
		if got := callSummary(tt.call); got != tt.want {
			t.Errorf("callSummary(%v) = %q, want %q", tt.call.Params, got, tt.want)
		}
	}
}

// TestASavedDenyRuleRefusesAndIsAudited: a tool whose saved permission is deny never runs, the
// model is told why, and the audit log records the denial.
func TestASavedDenyRuleRefusesAndIsAudited(t *testing.T) {
	a, srv, dir := gateAssistant(t, policy.ModeOperate,
		toolCall("write_file", map[string]any{"path": "x.txt", "content": "hi"}), ollamatest.Turn{Content: "ok"})
	if err := a.permissions.Set("write_file", policy.Deny); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := a.ProcessMessage("write x.txt"); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); !os.IsNotExist(err) {
		t.Fatal("a denied write must not run")
	}
	if got := messageContent(t, lastChatBody(t, srv), 0); got != "Tool 'write_file' is denied by the saved permission rule" {
		t.Fatalf("the model was told %q", got)
	}
	if !strings.Contains(out, "Tool 'write_file' blocked by permission settings") {
		t.Fatalf("%q", out)
	}
	recs, err := a.storage.ReadAudit("")
	if err != nil || len(recs) != 1 || recs[0].Decision != "deny" || recs[0].Rule != "permission" {
		t.Fatalf("audit %+v err=%v", recs, err)
	}
}

func TestAuditRecordsEveryTarget(t *testing.T) {
	a, _, _ := gateAssistant(t, policy.ModeOperate)
	inv := policy.Invocation{Tool: "cloud", Verb: "delete", Classification: policy.Mutate, Command: "aws s3 rb s3://b",
		Targets: policy.Targets{KubeContext: "prod", KubeNamespace: "shop", CloudAccount: "123456789012",
			Paths: []string{"/a", "/b"}, Hosts: []string{"api.example.com"}}}
	a.audit(inv, "allow", "policy", "", true)
	recs, err := a.storage.ReadAudit(a.GetSession().ID)
	if err != nil || len(recs) != 1 {
		t.Fatalf("audit %+v err=%v", recs, err)
	}
	want := map[string]string{"kube_context": "prod", "kube_namespace": "shop", "cloud_account": "123456789012",
		"paths": "/a,/b", "hosts": "api.example.com"}
	rec := recs[0]
	for k, v := range want {
		if rec.Targets[k] != v {
			t.Errorf("target %s = %q, want %q", k, rec.Targets[k], v)
		}
	}
	if rec.Mode != "operate" || rec.Verb != "delete" || rec.Classification != "mutate" || !rec.DryRun {
		t.Fatalf("record %+v", rec)
	}
}

func TestAuditReportsWhatItCouldNotRecord(t *testing.T) {
	a, _, dir := gateAssistant(t, policy.ModeOperate)
	var out bytes.Buffer
	a.out = &out
	a.session = nil
	if os.Geteuid() != 0 {
		taracode := filepath.Join(dir, ".taracode")
		if err := os.Chmod(taracode, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(taracode, 0o755) })
		a.audit(policy.Invocation{Tool: "write_file"}, "allow", "policy", "", false)
		if !strings.Contains(out.String(), "Audit log write failed") {
			t.Fatalf("%q", out.String())
		}
	}
	a.storage = nil
	out.Reset()
	a.audit(policy.Invocation{Tool: "write_file"}, "deny", "mode", "", false)
	if !strings.Contains(out.String(), "Audit log unavailable (no project storage): the deny decision for write_file was not recorded") {
		t.Fatalf("%q", out.String())
	}
}

func TestAFailingToolIsAnErrorResult(t *testing.T) {
	a, srv, _ := gateAssistant(t, policy.ModeInvestigate,
		toolCall("read_file", map[string]any{"path": "missing.txt"}), ollamatest.Turn{Content: "not there"})
	_ = captureStdout(t, func() {
		if err := a.ProcessMessage("read it"); err != nil {
			t.Error(err)
		}
	})
	if got := messageContent(t, lastChatBody(t, srv), 0); !strings.HasPrefix(got, "Error: ") || !strings.Contains(got, "missing.txt") {
		t.Fatalf("the tool result %q", got)
	}
}

// TestTheTestAssistantAllowsWhatItIsAsked: newForTest answers every permission prompt with allow,
// so a test that does not script the prompt still runs the mutation it is about.
func TestTheTestAssistantAllowsWhatItIsAsked(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.permissions = nil // no saved rules: every mutation asks
	enterOperate(t, a)
	srv.Turns = []ollamatest.Turn{toolCall("write_file", map[string]any{"path": "x.txt", "content": "hi"}),
		{Content: "written"}}
	_ = captureStdout(t, func() {
		if err := a.ProcessMessage("write x.txt"); err != nil {
			t.Error(err)
		}
	})
	if data, err := os.ReadFile(filepath.Join(a.workingDir, "x.txt")); err != nil || string(data) != "hi" {
		t.Fatalf("x.txt = %q, %v", data, err)
	}
}
