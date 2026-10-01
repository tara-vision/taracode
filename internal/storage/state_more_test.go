package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	projectcontext "github.com/tara-vision/taracode/internal/context"
)

func writeState(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, ".taracode", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewManagerLoadsTheSavedState(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "state/current.json", `{"active_plan_id":"p1","working_context":"deploys"}`)
	writeState(t, root, "state/preferences.json", `{"preferred_model":"qwen3.5:9b","max_history_length":7}`)
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if s := m.GetCurrentState(); s.ActivePlanID != "p1" || s.WorkingContext != "deploys" {
		t.Fatalf("state %+v", s)
	}
	if p := m.GetPreferences(); p.PreferredModel != "qwen3.5:9b" || p.MaxHistoryLength != 7 || !p.AutoLoadContext {
		t.Fatalf("a field the file leaves out keeps its default: %+v", p)
	}
}

func TestNewManagerSurvivesBrokenStateFiles(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"history/sessions.json", "state/current.json", "state/preferences.json"} {
		writeState(t, root, rel, "{broken")
	}
	m, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := m.ListSessions(); len(list) != 0 || m.GetActiveSessionID() != "" {
		t.Fatalf("index %v", list)
	}
	if s := m.GetCurrentState(); s.ActivePlanID != "" || s.LastActivity.IsZero() {
		t.Fatalf("state %+v", s)
	}
	if p := m.GetPreferences(); !reflect.DeepEqual(p, DefaultPreferences()) {
		t.Fatalf("preferences %+v", p)
	}
}

func TestStateAndPreferencesAreCopiesAndPersist(t *testing.T) {
	m, root := newManager(t)
	state := m.GetCurrentState()
	state.ActivePlanID = "changed"
	prefs := m.GetPreferences()
	prefs.PreferredModel = "gemma4:12b"
	if m.GetCurrentState().ActivePlanID != "" || m.GetPreferredModel() != "" {
		t.Fatal("the getters hand out copies")
	}
	if err := m.UpdateCurrentState(&CurrentState{ActivePlanID: "p2", LastActivity: time.Now()}); err != nil {
		t.Fatal(err)
	}
	prefs.ExcludeDirs = []string{"vendor"}
	if err := m.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.GetCurrentState().ActivePlanID != "p2" || reopened.GetPreferredModel() != "gemma4:12b" ||
		len(reopened.GetPreferences().ExcludeDirs) != 1 {
		t.Fatalf("state %+v, preferences %+v", reopened.GetCurrentState(), reopened.GetPreferences())
	}
	err = m.UpdateCurrentState(&CurrentState{LastActivity: farFuture})
	if err == nil || !strings.HasPrefix(err.Error(), "failed to marshal current state: ") {
		t.Fatalf("err = %v", err)
	}
}

// TestPreferredModelWithoutLoadedPreferences: a Manager whose preferences were never loaded has no
// preferred model, and saving one fills in the defaults around it.
func TestPreferredModelWithoutLoadedPreferences(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{rootDir: dir}
	if m.GetPreferredModel() != "" {
		t.Fatal("no preferences, no model")
	}
	if err := m.SetPreferredModel("gemma4:12b"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state", "preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved Preferences
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.PreferredModel != "gemma4:12b" || saved.MaxHistoryLength != 100 || !saved.AutoLoadContext {
		t.Fatalf("saved %+v", saved)
	}
}

func TestProjectContextRoundTrip(t *testing.T) {
	m, root := newManager(t)
	if pc, err := m.LoadProjectContext(); pc != nil || err != nil {
		t.Fatalf("nothing saved yet: %v %v", pc, err)
	}
	in := &projectcontext.ProjectContext{RootPath: "/w", ProjectType: "go", ModuleName: "example.com/shop",
		Dependencies: []string{"github.com/spf13/cobra"}}
	if err := m.SaveProjectContext(in); err != nil {
		t.Fatal(err)
	}
	out, err := m.LoadProjectContext()
	if err != nil || out.ModuleName != "example.com/shop" || out.Dependencies[0] != "github.com/spf13/cobra" {
		t.Fatalf("%+v %v", out, err)
	}
	writeState(t, root, "context/project.json", "{broken")
	if _, err := m.LoadProjectContext(); err == nil || !strings.HasPrefix(err.Error(), "failed to parse project context: ") {
		t.Fatalf("broken file: %v", err)
	}
	err = m.SaveProjectContext(&projectcontext.ProjectContext{CreatedAt: farFuture})
	if err == nil || !strings.HasPrefix(err.Error(), "failed to marshal project context: ") {
		t.Fatalf("unencodable context: %v", err)
	}
}

func TestProjectConfigFailures(t *testing.T) {
	m, root := newManager(t)
	if _, err := m.LoadProjectConfig(); !os.IsNotExist(err) {
		t.Fatalf("no config yet: %v", err)
	}
	writeState(t, root, "project.json", "{broken")
	if _, err := m.LoadProjectConfig(); err == nil || !strings.HasPrefix(err.Error(), "failed to parse project config: ") {
		t.Fatalf("broken file: %v", err)
	}
	err := m.SaveProjectConfig(&ProjectConfig{InitializedAt: farFuture})
	if err == nil || !strings.HasPrefix(err.Error(), "failed to marshal project config: ") {
		t.Fatalf("unencodable config: %v", err)
	}
}

// TestSaveFileSummaryNamesTheFileAfterThePath: every separator and character a file system may
// refuse becomes an underscore.
func TestSaveFileSummaryNamesTheFileAfterThePath(t *testing.T) {
	m, root := newManager(t)
	analysis := &projectcontext.FileAnalysis{Path: "cmd/root.go", Language: "go", LineCount: 42}
	if err := m.SaveFileSummary(`cmd/a\b:c*d?e"f<g>h|i.go`, analysis); err != nil {
		t.Fatal(err)
	}
	summaries := filepath.Join(root, ".taracode", "context", "summaries")
	data, err := os.ReadFile(filepath.Join(summaries, "cmd_a_b_c_d_e_f_g_h_i.go.json"))
	if err != nil || !strings.Contains(string(data), `"line_count": 42`) {
		t.Fatalf("%s %v", data, err)
	}
	if got := sanitizeFilename("a//b::c"); got != "a__b__c" {
		t.Fatalf("each character is replaced: %q", got)
	}
	replaceWithFile(t, summaries)
	if err := m.SaveFileSummary("x.go", analysis); err == nil {
		t.Fatal("a summaries directory that is a file")
	}
}

func TestDefaultMemoryConfig(t *testing.T) {
	c := DefaultMemoryConfig()
	if !c.Enabled || c.MaxMemories != 500 || c.MaxContextTokens != 2000 || c.RetentionDays != 90 || !c.AutoCapture ||
		c.Categories != nil {
		t.Fatalf("%+v", c)
	}
}
