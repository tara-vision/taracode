package agent

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
	a, srv, dir := gateAssistant(t, policy.ModeOperate,
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
	if _, err := os.Stat(filepath.Join(dir, "infra", "terraform.tfstate")); !os.IsNotExist(err) {
		t.Fatalf("the protected state file must never be written: %v", err)
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

func TestFailedDryRunIsRedactedBeforeTheModelAndTheAuditLog(t *testing.T) {
	fakeKubectl := "#!/bin/sh\nif [ \"$1\" = diff ]; then echo 'error: token AKIAIOSFODNN7EXAMPLE rejected'; exit 2; fi\nif [ \"$1 $2\" = 'config current-context' ]; then echo dev; exit 0; fi\nif [ \"$1 $2\" = 'config view' ]; then echo apps; exit 0; fi\necho \"kubectl $@\"\n"
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a, srv, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("kubectl", map[string]any{"verb": "apply", "args": "-f x.yaml"}), ollamatest.Turn{Content: "ok"})
	out := captureStdout(t, func() { _ = a.ProcessMessage("apply") })
	msg := messageContent(t, lastChatBody(t, srv), 0)
	if !strings.Contains(msg, "Dry run (kubectl_apply) failed") || strings.Contains(msg, "AKIAIOSFODNN7EXAMPLE") ||
		!strings.Contains(msg, "[redacted:aws-access-key]") {
		t.Fatalf("tool message %q", msg)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 1 || recs[0].Rule != "dry_run" || recs[0].Decision != "deny" || !recs[0].DryRun {
		t.Fatalf("audit %+v", recs)
	}
	raw, err := os.ReadFile(a.storage.AuditPath())
	if err != nil || strings.Contains(string(raw), "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(string(raw), "[redacted:aws-access-key]") {
		t.Fatalf("the audit log on disk must hold the redacted reason: %s %v", raw, err)
	}
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("the secret reached the screen:\n%s", out)
	}
}

func TestRememberedAnswerThatCannotBeSavedWarns(t *testing.T) {
	a, _, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("write_file", map[string]any{"path": "a.txt", "content": "1"}),
		toolCall("write_file", map[string]any{"path": "b.txt", "content": "2"}), ollamatest.Turn{Content: "ok"})
	storeDir := t.TempDir()
	perms, _, err := policy.LoadPermissions(filepath.Join(storeDir, "sub", "permissions.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A regular file where the store's directory must go makes every save fail.
	if err := os.WriteFile(filepath.Join(storeDir, "sub"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.permissions = perms
	asked := 0
	a.confirmPermission = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		asked++
		return ui.PermissionChoice{Allowed: true, Remember: true}
	}
	out := captureStdout(t, func() { _ = a.ProcessMessage("go") })
	if !strings.Contains(out, "Could not save the allow rule for write_file") || strings.Contains(out, "Saved: write_file") {
		t.Fatalf("a failed save must warn, not claim success:\n%s", out)
	}
	if asked != 1 || a.permissions.For("write_file") != policy.Allow {
		t.Fatalf("the answer still applies for this session: asked %d, rule %q", asked, a.permissions.For("write_file"))
	}
}

func TestRememberWithoutAPermissionStoreWarns(t *testing.T) {
	a, _, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("write_file", map[string]any{"path": "a.txt", "content": "1"}), ollamatest.Turn{Content: "ok"})
	a.permissions = nil
	a.confirmPermission = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		return ui.PermissionChoice{Allowed: true, Remember: true}
	}
	out := captureStdout(t, func() { _ = a.ProcessMessage("go") })
	if !strings.Contains(out, "not remembered") || !strings.Contains(out, "no permission store") {
		t.Fatalf("remember without a store must warn:\n%s", out)
	}
}

func TestPolicyModeGoesThroughSetMode(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.pol.Mode = policy.ModeOperate
	// A non-"built-in" source is what marks this as coming from an actual policy file; applyStartupMode
	// ignores pol.Mode otherwise (the built-in default's Mode is always "investigate" anyway).
	a.policySources = []string{"policy.yaml"}
	out := captureStdout(t, func() { a.applyStartupMode(Options{}) })
	if a.Mode() != policy.ModeInvestigate || !strings.Contains(out, "operate mode needs an initialised project") {
		t.Fatalf("operate from the policy without storage must be refused with a warning: %q\n%s", a.Mode(), out)
	}
	st, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	a.storage = st
	a.applyStartupMode(Options{})
	if a.Mode() != policy.ModeOperate || len(a.toolDefs) != 16 {
		t.Fatalf("operate from the policy with storage: %q, %d tools", a.Mode(), len(a.toolDefs))
	}
}

// TestApplyStartupModePrecedence covers the rest of the precedence switch
// TestPolicyModeGoesThroughSetMode does not: the --mode flag (Options.Mode) applied directly, the
// config default (Options.DefaultMode) applied when nothing else names a mode (and refused the
// same way without storage, warning and staying in investigate), and the flag winning over a
// policy file naming the opposite mode in both directions.
func TestApplyStartupModePrecedence(t *testing.T) {
	cases := []struct {
		name        string
		opts        Options
		withStorage bool
		policyMode  policy.Mode // "" = no policy file (a.policySources stays empty, so "built-in")
		wantMode    policy.Mode
		wantWarn    string // substring expected in the warning output; "" = none expected
	}{
		{
			name: "the flag applies with storage",
			opts: Options{Mode: policy.ModeOperate}, withStorage: true,
			wantMode: policy.ModeOperate,
		},
		{
			name: "the config default applies with storage",
			opts: Options{DefaultMode: policy.ModeOperate}, withStorage: true,
			wantMode: policy.ModeOperate,
		},
		{
			name: "the config default is refused without storage and stays investigate",
			opts: Options{DefaultMode: policy.ModeOperate}, withStorage: false,
			wantMode: policy.ModeInvestigate, wantWarn: "operate mode needs an initialised project",
		},
		{
			name: "the flag investigate wins over a policy file naming operate",
			opts: Options{Mode: policy.ModeInvestigate}, withStorage: true,
			policyMode: policy.ModeOperate, wantMode: policy.ModeInvestigate,
		},
		{
			name: "the flag operate wins over a policy file naming investigate",
			opts: Options{Mode: policy.ModeOperate}, withStorage: true,
			policyMode: policy.ModeInvestigate, wantMode: policy.ModeOperate,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestAssistant(t, false)
			if tc.withStorage {
				st, err := storage.NewManager(a.workingDir)
				if err != nil {
					t.Fatal(err)
				}
				a.storage = st
			}
			if tc.policyMode != "" {
				a.pol.Mode = tc.policyMode
				a.policySources = []string{"policy.yaml"}
			}

			out := captureStdout(t, func() { a.applyStartupMode(tc.opts) })

			if a.Mode() != tc.wantMode {
				t.Fatalf("Mode() = %q, want %q", a.Mode(), tc.wantMode)
			}
			if tc.wantWarn != "" && !strings.Contains(out, tc.wantWarn) {
				t.Fatalf("warning output = %q, want it to contain %q", out, tc.wantWarn)
			}
		})
	}
}

