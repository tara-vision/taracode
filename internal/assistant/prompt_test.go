package assistant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/storage"
)

// TestBuildSystemPromptFullSecurityPrompt covers the one prompt pairing the other tests never
// reach: security mode without native tools, which picks the full securitySystemPrompt (the one
// that teaches the JSON-in-content tool format) rather than its compact counterpart.
func TestBuildSystemPromptFullSecurityPrompt(t *testing.T) {
	dir := t.TempDir()

	prompt := buildSystemPromptWithModeAndTools(dir, nil, storage.ModeSecurity, false)

	if !strings.Contains(prompt, "SECURITY MODE") || !strings.Contains(prompt, "TOOL FORMAT") {
		t.Fatalf("expected the full security prompt (with TOOL FORMAT):\n%s", prompt)
	}
}

// TestBuildSystemPromptPicksCompactOrFullByNativeTools pins the whole point of the two prompt
// pairs: native function calling needs no JSON-in-content tool examples, so the compact prompt
// stays well under the size of the one that teaches the fallback format.
func TestBuildSystemPromptPicksCompactOrFullByNativeTools(t *testing.T) {
	dir := t.TempDir()

	compact := buildSystemPromptWithModeAndTools(dir, nil, storage.ModeDevOps, true)
	if len(compact) >= 2000 {
		t.Fatalf("compact prompt is %d chars, want under 2000:\n%s", len(compact), compact)
	}
	if strings.Contains(compact, "TOOL FORMAT") {
		t.Fatalf("compact prompt should not teach JSON-in-content tool calls:\n%s", compact)
	}

	full := buildSystemPromptWithModeAndTools(dir, nil, storage.ModeDevOps, false)
	if !strings.Contains(full, "TOOL FORMAT") {
		t.Fatalf("full prompt should teach JSON-in-content tool calls:\n%s", full)
	}
	if len(full) <= len(compact) {
		t.Fatalf("full prompt (%d chars) should be longer than the compact one (%d chars)", len(full), len(compact))
	}
}

// TestBuildSystemPromptAppendsTaracodeMD covers the project-context injection: a TARACODE.md in
// the working directory is folded into the prompt verbatim.
func TestBuildSystemPromptAppendsTaracodeMD(t *testing.T) {
	dir := t.TempDir()
	content := "Project rule: always run `make test` before committing."
	if err := os.WriteFile(filepath.Join(dir, "TARACODE.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPromptWithModeAndTools(dir, nil, storage.ModeDevOps, true)

	if !strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Fatalf("prompt missing the PROJECT CONTEXT header:\n%s", prompt)
	}
	if !strings.Contains(prompt, content) {
		t.Fatalf("prompt missing the TARACODE.md content:\n%s", prompt)
	}
}

// TestBuildSystemPromptAppendsWorkingDirectory covers the trailing "Current working directory"
// line every prompt ends with, whatever mode or tool setting produced it.
func TestBuildSystemPromptAppendsWorkingDirectory(t *testing.T) {
	dir := t.TempDir()

	prompt := buildSystemPromptWithModeAndTools(dir, nil, storage.ModeDevOps, true)

	want := fmt.Sprintf("Current working directory: %s", dir)
	if !strings.HasSuffix(prompt, want) {
		t.Fatalf("prompt does not end with the working directory line:\n%s", prompt)
	}
}

// TestSetModeSwitchesPromptAndRejectsUnknown covers all three branches of SetMode: switching to
// security rebuilds the prompt, initializes the audit log (a real session is attached so that
// branch actually runs) and updates the conversation's system message; switching back to devops
// does the same without an audit log; an unrecognized mode is refused without touching either.
func TestSetModeSwitchesPromptAndRejectsUnknown(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	a.storage, a.session = store, session

	if err := a.SetMode("security"); err != nil {
		t.Fatalf("SetMode(security) = %v", err)
	}
	if a.GetMode() != storage.ModeSecurity {
		t.Fatalf("GetMode() = %q, want security", a.GetMode())
	}
	if !strings.Contains(a.systemPrompt, "SECURITY MODE") {
		t.Fatalf("system prompt not switched to security mode:\n%s", a.systemPrompt)
	}
	if a.conversation[0].Content != a.systemPrompt {
		t.Fatalf("conversation's system message was not updated with the new prompt")
	}
	auditLog, err := store.GetAuditLog(session.ID)
	if err != nil || auditLog == nil {
		t.Fatalf("audit log not initialized on entering security mode: log=%v err=%v", auditLog, err)
	}

	if err := a.SetMode("devops"); err != nil {
		t.Fatalf("SetMode(devops) = %v", err)
	}
	if a.GetMode() != storage.ModeDevOps {
		t.Fatalf("GetMode() = %q, want devops", a.GetMode())
	}
	if strings.Contains(a.systemPrompt, "SECURITY MODE") {
		t.Fatalf("system prompt still in security mode after switching back to devops:\n%s", a.systemPrompt)
	}
	if a.conversation[0].Content != a.systemPrompt {
		t.Fatalf("conversation's system message was not updated after switching back to devops")
	}
	beforeBogus := a.systemPrompt

	badErr := a.SetMode("bogus")
	if badErr == nil || !strings.Contains(badErr.Error(), "invalid mode") {
		t.Fatalf("SetMode(bogus) = %v, want an invalid mode error", badErr)
	}
	if a.GetMode() != storage.ModeDevOps {
		t.Fatalf("GetMode() = %q after a rejected switch, want it to stay devops", a.GetMode())
	}
	if a.systemPrompt != beforeBogus {
		t.Fatal("a rejected SetMode call must not change the current system prompt")
	}
}

// TestGetMemoryManagerNilWithoutTaracodeDir and TestGetMemoryManagerBuildsOneWhenTaracodeDirExists
// cover both branches of the unexported helper the prompt builder uses to fold in project
// memories: no .taracode directory means no manager, a working one means a real *memory.Manager.
func TestGetMemoryManagerNilWithoutTaracodeDir(t *testing.T) {
	dir := t.TempDir()

	if mm := getMemoryManager(dir); mm != nil {
		t.Fatalf("getMemoryManager(%q) = %v, want nil without a .taracode directory", dir, mm)
	}
}

func TestGetMemoryManagerBuildsOneWhenTaracodeDirExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".taracode"), 0o755); err != nil {
		t.Fatal(err)
	}

	mm := getMemoryManager(dir)

	if mm == nil {
		t.Fatal("getMemoryManager = nil, want a manager once .taracode exists")
	}
}

// TestBuildSystemPromptIncludesTheActivePlanWithTaskStatusMarkers covers the active-plan section:
// with a real storage manager holding a plan with a completed, an in-progress and a pending task,
// the prompt lists all three tasks with their status markers.
func TestBuildSystemPromptIncludesTheActivePlanWithTaskStatusMarkers(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.CreatePlan("Ship the feature", []string{"write tests", "implement", "review"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateTaskStatus(plan.ID, plan.Tasks[0].ID, storage.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateTaskStatus(plan.ID, plan.Tasks[1].ID, storage.TaskStatusInProgress); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPromptWithModeAndTools(dir, store, storage.ModeDevOps, true)

	if !strings.Contains(prompt, "ACTIVE PLAN") || !strings.Contains(prompt, "Ship the feature") {
		t.Fatalf("prompt missing the active plan section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[x] write tests") {
		t.Fatalf("prompt missing the completed task marker:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[>] implement") {
		t.Fatalf("prompt missing the in-progress task marker:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[ ] review") {
		t.Fatalf("prompt missing the pending task marker:\n%s", prompt)
	}
}
