package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
)

// TestBuildSystemPromptAppendsTaracodeMD covers the project-context injection: a TARACODE.md in
// the working directory is folded into the prompt verbatim.
func TestBuildSystemPromptAppendsTaracodeMD(t *testing.T) {
	dir := t.TempDir()
	content := "Project rule: always run `make test` before committing."
	if err := os.WriteFile(filepath.Join(dir, "TARACODE.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPrompt(dir, nil, policy.ModeInvestigate, 0)

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

	prompt := buildSystemPrompt(dir, nil, policy.ModeInvestigate, 0)

	want := fmt.Sprintf("Current working directory: %s", dir)
	if !strings.HasSuffix(prompt, want) {
		t.Fatalf("prompt does not end with the working directory line:\n%s", prompt)
	}
}

// TestBuildSystemPromptCarriesTodaysDate covers the date line every prompt carries since 3.1.0,
// when the get_datetime tool was retired: the model learns today's date from the prompt instead of
// spending a tool call on it, so certificate expiry, event age and release recency reasoning start
// from the right day.
func TestBuildSystemPromptCarriesTodaysDate(t *testing.T) {
	prompt := buildSystemPrompt(t.TempDir(), nil, policy.ModeInvestigate, 0)

	want := "Today is " + time.Now().Format("Monday, 2006-01-02")
	if !strings.Contains(prompt, want) {
		t.Fatalf("prompt missing %q:\n%s", want, prompt)
	}
}

// TestPromptCarriesThePersonaTheModeAndAgentsMD covers the persona, both mode lines, the
// TARACODE.md and AGENTS.md sections, and the truncation of an oversized context file.
func TestPromptCarriesThePersonaTheModeAndAgentsMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents\nUse make test.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "TARACODE.md"), []byte("# TARACODE\nProd is eu-west-1.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := buildSystemPrompt(dir, nil, policy.ModeInvestigate, 0)
	for _, want := range []string{"local-first DevOps operator", "Mode: investigate", "read-only", "## PROJECT CONTEXT", "Prod is eu-west-1", "## AGENTS.md", "Use make test", "Current working directory: " + dir} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "security mode") || strings.Contains(p, "TOOL FORMAT") {
		t.Error("the old prompts must be gone")
	}
	op := buildSystemPrompt(dir, nil, policy.ModeOperate, 0)
	if !strings.Contains(op, "Mode: operate") || !strings.Contains(op, "dry run") {
		t.Error("operate mode line")
	}
	big := strings.Repeat("x", maxContextFileBytes+100)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := buildSystemPrompt(dir, nil, policy.ModeInvestigate, 0); !strings.Contains(p, "truncated") || len(p) > maxContextFileBytes*2+4000 {
		t.Errorf("large files are capped: %d bytes", len(p))
	}
}

// TestPromptAndSchemasFitTheContextBudget covers spec 4's context budget: the system prompt plus
// the exposed tool schemas must stay under 2,500 estimated tokens in investigate mode and 3,500 in
// operate mode.
func TestPromptAndSchemasFitTheContextBudget(t *testing.T) {
	r := tools.NewBuiltinRegistry(tools.Options{}, tools.Config{})
	for _, c := range []struct {
		mode   policy.Mode
		budget int
	}{{policy.ModeInvestigate, 2500}, {policy.ModeOperate, 3500}} {
		prompt := buildSystemPrompt(t.TempDir(), nil, c.mode, 0)
		total := EstimateTokens(prompt) + EstimateToolDefsTokens(r.Definitions(c.mode))
		if total > c.budget {
			t.Errorf("%s mode: prompt plus schemas estimate %d tokens, budget %d (spec 4)", c.mode, total, c.budget)
		}
	}
}

// TestSetModeSwitchesToolsAndPrompt covers SetMode: investigate exposes the thirteen read-form
// tools, operate all fifteen, the system prompt (and the conversation's system message) carries the
// mode line, and operate without project storage is refused.
func TestSetModeSwitchesToolsAndPrompt(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	if a.Mode() != policy.ModeInvestigate || len(a.toolDefs) != 13 {
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
	if a.Mode() != policy.ModeOperate || len(a.toolDefs) != 15 || a.ToolRegistry().Available(a.Mode()) != 15 {
		t.Fatalf("operate: mode %q, %d tools", a.Mode(), len(a.toolDefs))
	}
	if !strings.Contains(a.systemPrompt, "Mode: operate") || a.conversation[0].Content != a.systemPrompt {
		t.Fatalf("the prompt must carry the mode:\n%s", a.conversation[0].Content)
	}
	if err := a.SetMode(policy.ModeInvestigate); err != nil || len(a.toolDefs) != 13 ||
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
	if a.Mode() != policy.ModeOperate || len(a.toolDefs) != 15 {
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

	prompt := buildSystemPrompt(dir, store, policy.ModeInvestigate, 0)

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
