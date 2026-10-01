package agent

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
)

func summarySession(messages ...storage.ConversationMessage) *storage.Session {
	return &storage.Session{ID: "s1", Messages: messages}
}

func TestGenerateSummaryKeepsAnExistingSummary(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.session = summarySession(storage.ConversationMessage{Role: "user", Content: "deploy"},
		storage.ConversationMessage{Role: "assistant", Content: "deployed"})
	a.session.Summary = "The app was deployed."
	summary, err := a.GenerateSummary()
	if err != nil || summary != "The app was deployed." {
		t.Fatalf("summary %q, err %v", summary, err)
	}
	if len(srv.Requests) != 0 {
		t.Fatalf("an existing summary is not asked for again: %d requests", len(srv.Requests))
	}
}

// TestGenerateSummarySendsTheConversationShortened: only user and assistant messages are sent,
// each cut to 500 bytes, and the answer comes back trimmed.
func TestGenerateSummarySendsTheConversationShortened(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	long := strings.Repeat("k", 600)
	a.session = summarySession(storage.ConversationMessage{Role: "user", Content: long},
		storage.ConversationMessage{Role: "tool", Content: "pod list"},
		storage.ConversationMessage{Role: "assistant", Content: "three pods"})
	srv.Turns = []ollamatest.Turn{{Content: "  Counted the pods.  "}}
	summary, err := a.GenerateSummary()
	if err != nil || summary != "Counted the pods." {
		t.Fatalf("summary %q, err %v", summary, err)
	}
	want := "user: " + long[:500] + "...\nassistant: three pods\n"
	if got := messageContent(t, lastChatBody(t, srv), 0); got != want {
		t.Fatalf("the summary request carried %q", got)
	}
}

func TestGenerateSummaryReportsAFailedRequest(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.session = summarySession(storage.ConversationMessage{Role: "user", Content: "deploy"},
		storage.ConversationMessage{Role: "assistant", Content: "deployed"})
	srv.Turns = []ollamatest.Turn{{Status: 500, Error: "model crashed"}}
	if _, err := a.GenerateSummary(); err == nil || !strings.HasPrefix(err.Error(), "failed to generate summary: ") {
		t.Fatalf("err = %v", err)
	}
}
