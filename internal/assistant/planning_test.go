package assistant

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

func TestSendMessageForPlanningReturnsTheModelText(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []ollamatest.Turn{{Content: `{"steps": []}`, PromptTokens: 7, CompletionTokens: 3}}

	plan, err := a.SendMessageForPlanning("plan a deploy")
	if err != nil {
		t.Fatal(err)
	}

	if plan != `{"steps": []}` {
		t.Fatalf("plan = %q", plan)
	}
	if a.sessionUsage.TotalTokens != 10 {
		t.Fatalf("usage = %+v, want 10 total tokens", a.sessionUsage)
	}
	body := lastChatBody(t, srv)
	if _, ok := body["tools"]; ok {
		t.Fatalf("planning must not send tool schemas: %v", body["tools"])
	}
	if conversation := len(a.conversation); conversation != 1 {
		t.Fatalf("planning changed the main conversation: %d messages", conversation)
	}
}

func TestSendMessageForPlanningReportsServerErrors(t *testing.T) {
	a, srv := newTestAssistant(t, true)
	srv.Turns = []ollamatest.Turn{{Status: 500, Error: "planner offline"}}

	_, err := a.SendMessageForPlanning("plan a deploy")

	if err == nil || !strings.Contains(err.Error(), "planner offline") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyzeImagesSendsTheImageAndEstimatesUsage(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	srv.Turns = []ollamatest.Turn{{Content: "A cat on a keyboard."}}
	image := &ImageData{Path: "cat.png", MimeType: "image/png", Base64: "aGVsbG8="}

	answer, err := a.AnalyzeImages("what is this?", []*ImageData{image})
	if err != nil {
		t.Fatal(err)
	}

	if answer != "A cat on a keyboard." {
		t.Fatalf("answer = %q", answer)
	}
	if a.sessionUsage.TotalTokens == 0 {
		t.Fatal("usage was not estimated for a reply without token counts")
	}
	message := lastMessage(t, lastChatBody(t, srv), 0)
	images, ok := message["images"].([]any)
	if !ok || len(images) != 1 || images[0] != "aGVsbG8=" {
		t.Fatalf("image not sent as base64: %v", message)
	}
}

func TestAnalyzeImagesRejectsAnEmptyImageList(t *testing.T) {
	a, _ := newTestAssistant(t, false)

	if _, err := a.AnalyzeImages("what is this?", nil); err == nil {
		t.Fatal("expected an error for an empty image list")
	}
}