func TestAuditWithoutStorageWarns(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{
		toolCall("write_file", map[string]any{"path": "x.txt", "content": "hi"}), {Content: "ok"},
	}
	out := captureStdout(t, func() { _ = a.ProcessMessage("write") })
	if !strings.Contains(out, "Audit log unavailable") || !strings.Contains(out, "write_file") {
		t.Fatalf("a mutation that cannot be audited must say so:\n%s", out)
	}
}

// The first shake-out's malformed prod-context scale (ruling P3-R59): args repeats the command with
// another namespace than the namespace parameter, which the kubectl tool refuses as an argument error.
var (
	malformedProdScale = map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout",
		"namespace": "shop", "context": "prod-cluster", "args": "scale deployment checkout --replicas=3 -n prod-shop"}
	malformedProdScaleReason = `namespace is given both as a parameter ("shop") and in args ("prod-shop") with ` +
		`different values; use one`
)

// checkArgumentRefusal checks the event of a call refused for its arguments: not allowed, rule
// classifier, the verb the call named and that verb's own classification, the plain reason, no error.
func checkArgumentRefusal(t *testing.T, ev ToolEvent, verb string, class policy.Classification, reason string) {
	t.Helper()
	if ev.Allowed || ev.Rule != "classifier" || ev.Verb != verb || ev.Classification != class || ev.Reason != reason ||
		ev.Err != nil {
		t.Errorf("event %+v, want a classifier refusal of %s (%s): %s", ev, verb, class, reason)
	}
}

