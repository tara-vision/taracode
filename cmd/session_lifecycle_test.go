package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestClearStartsANewSession(t *testing.T) {
	r, _ := projectREPL(t)
	asst, first := r.asst, r.asst.GetSession().ID
	out := captureStdoutForTest(t, func() { r.dispatch("/clear") })
	if !strings.Contains(out, "Conversation cleared. Started new session.") || r.asst != asst || r.asst.GetSession().ID == first {
		t.Fatalf("output %q; the same assistant moves to a new session", out)
	}
}

// TestClearWithoutStorageRebuildsTheAssistant: an ephemeral session has no session to start, so
// /clear builds a fresh assistant on the same connection instead.
func TestClearWithoutStorageRebuildsTheAssistant(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	asst := r.asst
	_ = captureStdoutForTest(t, func() {
		if err := asst.ProcessMessage("unused"); err == nil {
			t.Error("the fake has no turn scripted; the message must fail")
		}
	})
	if asst.GetConversationLength() != 2 {
		t.Fatalf("the failed message stays in the conversation: %d", asst.GetConversationLength())
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/clear") })
	if !strings.Contains(out, "Conversation cleared. Started new session.") || r.asst == asst ||
		r.asst.GetConversationLength() != 1 {
		t.Fatalf("output %q, conversation %d", out, r.asst.GetConversationLength())
	}
}

func TestClearReportsAFailedRebuild(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	asst := r.asst
	r.opts.Host, r.opts.Model = "http://127.0.0.1:1", "" // nothing to connect to and no model to fall back on
	stdout, stderr := captureOutput(t, func() { r.dispatch("/clear") })
	if !strings.Contains(stderr, "Error clearing:") || strings.Contains(stdout, "Conversation cleared") || r.asst != asst {
		t.Fatalf("stdout %q, stderr %q", stdout, stderr)
	}
}

// loadedSession builds a project repl whose startup session already holds three messages.
func loadedSession(t *testing.T, turns ...ollamatest.Turn) (*repl, *storage.Manager) {
	t.Helper()
	dir := t.TempDir()
	storedSession(t, dir, []storage.ConversationMessage{
		{Role: "user", Content: "the pods crash"}, {Role: "assistant", Content: "the image tag is wrong"},
		{Role: "user", Content: "fixed it"},
	})
	r := replOn(t, fakeOllama(t, turns...), dir, false)
	return r, r.asst.GetStorage()
}

func TestExitSummarisesTheSession(t *testing.T) {
	r, st := loadedSession(t, ollamatest.Turn{Content: "They fixed the crashing pods."})
	out := captureStdoutForTest(t, func() { handleExitWithSummary(r.asst) })
	if out != "Generating summary... done.\nGoodbye!\n" {
		t.Fatalf("%q", out)
	}
	saved, err := st.GetSession(r.asst.GetSession().ID)
	if err != nil || saved.Summary != "They fixed the crashing pods." {
		t.Fatalf("saved %+v err=%v", saved, err)
	}
	if out := captureStdoutForTest(t, func() { handleExitWithSummary(r.asst) }); out != "Goodbye!\n" {
		t.Fatalf("a summarised session is not summarised again: %q", out)
	}
}

func TestExitSkipsAFailedSummary(t *testing.T) {
	r, _ := loadedSession(t, ollamatest.Turn{Status: 500, Error: "model crashed"})
	if out := captureStdoutForTest(t, func() { handleExitWithSummary(r.asst) }); out != "Generating summary... skipped.\nGoodbye!\n" {
		t.Fatalf("%q", out)
	}
}

func TestExitWithoutASessionJustSaysGoodbye(t *testing.T) {
	r := replOn(t, fakeOllama(t), t.TempDir(), true)
	if out := captureStdoutForTest(t, func() { handleExitWithSummary(r.asst) }); out != "Goodbye!\n" {
		t.Fatalf("%q", out)
	}
}

func TestStatusShowsTheConnectionProjectAndSession(t *testing.T) {
	srv := fakeOllama(t)
	ephemeral := replOn(t, srv, t.TempDir(), true)
	out := captureStdoutForTest(t, func() { ephemeral.dispatch("/status") })
	for _, want := range []string{
		"Status:", "Provider: Ollama (ollama)", "Host: " + srv.URL, "Model: gemma4:12b",
		"Project: Not initialized (run /init)", "Session: None", "Storage: Not available",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/status lacks %q:\n%s", want, out)
		}
	}

	r, _ := projectREPL(t)
	writeFile(t, filepath.Join(r.projectRoot, "TARACODE.md"), "# project\n")
	if err := os.WriteFile(filepath.Join(r.projectRoot, ".taracode", "context", "project.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/status") })
	for _, want := range []string{
		"Project: Initialized", "Context: Cached in .taracode/context/",
		"Session: " + r.asst.GetSession().ID[:8] + " (0 messages)", "Storage: " + filepath.Join(r.projectRoot, ".taracode"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/status lacks %q:\n%s", want, out)
		}
	}
}
