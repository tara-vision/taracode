package agent

import (
	gocontext "context"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/ollama"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
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

// buildConversation returns a system prompt followed by count user/assistant pairs.
func buildConversation(count int) []openai.ChatCompletionMessage {
	conversation := []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: "You are taracode.",
	}}
	for i := 0; i < count; i++ {
		conversation = append(conversation,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "question"},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "answer"},
		)
	}
	return conversation
}

func TestCompactConversationSummarizesThroughTheClient(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Turns = []ollamatest.Turn{{Content: "The user asked twice and the assistant answered."}}
	client := ollama.New(srv.URL, nil)
	conversation := buildConversation(5)
	cfg := CompactionConfig{Enabled: true, Threshold: 0.1, KeepRecent: 2, MaxTokens: 1000}

	options := llm.Options{NumPredict: compactionSummaryTokens, Think: llm.ThinkOff}
	compacted, event, err := CompactConversation(
		gocontext.Background(), conversation, nil, cfg, client, "gemma4:12b", options)
	if err != nil {
		t.Fatal(err)
	}

	if event == nil {
		t.Fatal("no compaction event")
	}
	if event.MessagesBefore != len(conversation) || event.MessagesAfter != len(compacted) {
		t.Fatalf("event = %+v, compacted %d messages", event, len(compacted))
	}
	summary := compacted[1]
	if summary.Role != openai.ChatMessageRoleSystem ||
		!strings.Contains(summary.Content, "the assistant answered") {
		t.Fatalf("summary message = %+v", summary)
	}
	if body := srv.Requests[0].Body; body["stream"] != false {
		t.Fatalf("summary request should not stream: %v", body["stream"])
	}
}

// TestCompactConversationSummaryCarriesTheGivenOptions covers the fix for the summary request
// bypassing the session options: CompactConversation must send exactly the llm.Options it is
// given (num_ctx, keep_alive, think) instead of the old bare NumPredict-only request, or Ollama
// reloads the runner for a different context window and a thinking model spends the summary's
// small token budget on reasoning instead of the answer.
func TestCompactConversationSummaryCarriesTheGivenOptions(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Turns = []ollamatest.Turn{{Content: "summary text"}}
	client := ollama.New(srv.URL, nil)
	conversation := buildConversation(5)
	cfg := CompactionConfig{Enabled: true, Threshold: 0.1, KeepRecent: 2, MaxTokens: 1000}
	options := llm.Options{NumCtx: 32768, KeepAlive: "-1", Think: llm.ThinkOff, NumPredict: compactionSummaryTokens}

	_, event, err := CompactConversation(
		gocontext.Background(), conversation, nil, cfg, client, "gemma4:12b", options)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("no compaction event")
	}

	body := srv.Requests[0].Body
	opts, ok := body["options"].(map[string]any)
	if !ok || opts["num_ctx"] != float64(32768) || opts["num_predict"] != float64(compactionSummaryTokens) {
		t.Fatalf("summary request options: %v", body["options"])
	}
	if body["keep_alive"] != "-1" {
		t.Fatalf("summary request keep_alive = %v, want -1", body["keep_alive"])
	}
	if body["think"] != false {
		t.Fatalf("summary request think = %v, want false", body["think"])
	}
}

func TestCompactConversationFallsBackWhenTheServerFails(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Turns = []ollamatest.Turn{{Status: 500, Error: "summary model unloaded"}}
	client := ollama.New(srv.URL, nil)
	conversation := buildConversation(5)
	cfg := CompactionConfig{Enabled: true, Threshold: 0.1, KeepRecent: 2, MaxTokens: 1000}
	options := llm.Options{NumPredict: compactionSummaryTokens, Think: llm.ThinkOff}

	compacted, event, err := CompactConversation(
		gocontext.Background(), conversation, nil, cfg, client, "gemma4:12b", options)
	if err != nil {
		t.Fatalf("a failed summary must not fail compaction: %v", err)
	}

	if event == nil {
		t.Fatal("no compaction event")
	}
	if !strings.Contains(compacted[1].Content, "messages summarized") {
		t.Fatalf("fallback summary missing: %q", compacted[1].Content)
	}
}

func TestGenerateSummaryWithoutAClient(t *testing.T) {
	_, err := generateSummary(gocontext.Background(), buildConversation(1), nil, "gemma4:12b", llm.Options{})

	if err == nil {
		t.Fatal("expected an error when no client is configured")
	}
}