// checkAuditDeny checks one audit record: a deny by rule of the verb, in mode.
func checkAuditDeny(t *testing.T, rec storage.AuditRecord, rule, verb string, mode policy.Mode) {
	t.Helper()
	if rec.Decision != "deny" || rec.Rule != rule || rec.Verb != verb || rec.Classification != "mutate" ||
		rec.Mode != string(mode) {
		t.Errorf("audit %+v, want a %s deny of %s in %s mode", rec, rule, verb, mode)
	}
}

// TestKubectlArgumentErrorIsRefusedInOperateMode pins ruling P3-R59 (B2, closing F4) through the loop
// under the built-in policy: the malformed prod-context scale, which the policy used to allow with no
// verb and no targets, is refused at the gate with rule classifier and the plain reason, audited as a
// deny of scale, and never runs; the well-formed scale is still denied by the protected context.
func TestKubectlArgumentErrorIsRefusedInOperateMode(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	wellFormed := map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout", "namespace": "shop",
		"context": "prod-cluster", "args": "--replicas=3"}
	a, srv, _ := gateAssistant(t, policy.ModeOperate, toolCall("kubectl", malformedProdScale),
		toolCall("kubectl", wellFormed), ollamatest.Turn{Content: "ok"})
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	out := captureStdout(t, func() { _ = a.ProcessMessage("scale checkout to 3 on prod") })
	msgs := toolMessages(t, srv)
	if len(msgs) != 2 || msgs[0] != "Error: "+malformedProdScaleReason ||
		!strings.Contains(msgs[1], `Blocked by policy: kube context "prod-cluster"`) {
		t.Fatalf("tool messages %q", msgs)
	}
	if !strings.Contains(out, malformedProdScaleReason) {
		t.Errorf("the refusal is shown:\n%s", out)
	}
	if len(events) != 2 {
		t.Fatalf("events %+v", events)
	}
	checkArgumentRefusal(t, events[0], "scale", policy.Mutate, malformedProdScaleReason)
	if events[1].Allowed || events[1].Rule != "protected.kube_contexts" || events[1].Verb != "scale" {
		t.Errorf("the well-formed scale: %+v", events[1])
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 2 {
		t.Fatalf("audit %+v", recs)
	}
	checkAuditDeny(t, recs[0], "classifier", "scale", policy.ModeOperate)
	if r := recs[0]; r.Reason != malformedProdScaleReason || r.Targets["kube_context"] != "*" ||
		r.Targets["kube_namespace"] != "*" || !strings.HasSuffix(r.Command, "--replicas=3 -n prod-shop") {
		t.Errorf("the refusal's audit record %+v", r)
	}
	checkAuditDeny(t, recs[1], "protected.kube_contexts", "scale", policy.ModeOperate)
	if st := a.LastTurn(); st.ToolCalls != 2 || st.Denied != 2 {
		t.Errorf("turn %+v", st)
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("a refused scale must not run")
	}
}

