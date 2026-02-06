package assistant

import (
	gocontext "context"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// truncateRuneSafe truncates a string at a rune boundary, avoiding mid-rune slicing
func truncateRuneSafe(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// CompactionConfig holds conversation compaction settings
type CompactionConfig struct {
	Enabled    bool    // Enable auto-compaction
	Threshold  float64 // Trigger at this fraction of max tokens (e.g., 0.75)
	KeepRecent int     // Number of recent message pairs to keep untouched
	MaxTokens  int     // Max context tokens (from config)
}

// CompactionEvent records when compaction happened
type CompactionEvent struct {
	Timestamp      time.Time
	TokensBefore   int
	TokensAfter    int
	MessagesBefore int
	MessagesAfter  int
}

// CompactionState tracks compaction history for the session
type CompactionState struct {
	Events         []CompactionEvent
	TotalCompacted int // Total messages compacted across all events
}

// NewCompactionState creates a new compaction state tracker
func NewCompactionState() *CompactionState {
	return &CompactionState{
		Events: make([]CompactionEvent, 0),
	}
}

// EstimateTokens provides a rough token estimate for a string.
// Uses the heuristic of ~4 characters per token for English text.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// ~4 chars per token is a reasonable heuristic for English text with code
	return (len(text) + 3) / 4
}

// EstimateConversationTokens estimates total tokens in the conversation
func EstimateConversationTokens(conversation []openai.ChatCompletionMessage) int {
	total := 0
	for _, msg := range conversation {
		total += EstimateTokens(msg.Content)
		// Account for role overhead (~4 tokens per message)
		total += 4
		// Tool calls add tokens
		for _, tc := range msg.ToolCalls {
			total += EstimateTokens(tc.Function.Name)
			total += EstimateTokens(tc.Function.Arguments)
		}
	}
	return total
}

// EstimateToolDefsTokens estimates tokens used by tool definitions
func EstimateToolDefsTokens(toolDefs []openai.Tool) int {
	total := 0
	for _, tool := range toolDefs {
		if tool.Function == nil {
			continue
		}
		total += EstimateTokens(tool.Function.Name)
		total += EstimateTokens(tool.Function.Description)
		// Parameters JSON schema adds tokens
		if tool.Function.Parameters != nil {
			total += 50 // rough estimate per tool parameter schema
		}
	}
	return total
}

// ShouldCompact checks if the conversation needs compaction
func ShouldCompact(conversation []openai.ChatCompletionMessage, toolDefs []openai.Tool, cfg CompactionConfig) bool {
	if !cfg.Enabled || cfg.MaxTokens <= 0 {
		return false
	}

	// Need at least enough messages to compact (keep_recent pairs * 2 + system + some to compact)
	minMessages := cfg.KeepRecent*2 + 3 // system + at least 1 pair to compact + keep_recent pairs
	if len(conversation) < minMessages {
		return false
	}

	totalTokens := EstimateConversationTokens(conversation) + EstimateToolDefsTokens(toolDefs)
	threshold := int(float64(cfg.MaxTokens) * cfg.Threshold)

	return totalTokens > threshold
}

// CompactConversation summarizes older messages to free context space.
// Keeps the system prompt (first message) and the last keepRecent user+assistant pairs.
// Replaces everything in between with a summary.
func CompactConversation(
	ctx gocontext.Context,
	conversation []openai.ChatCompletionMessage,
	toolDefs []openai.Tool,
	cfg CompactionConfig,
	client *openai.Client,
	model string,
) ([]openai.ChatCompletionMessage, *CompactionEvent, error) {
	if len(conversation) < cfg.KeepRecent*2+3 {
		return conversation, nil, nil
	}

	tokensBefore := EstimateConversationTokens(conversation) + EstimateToolDefsTokens(toolDefs)

	// Identify what to keep vs. what to summarize
	// conversation[0] = system prompt (always keep)
	// conversation[1..N-keepRecent*2] = summarize these
	// conversation[N-keepRecent*2..N] = keep these recent messages

	keepEnd := len(conversation) - cfg.KeepRecent*2
	if keepEnd < 2 {
		keepEnd = 2 // Keep at least system prompt + 1 message
	}

	toSummarize := conversation[1:keepEnd]
	toKeep := conversation[keepEnd:]

	// Build a summary of the old messages
	summary, err := generateSummary(ctx, toSummarize, client, model)
	if err != nil {
		// Fallback: simple message count summary
		summary = buildFallbackSummary(toSummarize)
	}

	// Build new conversation: system prompt + summary message + recent messages
	compacted := make([]openai.ChatCompletionMessage, 0, 2+len(toKeep))
	compacted = append(compacted, conversation[0]) // system prompt

	// Insert summary as a system message
	compacted = append(compacted, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: summary,
	})

	// Append recent messages
	compacted = append(compacted, toKeep...)

	tokensAfter := EstimateConversationTokens(compacted) + EstimateToolDefsTokens(toolDefs)

	event := &CompactionEvent{
		Timestamp:      time.Now(),
		TokensBefore:   tokensBefore,
		TokensAfter:    tokensAfter,
		MessagesBefore: len(conversation),
		MessagesAfter:  len(compacted),
	}

	return compacted, event, nil
}

