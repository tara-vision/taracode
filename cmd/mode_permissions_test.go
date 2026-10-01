package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

func TestModeWithoutArgumentsShowsTheMode(t *testing.T) {
	r, _ := projectREPL(t)
	out := captureStdoutForTest(t, func() { r.dispatch("/mode") })
	want := fmt.Sprintf("Mode: investigate (%d tools exposed)", r.asst.ToolRegistry().Available(policy.ModeInvestigate))
	if !strings.Contains(out, want) || !strings.Contains(out, "Usage: /mode investigate | operate") ||
		strings.Contains(out, "locked") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/mode yolo") })
	if !strings.Contains(out, `Unknown mode "yolo" (investigate or operate)`) || r.asst.Mode() != policy.ModeInvestigate {
		t.Fatalf("%q", out)
	}
}

// brokenPolicyREPL is an initialised repl whose project policy does not parse.
func brokenPolicyREPL(t *testing.T) *repl {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".taracode"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".taracode", "policy.yaml"), "version: 1\nprotected:\n  pths: []\n")
	return replOn(t, fakeOllama(t), dir, false)
}

func TestABrokenPolicyIsShownByModeAndPolicy(t *testing.T) {
	r := brokenPolicyREPL(t)
	out := captureStdoutForTest(t, func() { r.dispatch("/mode") })
	if !strings.Contains(out, "Operate mode is locked: ") || !strings.Contains(out, "pths") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/policy show") })
	if !strings.Contains(out, "Policy error (operate mode locked): ") || strings.Contains(out, "Sources:") {
		t.Fatalf("%q", out)
	}
}

func TestPolicyShowListsTheRememberedPermissions(t *testing.T) {
	r, _ := projectREPL(t)
	out := captureStdoutForTest(t, func() { r.dispatch("/policy") })
	if !strings.Contains(out, "Usage: /policy show") {
		t.Fatalf("%q", out)
	}
	if err := r.asst.Permissions().Set("write_file", policy.Allow); err != nil {
		t.Fatal(err)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/policy show") })
	if !strings.Contains(out, "Sources: built-in") || !strings.Contains(out, "Remembered permissions:") ||
		!strings.Contains(out, "  write_file     allow") {
		t.Fatalf("%q", out)
	}
}

func TestPermissionsNeedAnInitialisedProject(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	if out := captureStdoutForTest(t, func() { r.dispatch("/permissions") }); !strings.Contains(out, "No permission store: run /init first.") {
		t.Fatalf("%q", out)
	}
}

func TestPermissionsSetListAndReset(t *testing.T) {
	r, _ := projectREPL(t)
	perms := r.asst.Permissions()
	tests := []struct {
		command, want string
	}{
		{"/permissions allow", "Usage: /permissions allow|deny|ask <tool|all> | reset"},
		{"/permissions maybe write_file", "Usage: /permissions allow|deny|ask <tool|all> | reset"},
		{"/permissions allow nope", `Unknown tool "nope"; see /tools`},
		{"/permissions deny write_file", ui.IconSuccess + " write_file -> deny"},
		{"/permissions allow all", ui.IconSuccess + " all -> allow"},
		{"/permissions", "Permission rules for mutations (reads never ask; default: ask):"},
	}
	for _, tt := range tests {
		if out := captureStdoutForTest(t, func() { r.dispatch(tt.command) }); !strings.Contains(out, tt.want) {
			t.Errorf("%s lacks %q: %q", tt.command, tt.want, out)
		}
	}
	if perms.For("write_file") != policy.Deny || perms.For("edit_file") != policy.Allow {
		t.Fatalf("rules %v", perms.Rules())
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/permissions") })
	if !strings.Contains(out, "  write_file     deny") || !strings.Contains(out, "  *              allow") {
		t.Fatalf("the listing lacks a rule: %q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/permissions reset") })
	if !strings.Contains(out, "Permissions reset: every mutation asks again.") || len(perms.Rules()) != 0 {
		t.Fatalf("rules %v, output %q", perms.Rules(), out)
	}
}

func TestPermissionsReportASaveError(t *testing.T) {
	skipIfRoot(t)
	r, _ := projectREPL(t)
	if err := r.asst.Permissions().Set("write_file", policy.Ask); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(r.projectRoot, ".taracode", "permissions.json")
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o644) })
	for _, command := range []string{"/permissions deny write_file", "/permissions reset"} {
		if out := captureStdoutForTest(t, func() { r.dispatch(command) }); !strings.Contains(out, ui.IconError+" ") ||
			!strings.Contains(out, "permission denied") {
			t.Errorf("%s: %q", command, out)
		}
	}
}

