package assistant

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// TestGetContextInfoSumsTokensAndDefaultsMaxTokens covers the token accounting GetContextInfo
// reports for a known system prompt and conversation, and the 32768 default that applies when
// compactionCfg.MaxTokens is unset.
func TestGetContextInfoSumsTokensAndDefaultsMaxTokens(t *testing.T) {
	a, _ := newTestAssistant(t, false)
	a.systemPrompt = "You are a test assistant."
	a.conversation = []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: a.systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: "hello there"},
		{Role: openai.ChatMessageRoleAssistant, Content: "hi, how can I help?"},
	}
	a.compactionCfg.MaxTokens = 0

	info := a.GetContextInfo()

	wantSystem := EstimateTokens(a.systemPrompt)
	wantConversation := EstimateTokens("hello there") + EstimateTokens("hi, how can I help?")
	wantToolDefs := EstimateToolDefsTokens(a.toolDefs)
	if info.SystemPromptTokens != wantSystem {
		t.Errorf("SystemPromptTokens = %d, want %d", info.SystemPromptTokens, wantSystem)
	}
	if info.ConversationTokens != wantConversation {
		t.Errorf("ConversationTokens = %d, want %d", info.ConversationTokens, wantConversation)
	}
	if info.ToolDefsTokens != wantToolDefs {
		t.Errorf("ToolDefsTokens = %d, want %d", info.ToolDefsTokens, wantToolDefs)
	}
	wantTotal := wantSystem + wantConversation + wantToolDefs
	if info.TotalTokens != wantTotal {
		t.Errorf("TotalTokens = %d, want %d", info.TotalTokens, wantTotal)
	}
	if info.MaxTokens != 32768 {
		t.Errorf("MaxTokens = %d, want the 32768 default", info.MaxTokens)
	}
	if info.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", info.MessageCount)
	}
}

// TestForceCompactTooFewMessagesErrors covers the guard: a conversation shorter than
// KeepRecent*2+3 messages cannot be compacted and ForceCompact must say so rather than run.
func TestForceCompactTooFewMessagesErrors(t *testing.T) {
	a, _ := newTestAssistant(t, false)

	err := a.ForceCompact()

	if err == nil || !strings.Contains(err.Error(), "too short to compact") {
		t.Fatalf("ForceCompact() = %v, want a too-short-to-compact error", err)
	}
}

// TestForceCompactShortensConversationAndRecordsEvent covers the success path: with enough
// messages and a scripted summary reply, ForceCompact replaces the older messages with a summary
// and records exactly one compaction event.
func TestForceCompactShortensConversationAndRecordsEvent(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.compactionCfg.KeepRecent = 1
	a.conversation = append(a.conversation,
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "first question"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "first answer"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "second question"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "second answer"},
	)
	srv.Turns = []ollamatest.Turn{{Content: "The user asked twice and got two answers."}}
	before := len(a.conversation)

	if err := a.ForceCompact(); err != nil {
		t.Fatalf("ForceCompact() = %v", err)
	}

	if len(a.conversation) >= before {
		t.Fatalf("conversation not shortened: before=%d after=%d", before, len(a.conversation))
	}
	if !strings.Contains(a.conversation[1].Content, "The user asked twice") {
		t.Fatalf("summary message missing from the compacted conversation: %+v", a.conversation[1])
	}
	if len(a.compactionState.Events) != 1 {
		t.Fatalf("compaction events = %d, want 1", len(a.compactionState.Events))
	}
	if a.compactionState.TotalCompacted <= 0 {
		t.Fatalf("TotalCompacted = %d, want > 0", a.compactionState.TotalCompacted)
	}
}