// generateSummary uses the LLM to create a concise summary of old messages
func generateSummary(
	ctx gocontext.Context,
	messages []openai.ChatCompletionMessage,
	client *openai.Client,
	model string,
) (string, error) {
	// Build a condensed representation of the messages to summarize
	var sb strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case openai.ChatMessageRoleUser:
			content := truncateRuneSafe(msg.Content, 200)
			sb.WriteString(fmt.Sprintf("User: %s\n", content))
		case openai.ChatMessageRoleAssistant:
			content := truncateRuneSafe(msg.Content, 200)
			sb.WriteString(fmt.Sprintf("Assistant: %s\n", content))
		case openai.ChatMessageRoleTool:
			content := truncateRuneSafe(msg.Content, 100)
			sb.WriteString(fmt.Sprintf("Tool result: %s\n", content))
		}
	}

	summaryPrompt := fmt.Sprintf(`Summarize this conversation history in 2-3 sentences. Focus on:
- What the user asked for
- What actions were taken (files read/modified, commands run)
- Key findings or decisions made

Conversation:
%s

Write ONLY the summary, nothing else.`, sb.String())

	// Use a short timeout for the summary call
	summaryCtx, cancel := gocontext.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(summaryCtx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: summaryPrompt,
			},
		},
		MaxTokens: 200,
	})

	if err != nil {
		return "", fmt.Errorf("summary generation failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty summary response")
	}

	return fmt.Sprintf("[Session context (compacted): %s]", resp.Choices[0].Message.Content), nil
}

// buildFallbackSummary creates a simple summary when LLM summarization fails
func buildFallbackSummary(messages []openai.ChatCompletionMessage) string {
	userCount := 0
	toolCount := 0
	var topics []string

	for _, msg := range messages {
		switch msg.Role {
		case openai.ChatMessageRoleUser:
			userCount++
			// Extract first line as topic hint
			if content := strings.TrimSpace(msg.Content); content != "" {
				firstLine := strings.SplitN(content, "\n", 2)[0]
				firstLine = truncateRuneSafe(firstLine, 60)
				topics = append(topics, firstLine)
			}
		case openai.ChatMessageRoleTool:
			toolCount++
		}
	}

	// Keep at most 3 topic hints
	if len(topics) > 3 {
		topics = topics[:3]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Session context (compacted): %d messages summarized", len(messages)))
	if userCount > 0 {
		if userCount == 1 {
			sb.WriteString(", 1 user query")
		} else {
			sb.WriteString(fmt.Sprintf(", %d user queries", userCount))
		}
	}
	if toolCount > 0 {
		if toolCount == 1 {
			sb.WriteString(", 1 tool call")
		} else {
			sb.WriteString(fmt.Sprintf(", %d tool calls", toolCount))
		}
	}
	if len(topics) > 0 {
		sb.WriteString(fmt.Sprintf(". Topics: %s", strings.Join(topics, "; ")))
	}
	sb.WriteString("]")

	return sb.String()
}