// TestKubectlArgumentErrorIsRefusedInInvestigateMode pins ruling P3-R59 (B2): in investigate mode an
// argument error reaches the model as the plain reason, never as the mode rule's advice to switch
// modes; a malformed read is refused the same way without an audit record (the log holds mutations),
// and the shake-out's repeated command line runs, normalized.
func TestKubectlArgumentErrorIsRefusedInInvestigateMode(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	anotherVerb := map[string]any{"verb": "get", "args": "describe pod x"}
	repeated := map[string]any{"verb": "get", "resource": "pods", "namespace": "billing", "args": "get pods -n billing"}
	a, srv, _ := gateAssistant(t, policy.ModeInvestigate, toolCall("kubectl", malformedProdScale),
		toolCall("kubectl", anotherVerb), toolCall("kubectl", repeated), ollamatest.Turn{Content: "ok"})
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	_ = captureStdout(t, func() { _ = a.ProcessMessage("look at checkout") })
	verbReason := `args starts with "describe" but verb is "get"; args holds only extra flags ` +
		`(for example -l app=web --tail=100), never the verb, resource, name, namespace or context`
	msgs := toolMessages(t, srv)
	if len(msgs) != 3 || msgs[0] != "Error: "+malformedProdScaleReason || msgs[1] != "Error: "+verbReason ||
		msgs[2] != "pods" {
		t.Fatalf("tool messages %q", msgs)
	}
	if len(events) != 3 {
		t.Fatalf("events %+v", events)
	}
	checkArgumentRefusal(t, events[0], "scale", policy.Mutate, malformedProdScaleReason)
	checkArgumentRefusal(t, events[1], "get", policy.Read, verbReason)
	if !events[2].Allowed || events[2].Rule != "read" {
		t.Errorf("the normalized read must run: %+v", events[2])
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != 1 {
		t.Fatalf("only the mutating verb is audited: %+v", recs)
	}
	checkAuditDeny(t, recs[0], "classifier", "scale", policy.ModeInvestigate)
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("a refused scale must not run")
	}
}