// auditREPL is an initialised repl with two audit records: one in its own session, one elsewhere.
func auditREPL(t *testing.T) *repl {
	t.Helper()
	r, _ := projectREPL(t)
	at := time.Date(2026, 10, 1, 14, 5, 9, 0, time.Local)
	st := r.asst.GetStorage()
	for _, rec := range []storage.AuditRecord{
		{Time: at, SessionID: r.asst.GetSession().ID, Tool: "kubectl", Command: "kubectl apply -f web.yaml",
			Decision: "allow", Rule: "permission"},
		{Time: at, SessionID: "an-older-session", Tool: "write_file", Targets: map[string]string{"paths": "notes.txt"},
			Decision: "deny", Rule: "protected.paths"},
	} {
		if err := st.AppendAudit(rec); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestAuditListsTheSessionsMutations(t *testing.T) {
	ephemeral := replOn(t, fakeOllama(t), t.TempDir(), true)
	if out := captureStdoutForTest(t, func() { ephemeral.dispatch("/audit") }); !strings.Contains(out, "No audit log: run /init first.") {
		t.Fatalf("%q", out)
	}
	empty, _ := projectREPL(t)
	if out := captureStdoutForTest(t, func() { empty.dispatch("/audit") }); !strings.Contains(out, "No mutations recorded in this session") {
		t.Fatalf("%q", out)
	}
	if out := captureStdoutForTest(t, func() { empty.dispatch("/audit all") }); !strings.Contains(out, "No mutations recorded.\n") {
		t.Fatalf("%q", out)
	}

	r := auditREPL(t)
	out := captureStdoutForTest(t, func() { r.dispatch("/audit") })
	if !strings.Contains(out, "  14:05:09  allow permission             kubectl    kubectl apply -f web.yaml") ||
		strings.Contains(out, "notes.txt") {
		t.Fatalf("/audit:\n%s", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/audit all") })
	if !strings.Contains(out, "kubectl apply -f web.yaml") ||
		!strings.Contains(out, "  14:05:09  deny  protected.paths        write_file notes.txt") {
		t.Fatalf("/audit all:\n%s", out)
	}
}

func TestAuditExportAndClear(t *testing.T) {
	r := auditREPL(t)
	t.Chdir(t.TempDir())
	out := captureStdoutForTest(t, func() { r.dispatch("/audit export json") })
	matches, err := filepath.Glob("taracode-audit-*.json")
	if err != nil || len(matches) != 1 || !strings.Contains(out, "Exported 2 records to "+matches[0]) {
		t.Fatalf("files %v err=%v output %q", matches, err, out)
	}
	var records []storage.AuditRecord
	if err := json.Unmarshal([]byte(readFile(t, matches[0])), &records); err != nil || len(records) != 2 ||
		records[0].Command != "kubectl apply -f web.yaml" {
		t.Fatalf("export %+v err=%v", records, err)
	}

	out = captureStdoutForTest(t, func() { r.dispatch("/audit clear") })
	if !strings.Contains(out, "Audit log cleared.") {
		t.Fatalf("%q", out)
	}
	if recs, err := r.asst.GetStorage().ReadAudit(""); err != nil || len(recs) != 0 {
		t.Fatalf("after clear: %+v %v", recs, err)
	}
}

func TestAuditReportsFileErrors(t *testing.T) {
	skipIfRoot(t)
	r := auditREPL(t)
	log := r.asst.GetStorage().AuditPath()
	if err := os.Chmod(log, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(log, 0o600) })
	for _, command := range []string{"/audit", "/audit export json"} {
		if out := captureStdoutForTest(t, func() { r.dispatch(command) }); !strings.Contains(out, ui.IconError+" ") ||
			!strings.Contains(out, "permission denied") {
			t.Errorf("%s: %q", command, out)
		}
	}
	if err := os.Chmod(log, 0o600); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Chmod(cwd, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cwd, 0o755) })
	if out := captureStdoutForTest(t, func() { r.dispatch("/audit export json") }); !strings.Contains(out, ui.IconError+" ") {
		t.Errorf("an export into a read-only directory: %q", out)
	}

	taracode := filepath.Dir(log)
	if err := os.Chmod(taracode, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taracode, 0o755) })
	if out := captureStdoutForTest(t, func() { r.dispatch("/audit clear") }); !strings.Contains(out, ui.IconError+" ") ||
		strings.Contains(out, "cleared") {
		t.Errorf("a clear that cannot remove the log: %q", out)
	}
}
