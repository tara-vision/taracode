package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	projectcontext "github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestAccessorsReportTheAssistantsState(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	if a.GetProvider() == nil || a.GetSessionUsage() != a.sessionUsage || a.GetConversationLength() != 1 ||
		a.GetProjectContext() != nil || a.GetStorage() != nil || a.GetSession() != nil {
		t.Fatalf("a fresh test assistant: %+v", a)
	}
	a.projectCtx = &projectcontext.ProjectContext{ProjectType: "Go"}
	if a.GetProjectContext().ProjectType != "Go" {
		t.Fatal("the project context")
	}
	if err := a.ClearAudit(); err == nil || !strings.Contains(err.Error(), "no project storage") {
		t.Fatalf("ClearAudit without storage: %v", err)
	}
	if _, err := a.ListSessions(); err == nil {
		t.Fatal("ListSessions without storage")
	}
	if a.GetSessionFresh() != nil {
		t.Fatal("no storage, no session")
	}

	before := len(a.toolDefs)
	a.toolRegistry.UnregisterMCP("none")
	a.toolDefs = nil
	a.RefreshTools()
	if len(a.toolDefs) != before {
		t.Fatalf("RefreshTools re-reads the schemas: %d, want %d", len(a.toolDefs), before)
	}
}

func TestClearAuditRemovesTheLog(t *testing.T) {
	a, _, _ := gateAssistant(t, "investigate")
	if err := a.storage.AppendAudit(storage.AuditRecord{Tool: "write_file", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ClearAudit(); err != nil {
		t.Fatal(err)
	}
	if recs, err := a.storage.ReadAudit(""); err != nil || len(recs) != 0 {
		t.Fatalf("records %+v err=%v", recs, err)
	}
}

func TestSessionAccessorsReadTheStore(t *testing.T) {
	a, _, _ := gateAssistant(t, "investigate")
	id := a.GetSession().ID
	if err := a.storage.AddMessage(id, storage.ConversationMessage{Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if len(a.GetSession().Messages) != 0 {
		t.Fatal("GetSession is the session as it was loaded")
	}
	if fresh := a.GetSessionFresh(); fresh.ID != id || len(fresh.Messages) != 1 {
		t.Fatalf("GetSessionFresh rereads it: %+v", fresh)
	}
	a.session = &storage.Session{ID: "deleted-meanwhile"}
	if fresh := a.GetSessionFresh(); fresh.ID != "deleted-meanwhile" {
		t.Fatalf("a session the store lost stays as it is: %+v", fresh)
	}
	sessions, err := a.ListSessions()
	if err != nil || len(sessions) != 1 || sessions[0].ID != id {
		t.Fatalf("sessions %+v err=%v", sessions, err)
	}
}

func TestNewAndLoadSessionNeedStorage(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	if err := a.NewSession("x"); err == nil || err.Error() != "storage not initialized" {
		t.Fatalf("NewSession: %v", err)
	}
	if err := a.LoadSession("x"); err == nil || err.Error() != "storage not initialized" {
		t.Fatalf("LoadSession: %v", err)
	}
	g, _, dir := gateAssistant(t, "investigate")
	if err := g.LoadSession("nope"); err == nil {
		t.Fatal("an unknown session")
	}
	blockHistory(t, dir)
	if err := g.NewSession("x"); err == nil {
		t.Fatal("a session that cannot be written")
	}
}

// blockHistory makes the project's session directory unwritable.
func blockHistory(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	history := filepath.Join(dir, ".taracode", "history")
	if err := os.Chmod(history, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(history, 0o755) })
}

func TestLoadSessionSkipsStoredSystemMessages(t *testing.T) {
	a, _, _ := gateAssistant(t, "investigate")
	other, err := a.storage.CreateSession("old")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []storage.ConversationMessage{{Role: "system", Content: "old prompt"}, {Role: "user", Content: "hi"}} {
		if err := a.storage.AddMessage(other.ID, msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.LoadSession(other.ID); err != nil {
		t.Fatal(err)
	}
	if a.GetConversationLength() != 2 || a.conversation[0].Content == "old prompt" || a.conversation[1].Role != openai.ChatMessageRoleUser {
		t.Fatalf("conversation %+v", a.conversation)
	}
}

func TestRecordMessageReportsAStoreThatCannotBeWritten(t *testing.T) {
	a, _, dir := gateAssistant(t, "investigate")
	var out bytes.Buffer
	a.out = &out
	blockHistory(t, dir)
	sessionFile := filepath.Join(dir, ".taracode", "history", "session_"+a.GetSession().ID+".json")
	if err := os.Chmod(sessionFile, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionFile, 0o644) })
	a.recordMessage(storage.ConversationMessage{Role: "user", Content: "hi"})
	if !strings.Contains(out.String(), "Could not save the user message to the session") {
		t.Fatalf("%q", out.String())
	}
}

// fakeProvider is a provider without model management (an OpenAI-compatible server).
type fakeProvider struct{ info *provider.Info }

func (p *fakeProvider) Info() *provider.Info                           { return p.info }
func (p *fakeProvider) DetectModels(context.Context) ([]string, error) { return nil, nil }
func (p *fakeProvider) CreateClient() *openai.Client                   { return nil }
func (p *fakeProvider) LLM() llm.Client                                { return nil }
func (p *fakeProvider) SetModel(m string)                              { p.info.Model = m }

func TestModelManagementNeedsAnOllamaProvider(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.provider = nil
	if a.GetProviderInfo() != nil {
		t.Fatal("no provider, no info")
	}
	if _, err := a.ListModels(); err == nil || err.Error() != "provider not initialized" {
		t.Fatalf("ListModels: %v", err)
	}
	if err := a.SwitchModel("x"); err == nil || err.Error() != "provider not initialized" {
		t.Fatalf("SwitchModel: %v", err)
	}
	a.provider = &fakeProvider{info: &provider.Info{Name: "vLLM"}}
	if _, err := a.ListModels(); err == nil || !strings.Contains(err.Error(), "does not support model listing") {
		t.Fatalf("ListModels: %v", err)
	}
	if err := a.SwitchModel("x"); err == nil || !strings.Contains(err.Error(), "does not support model switching") {
		t.Fatalf("SwitchModel: %v", err)
	}
}

func TestListModelsAndSwitchModelOnOllama(t *testing.T) {
	a, srv, _ := gateAssistant(t, "investigate")
	srv.Models = append(srv.Models, ollamatest.ModelSpec{Name: "qwen3.5:9b", Capabilities: []string{"completion", "tools"},
		ContextLength: 32768, ParameterSize: "9B"})
	list, err := a.ListModels()
	if err != nil || len(list) != 2 || list[1].Name != "qwen3.5:9b" || list[1].Params != "9B" {
		t.Fatalf("models %+v err=%v", list, err)
	}
	if err := a.SwitchModel("qwen3.5:9b"); err != nil {
		t.Fatal(err)
	}
	if a.GetCurrentModel() != "qwen3.5:9b" || a.storage.GetPreferredModel() != "qwen3.5:9b" || len(srv.Unloaded) != 1 {
		t.Fatalf("model %s, saved %s, unloaded %v", a.GetCurrentModel(), a.storage.GetPreferredModel(), srv.Unloaded)
	}
}

func TestSwitchModelReportsWhatItCouldNotDo(t *testing.T) {
	a, srv, dir := gateAssistant(t, "investigate")
	srv.Models = append(srv.Models, ollamatest.ModelSpec{Name: "qwen3.5:9b", Capabilities: []string{"completion", "tools"},
		ContextLength: 32768})
	var out bytes.Buffer
	a.out = &out
	a.provider = &unloadFails{Provider: a.provider}
	if os.Geteuid() != 0 {
		state := filepath.Join(dir, ".taracode", "state")
		if err := os.Chmod(state, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(state, 0o755) })
	}
	if err := a.SwitchModel("qwen3.5:9b"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Note: Could not unload gemma4:12b (may not be loaded)") {
		t.Fatalf("%q", out.String())
	}
	if os.Geteuid() != 0 && !strings.Contains(out.String(), "Note: Could not save model preference") {
		t.Fatalf("%q", out.String())
	}
}

// unloadFails is a model-managing provider whose unload always fails.
type unloadFails struct{ provider.Provider }

func (u *unloadFails) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return u.Provider.(provider.ModelManager).ListModels(ctx)
}
func (u *unloadFails) UnloadModel(context.Context, string) error { return errors.New("not loaded") }