// TestANamespaceObjectIsProtectedInEverySpelling pins ruling P3-R69 through the loop in operate mode
// under the built-in policy: deleting kube-system as a namespace object is denied by the protected
// namespace in every spelling, in the tool (one-word, multi-word and comma forms) and on a shell line;
// deleting another namespace is denied by the deny pattern, read canonically; a read of kube-system
// runs. Nothing that is denied reaches kubectl.
func TestANamespaceObjectIsProtectedInEverySpelling(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	const protected, denyPattern = "protected.kube_namespaces", "deny.commands"
	denied := []struct {
		tool string
		args map[string]any
		rule string
	}{
		{"kubectl", map[string]any{"verb": "delete", "args": "namespace/kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "namespace/kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "ns", "name": "kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "args": "ns kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "ns kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete ns", "name": "kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "ns,kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "namespaces", "name": "kube-system", "namespace": "default"},
			protected},
		{"kubectl", map[string]any{"verb": "label", "resource": "ns", "name": "kube-system", "args": "team=x"}, protected},
		{"shell", map[string]any{"command": "kubectl delete ns kube-system"}, protected},
		{"shell", map[string]any{"command": "kubectl delete namespace/kube-system"}, protected},
		{"shell", map[string]any{"command": "kubectl delete namespaces kube-system --wait=false"}, protected},
		{"shell", map[string]any{"command": "kubectl -n default delete ns/kube-system"}, protected},
		{"shell", map[string]any{"command": "kubectl delete -- ns kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "args": "-- ns kube-system"}, protected},
		{"shell", map[string]any{"command": "kubectl delete --raw /api/v1/namespaces/kube-system"}, protected},
		{"kubectl", map[string]any{"verb": "delete", "resource": "ns", "name": "shop"}, denyPattern},
		{"shell", map[string]any{"command": "kubectl delete ns/shop"}, denyPattern},
	}
	turns := make([]ollamatest.Turn, 0, len(denied)+2)
	for _, d := range denied {
		turns = append(turns, toolCall(d.tool, d.args))
	}
	turns = append(turns, toolCall("kubectl", map[string]any{"verb": "get", "resource": "ns", "name": "kube-system"}),
		ollamatest.Turn{Content: "ok"})
	a, srv, _ := gateAssistant(t, policy.ModeOperate, turns...)
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	_ = captureStdout(t, func() { _ = a.ProcessMessage("remove the namespaces") })
	if len(events) != len(denied)+1 {
		t.Fatalf("events %+v", events)
	}
	recs, _ := a.storage.ReadAudit("")
	if len(recs) != len(denied) {
		t.Fatalf("audit %+v", recs)
	}
	for i, d := range denied {
		if ev := events[i]; ev.Allowed || ev.Rule != d.rule {
			t.Errorf("%s %v: event %+v, want a %s deny", d.tool, d.args, ev, d.rule)
		}
		if recs[i].Decision != "deny" || recs[i].Rule != d.rule {
			t.Errorf("%s %v: audit %+v", d.tool, d.args, recs[i])
		}
	}
	if read := events[len(denied)]; !read.Allowed || read.Rule != "read" {
		t.Errorf("kubectl get ns kube-system stays a read: %+v", read)
	}
	if msgs := toolMessages(t, srv); msgs[len(msgs)-1] != "pods" {
		t.Errorf("the read runs: %q", msgs[len(msgs)-1])
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("a denied namespace deletion must not run")
	}
}

// TestKubectlPairsAndShellExpansionsAreProtected drives policy-fix round 2 through the loop in operate
// mode under the built-in policy: a label or annotate object that kubectl's rule for a change reads as
// an object ("-" and "=a" are names, so kube-system after them is changed, item 1), and a word the
// shell expands (a brace or a glob in a target word, item 5, or in the resource type, a value, the
// verb or the program word), are denied by the protected namespace, or by the protected contexts when
// the classifier cannot read kubectl as the program, in the tool and on a shell line. Nothing that is
// denied reaches kubectl.
func TestKubectlPairsAndShellExpansionsAreProtected(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	const namespaces, contexts = "protected.kube_namespaces", "protected.kube_contexts"
	denied := []struct {
		tool string
		args map[string]any
		rule string
	}{
		{"shell", map[string]any{"command": "kubectl label ns - kube-system team=x"}, namespaces},
		{"shell", map[string]any{"command": "kubectl annotate ns =a kube-system note=x"}, namespaces},
		{"shell", map[string]any{"command": "kubectl label ns -- - kube-system a=b"}, namespaces},
		{"kubectl", map[string]any{"verb": "label", "resource": "ns", "name": "- kube-system", "args": "team=x"}, namespaces},
		{"shell", map[string]any{"command": "kubectl delete --wait=false ns kube-sys{tem,}"}, namespaces},
		{"shell", map[string]any{"command": "kubectl label ns kube-sys{tem,} x=y"}, namespaces},
		{"shell", map[string]any{"command": "kubectl -n kube-sys{tem,} delete pod x"}, namespaces},
		{"shell", map[string]any{"command": "kubectl -n kube-syst* delete pod x"}, namespaces},
		{"shell", map[string]any{"command": "kubectl delete {ns,kube-system}"}, namespaces},
		{"shell", map[string]any{"command": "kubectl delete n{s,} kube-system"}, namespaces},
		{"shell", map[string]any{"command": "kubectl delete ns shop --grace-period {0,kube-system}"}, namespaces},
		{"shell", map[string]any{"command": "kubectl {delete,ns} kube-system"}, namespaces},
		{"shell", map[string]any{"command": "{kubectl,delete} ns kube-system"}, contexts},
	}
	turns := make([]ollamatest.Turn, 0, len(denied)+1)
	for _, d := range denied {
		turns = append(turns, toolCall(d.tool, d.args))
	}
	a, _, _ := gateAssistant(t, policy.ModeOperate, append(turns, ollamatest.Turn{Content: "ok"})...)
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	_ = captureStdout(t, func() { _ = a.ProcessMessage("relabel the namespaces") })
	recs, _ := a.storage.ReadAudit("")
	if len(events) != len(denied) || len(recs) != len(denied) {
		t.Fatalf("events %+v, audit %+v", events, recs)
	}
	for i, d := range denied {
		if ev := events[i]; ev.Allowed || ev.Rule != d.rule {
			t.Errorf("%s %v: event %+v, want a %s deny", d.tool, d.args, ev, d.rule)
		}
		if recs[i].Decision != "deny" || recs[i].Rule != d.rule {
			t.Errorf("%s %v: audit %+v", d.tool, d.args, recs[i])
		}
	}
	if _, err := os.Stat(ranMarker); !os.IsNotExist(err) {
		t.Fatal("a denied change must not run")
	}
}

// TestALabelChangeIsNotANamespace (round 2, item 2): under the built-in policy, a label or annotate
// change in the TYPE/NAME form is not a second object, so ns/shop with a change targets the one
// namespace shop and asks, as kubectl patches shop alone; it is not a protected-namespace deny.
func TestALabelChangeIsNotANamespace(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"shell", map[string]any{"command": "kubectl label ns/shop team=x"}},
		{"shell", map[string]any{"command": "kubectl annotate namespace/shop note=x"}},
		{"kubectl", map[string]any{"verb": "label", "resource": "ns/shop", "args": "team=x"}},
	}
	turns := make([]ollamatest.Turn, 0, len(calls)+1)
	for _, c := range calls {
		turns = append(turns, toolCall(c.tool, c.args))
	}
	a, _, _ := gateAssistant(t, policy.ModeOperate, append(turns, ollamatest.Turn{Content: "ok"})...)
	a.permissions, _, _ = policy.LoadPermissions("")
	var asked []policy.Invocation
	a.confirmPermission = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		asked = append(asked, inv)
		return ui.PermissionChoice{Allowed: true}
	}
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	_ = captureStdout(t, func() { _ = a.ProcessMessage("label the shop namespace") })
	if len(asked) != len(calls) || len(events) != len(calls) {
		t.Fatalf("each change must ask: asked %+v, events %+v", asked, events)
	}
	for i, c := range calls {
		if asked[i].Targets.KubeNamespace != "shop" || !events[i].Allowed || events[i].Rule != "policy" {
			t.Errorf("%s %v: asked %+v, event %+v; want the namespace shop, on to the prompt", c.tool, c.args, asked[i],
				events[i])
		}
	}
	if _, err := os.Stat(ranMarker); err != nil {
		t.Fatal("an allowed change runs")
	}
}

