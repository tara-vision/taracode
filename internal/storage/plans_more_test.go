package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanLookupsReportWhatIsMissing(t *testing.T) {
	m, root := newManager(t)
	if err := m.UpdateTaskStatus("p", "t", TaskStatusCompleted); err == nil || err.Error() != "plan not found" {
		t.Fatalf("no plan: %v", err)
	}
	if err := m.ArchivePlan("p"); err == nil || err.Error() != "plan not found" {
		t.Fatalf("no plan: %v", err)
	}
	plan, err := m.CreatePlan("ship", []string{"build"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTaskStatus("other", plan.Tasks[0].ID, TaskStatusCompleted); err == nil {
		t.Fatal("another plan's ID")
	}
	if err := m.ArchivePlan("other"); err == nil {
		t.Fatal("another plan's ID")
	}
	active := filepath.Join(root, ".taracode", "plans", "active.json")
	if err := os.WriteFile(active, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetActivePlan(); err == nil || !strings.HasPrefix(err.Error(), "failed to parse plan: ") {
		t.Fatalf("broken plan: %v", err)
	}
	if err := m.UpdateTaskStatus(plan.ID, plan.Tasks[0].ID, TaskStatusCompleted); err == nil || err.Error() != "plan not found" {
		t.Fatalf("broken plan: %v", err)
	}
	if err := os.Remove(active); err != nil {
		t.Fatal(err)
	}
	if p, err := m.GetActivePlan(); p != nil || err != nil {
		t.Fatalf("the state names a plan whose file is gone: %v %v", p, err)
	}
}

func TestUpdateTaskStatusStampsOnlyCompletion(t *testing.T) {
	m, _ := newManager(t)
	plan, err := m.CreatePlan("ship", []string{"build", "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTaskStatus(plan.ID, plan.Tasks[1].ID, TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateTaskStatus(plan.ID, "no-such-task", TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	active, err := m.GetActivePlan()
	if err != nil || active.Tasks[0].Status != TaskStatusPending || active.Tasks[1].Status != TaskStatusInProgress ||
		active.Tasks[1].CompletedAt != nil {
		t.Fatalf("%+v %v", active, err)
	}
}

func TestArchivePlanKeepsTheArchivedPlan(t *testing.T) {
	m, root := newManager(t)
	plan, err := m.CreatePlan("ship", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.GetCurrentState().ActivePlanID != plan.ID || m.GetCurrentState().ActiveTaskID != "" {
		t.Fatalf("a plan without tasks has no active task: %+v", m.GetCurrentState())
	}
	active := filepath.Join(root, ".taracode", "plans", "active.json")
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("the active plan's file: %v", err)
	}
	if err := m.ArchivePlan(plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatalf("the archived plan is no longer the active one: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".taracode", "plans", "archive", "plan_"+plan.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var archived Plan
	if err := json.Unmarshal(data, &archived); err != nil || archived.Status != PlanStatusArchived || archived.Title != "ship" {
		t.Fatalf("%+v %v", archived, err)
	}
}

func TestPlanWritesReportFailures(t *testing.T) {
	m, root := newManager(t)
	plans := filepath.Join(root, ".taracode", "plans")
	plan, err := m.CreatePlan("ship", nil)
	if err != nil {
		t.Fatal(err)
	}
	replaceWithFile(t, filepath.Join(plans, "archive"))
	if err := m.ArchivePlan(plan.ID); err == nil {
		t.Fatal("an archive that is a file")
	}
	if active, _ := m.GetActivePlan(); active == nil {
		t.Fatal("a failed archive leaves the plan active")
	}
	replaceWithDir(t, filepath.Join(plans, "active.json"))
	if _, err := m.CreatePlan("again", nil); err == nil {
		t.Fatal("an active plan file that is a directory")
	}
}

func TestBackupFailures(t *testing.T) {
	m, root := newManager(t)
	if _, err := m.CreateBackup(filepath.Join(root, "missing.txt")); err == nil ||
		!strings.HasPrefix(err.Error(), "failed to read file for backup: ") {
		t.Fatalf("missing original: %v", err)
	}
	src := filepath.Join(root, "a.txt")
	if err := os.WriteFile(src, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	replaceWithFile(t, m.GetBackupDir())
	if _, err := m.CreateBackup(src); err == nil || !strings.HasPrefix(err.Error(), "failed to write backup: ") {
		t.Fatalf("backups is a file: %v", err)
	}
	if _, err := m.ListBackups("a.txt"); err == nil || !strings.HasPrefix(err.Error(), "failed to read backup directory: ") {
		t.Fatalf("backups is a file: %v", err)
	}
	if err := os.Remove(m.GetBackupDir()); err != nil {
		t.Fatal(err)
	}
	if list, err := m.ListBackups("a.txt"); list != nil || err != nil {
		t.Fatalf("no backups directory, no backups: %v %v", list, err)
	}
}

func TestListBackupsMatchesTheFileName(t *testing.T) {
	m, _ := newManager(t)
	dir := m.GetBackupDir()
	for _, name := range []string{"a.txt.100", "a.txt.200", "ab.txt.100", "b.txt.100"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "a.txt.300"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := m.ListBackups("a.txt")
	if err != nil || strings.Join(list, ",") != filepath.Join(dir, "a.txt.100")+","+filepath.Join(dir, "a.txt.200") {
		t.Fatalf("%v %v", list, err)
	}
}

func TestAuditWritesReportFailures(t *testing.T) {
	m, _ := newManager(t)
	if err := m.AppendAudit(AuditRecord{Time: farFuture}); err == nil || !strings.Contains(err.Error(), "year outside of range") {
		t.Fatalf("unencodable record: %v", err)
	}
	if _, err := os.Stat(m.AuditPath()); !os.IsNotExist(err) {
		t.Fatal("nothing is written for a record that cannot be encoded")
	}
	replaceWithDir(t, m.AuditPath())
	if err := m.AppendAudit(AuditRecord{Time: time.Now()}); err == nil {
		t.Fatal("a log that is a directory cannot be appended to")
	}
	if err := m.ClearAudit(); err == nil {
		t.Fatal("a log that is a non-empty directory cannot be cleared")
	}
}

// TestAuditLogIsPrivateAndOutlivesABrokenLine: the log is created readable by its owner only, and a
// malformed line in the middle does not hide the records after it.
func TestAuditLogIsPrivateAndOutlivesABrokenLine(t *testing.T) {
	m, _ := newManager(t)
	if err := m.AppendAudit(AuditRecord{Time: time.Now(), SessionID: "s1", Tool: "shell", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(m.AuditPath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the log's mode: %v %v", info, err)
	}
	f, err := os.OpenFile(m.AuditPath(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{broken\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.AppendAudit(AuditRecord{Time: time.Now(), SessionID: "s2", Tool: "shell", Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if recs, err := m.ReadAudit(""); err != nil || len(recs) != 2 || recs[0].SessionID != "s1" || recs[1].SessionID != "s2" {
		t.Fatalf("%+v %v", recs, err)
	}
}

func TestReadAuditReportsALogItCannotOpen(t *testing.T) {
	skipIfRoot(t)
	m, _ := newManager(t)
	if err := m.AppendAudit(AuditRecord{Time: time.Now(), Tool: "shell", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.AuditPath(), 0o000); err != nil {
		t.Fatal(err)
	}
	if recs, err := m.ReadAudit(""); err == nil || !os.IsPermission(err) || recs != nil {
		t.Fatalf("%v %v", recs, err)
	}
}
