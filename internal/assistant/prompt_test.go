package assistant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
)

// TestBuildSystemPromptAppendsTaracodeMD covers the project-context injection: a TARACODE.md in
// the working directory is folded into the prompt verbatim.
func TestBuildSystemPromptAppendsTaracodeMD(t *testing.T) {
	dir := t.TempDir()
	content := "Project rule: always run `make test` before committing."
	if err := os.WriteFile(filepath.Join(dir, "TARACODE.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPrompt(dir, nil, policy.ModeInvestigate)

	if !strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Fatalf("prompt missing the PROJECT CONTEXT header:\n%s", prompt)
	}
	if !strings.Contains(prompt, content) {
		t.Fatalf("prompt missing the TARACODE.md content:\n%s", prompt)
	}
}

// TestBuildSystemPromptAppendsWorkingDirectory covers the trailing "Current working directory"
// line every prompt ends with, whatever mode produced it.
func TestBuildSystemPromptAppendsWorkingDirectory(t *testing.T) {
	dir := t.TempDir()

	prompt := buildSystemPrompt(dir, nil, policy.ModeInvestigate)

	want := fmt.Sprintf("Current working directory: %s", dir)
	if !strings.HasSuffix(prompt, want) {
		t.Fatalf("prompt does not end with the working directory line:\n%s", prompt)
	}
}

// TestSetModeSwitchesToolsAndPrompt covers SetMode: investigate exposes the fourteen read-form
// tools, operate all sixteen, the system prompt (and the conversation's system message) carries the
// mode line, and operate without project storage is refused.
func TestSetModeSwitchesToolsAndPrompt(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	if a.Mode() != policy.ModeInvestigate || len(a.toolDefs) != 14 {
		t.Fatalf("start: mode %q, %d tools", a.Mode(), len(a.toolDefs))
	}
	if err := a.SetMode(policy.ModeOperate); err == nil || !strings.Contains(err.Error(), "/init") {
		t.Fatalf("operate without storage: %v", err)
	}
	if a.Mode() != policy.ModeInvestigate {
		t.Fatal("a refused switch must keep the mode")
	}
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	a.storage = store

	if err := a.SetMode(policy.ModeOperate); err != nil {
		t.Fatalf("SetMode(operate) = %v", err)
	}
	if a.Mode() != policy.ModeOperate || len(a.toolDefs) != 16 || a.ToolRegistry().Available(a.Mode()) != 16 {
		t.Fatalf("operate: mode %q, %d tools", a.Mode(), len(a.toolDefs))
	}
	if !strings.Contains(a.systemPrompt, "Mode: operate") || a.conversation[0].Content != a.systemPrompt {
		t.Fatalf("the prompt must carry the mode:\n%s", a.conversation[0].Content)
	}
	if err := a.SetMode(policy.ModeInvestigate); err != nil || len(a.toolDefs) != 14 ||
		!strings.Contains(a.systemPrompt, "Mode: investigate") {
		t.Fatalf("back to investigate: %v, %d tools", err, len(a.toolDefs))
	}
}

// TestSetModeRejectsUnknownModesAndALockedPolicy covers the two other refusals: a mode name that
// is neither investigate nor operate changes nothing, and a policy file that failed to load locks
// operate mode.
func TestSetModeRejectsUnknownModesAndALockedPolicy(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	enterOperate(t, a)

	if err := a.SetMode("yolo"); err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Fatalf("SetMode(yolo) = %v", err)
	}
	if a.Mode() != policy.ModeOperate || len(a.toolDefs) != 16 {
		t.Fatal("an unknown mode must change nothing")
	}

	if err := a.SetMode(policy.ModeInvestigate); err != nil {
		t.Fatal(err)
	}
	a.policyErr = errors.New("policy.yaml: bad key")
	if err := a.SetMode(policy.ModeOperate); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("a policy error must lock operate mode: %v", err)
	}
	if a.Mode() != policy.ModeInvestigate {
		t.Fatal("a locked switch must keep investigate mode")
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

	prompt := buildSystemPrompt(dir, store, policy.ModeInvestigate)

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