// TestANamespaceDeleteWithoutProtectionReachesThePrompt: with no protected namespace and no deny
// pattern, deleting a namespace is an ordinary mutation that asks, targeting that namespace.
func TestANamespaceDeleteWithoutProtectionReachesThePrompt(t *testing.T) {
	ranMarker := fakeKubeTools(t)
	setProcessKubeconfig(t, false)
	a, _, _ := gateAssistant(t, policy.ModeOperate,
		toolCall("kubectl", map[string]any{"verb": "delete", "resource": "namespace", "name": "shop"}),
		ollamatest.Turn{Content: "ok"})
	a.pol = policy.Policy{Version: policy.CurrentVersion, Mode: policy.ModeOperate}
	a.permissions, _, _ = policy.LoadPermissions("")
	var asked []policy.Invocation
	a.confirmPermission = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		asked = append(asked, inv)
		return ui.PermissionChoice{Allowed: true}
	}
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	_ = captureStdout(t, func() { _ = a.ProcessMessage("remove the shop namespace") })
	if len(asked) != 1 || asked[0].Verb != "delete" || asked[0].Targets.KubeNamespace != "shop" {
		t.Fatalf("the deletion must ask, targeting shop: %+v", asked)
	}
	if len(events) != 1 || !events[0].Allowed || events[0].Rule != "policy" {
		t.Fatalf("events %+v", events)
	}
	if _, err := os.Stat(ranMarker); err != nil {
		t.Fatal("an allowed deletion runs")
	}
}
