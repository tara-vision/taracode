package assistant

import (
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestGenerateSummaryGoesThroughTheClientAndIsStored(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	store, err := storage.NewManager(a.workingDir)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession("")
	if err != nil {
		t.Fatal(err)
	}
	a.storage, a.session = store, session
	a.session.Messages = []storage.ConversationMessage{
		{Role: "user", Content: "deploy the app"},
		{Role: "assistant", Content: "deployed"},
	}
	srv.Turns = []ollamatest.Turn{{Content: "The app was deployed."}}

	summary, err := a.GenerateSummary()
	if err != nil {
		t.Fatal(err)
	}

	if summary != "The app was deployed." {
		t.Fatalf("summary = %q", summary)
	}
	if a.session.Summary != summary {
		t.Fatalf("summary not stored on the session: %q", a.session.Summary)
	}
	if a.sessionUsage.TotalTokens == 0 {
		t.Fatal("usage was not estimated for a reply without token counts")
	}
	options, _ := lastChatBody(t, srv)["options"].(map[string]any)
	if options["num_predict"] != float64(100) {
		t.Fatalf("summary request options = %v", options)
	}
}

func TestGenerateSummarySkipsShortSessions(t *testing.T) {
	a, _ := newTestAssistant(t, false)

	summary, err := a.GenerateSummary()

	if err != nil || summary != "" {
		t.Fatalf("summary = %q, err = %v", summary, err)
	}
}
