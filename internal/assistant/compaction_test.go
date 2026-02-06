package assistant

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"empty", "", 0},
		{"short", "hello", 2},            // 5 chars -> (5+3)/4 = 2
		{"medium", "hello world foo", 4}, // 15 chars -> (15+3)/4 = 4
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EstimateTokens(tt.text)
			if result != tt.expected {
				t.Errorf("EstimateTokens(%q) = %d, expected %d", tt.text, result, tt.expected)
			}
		})
	}
}

func TestEstimateConversationTokens(t *testing.T) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "You are helpful."},
		{Role: openai.ChatMessageRoleUser, Content: "Hello"},
		{Role: openai.ChatMessageRoleAssistant, Content: "Hi there!"},
	}

	tokens := EstimateConversationTokens(messages)
	if tokens <= 0 {
		t.Error("expected positive token count")
	}

	// 3 messages * 4 overhead = 12 overhead tokens
	// Plus content tokens
	expectedMin := 12 // at least the overhead
	if tokens < expectedMin {
		t.Errorf("expected at least %d tokens (overhead), got %d", expectedMin, tokens)
	}
}

func TestEstimateConversationTokens_WithToolCalls(t *testing.T) {
	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleAssistant,
			Content: "",
			ToolCalls: []openai.ToolCall{
				{
					Function: openai.FunctionCall{
						Name:      "read_file",
						Arguments: `{"path": "/tmp/test.txt"}`,
					},
				},
			},
		},
	}

	tokens := EstimateConversationTokens(messages)
	if tokens <= 4 { // should be more than just the overhead
		t.Errorf("expected tokens > 4 with tool calls, got %d", tokens)
	}
}

func TestEstimateToolDefsTokens(t *testing.T) {
	toolDefs := []openai.Tool{
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "read_file",
				Description: "Read a file from the filesystem",
			},
		},
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "write_file",
				Description: "Write content to a file",
			},
		},
	}

	tokens := EstimateToolDefsTokens(toolDefs)
	if tokens <= 0 {
		t.Error("expected positive token count for tool definitions")
	}
}

func TestShouldCompact_Disabled(t *testing.T) {
	cfg := CompactionConfig{Enabled: false, MaxTokens: 32768, Threshold: 0.75, KeepRecent: 4}
	messages := make([]openai.ChatCompletionMessage, 20)
	for i := range messages {
		messages[i] = openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "test"}
	}

	if ShouldCompact(messages, nil, cfg) {
		t.Error("should not compact when disabled")
	}
}

func TestShouldCompact_TooFewMessages(t *testing.T) {
	cfg := CompactionConfig{Enabled: true, MaxTokens: 32768, Threshold: 0.75, KeepRecent: 4}
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "system"},
		{Role: openai.ChatMessageRoleUser, Content: "hello"},
		{Role: openai.ChatMessageRoleAssistant, Content: "hi"},
	}

	if ShouldCompact(messages, nil, cfg) {
		t.Error("should not compact with too few messages")
	}
}

func TestShouldCompact_UnderThreshold(t *testing.T) {
	cfg := CompactionConfig{Enabled: true, MaxTokens: 100000, Threshold: 0.75, KeepRecent: 2}
	messages := make([]openai.ChatCompletionMessage, 12)
	messages[0] = openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: "system"}
	for i := 1; i < len(messages); i++ {
		if i%2 == 1 {
			messages[i] = openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "short"}
		} else {
			messages[i] = openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "short"}
		}
	}

	if ShouldCompact(messages, nil, cfg) {
		t.Error("should not compact when under threshold")
	}
}

func TestShouldCompact_OverThreshold(t *testing.T) {
	cfg := CompactionConfig{Enabled: true, MaxTokens: 100, Threshold: 0.5, KeepRecent: 2}
	messages := make([]openai.ChatCompletionMessage, 12)
	messages[0] = openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: "This is a very long system prompt that uses lots of tokens " + string(make([]byte, 200)),
	}
	for i := 1; i < len(messages); i++ {
		messages[i] = openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: "This is another message with content " + string(make([]byte, 100)),
		}
	}

	if !ShouldCompact(messages, nil, cfg) {
		t.Error("should compact when over threshold")
	}
}

func TestBuildFallbackSummary(t *testing.T) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: "How do I deploy?"},
		{Role: openai.ChatMessageRoleAssistant, Content: "You can deploy using kubectl..."},
		{Role: openai.ChatMessageRoleTool, Content: `{"result": "ok"}`},
		{Role: openai.ChatMessageRoleUser, Content: "What about scaling?"},
	}

	summary := buildFallbackSummary(messages)
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if !strings.Contains(summary, "4 messages") {
		t.Errorf("expected message count in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "2 user queries") {
		t.Errorf("expected user query count in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "1 tool call") {
		t.Errorf("expected tool call count in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "How do I deploy?") {
		t.Errorf("expected topic hint in summary, got: %s", summary)
	}
}

func TestBuildFallbackSummary_TopicLimit(t *testing.T) {
	messages := make([]openai.ChatCompletionMessage, 10)
	for i := range messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: "Topic " + string(rune('A'+i)),
		}
	}

	summary := buildFallbackSummary(messages)
	// Should keep at most 3 topics
	if strings.Contains(summary, "Topic D") {
		t.Error("should limit topics to 3")
	}
}

func TestNewCompactionState(t *testing.T) {
	state := NewCompactionState()
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if len(state.Events) != 0 {
		t.Error("expected empty events")
	}
	if state.TotalCompacted != 0 {
		t.Error("expected zero total compacted")
	}
}
