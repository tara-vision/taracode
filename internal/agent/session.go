package agent

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/storage"
)

// GetSession returns the current session
func (a *Assistant) GetSession() *storage.Session {
	return a.session
}

// GetSessionFresh returns the current session refreshed from storage
// This ensures the message count and other fields are up-to-date
func (a *Assistant) GetSessionFresh() *storage.Session {
	if a.storage == nil || a.session == nil {
		return a.session
	}
	// Reload from storage to get current message count
	session, err := a.storage.GetSession(a.session.ID)
	if err == nil {
		a.session = session
	}
	return a.session
}

// GetStorage returns the storage manager
func (a *Assistant) GetStorage() *storage.Manager {
	return a.storage
}

// ListSessions returns all available sessions
func (a *Assistant) ListSessions() ([]storage.SessionMetadata, error) {
	if a.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return a.storage.ListSessions()
}

// NewSession creates a new conversation session
func (a *Assistant) NewSession(name string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.CreateSession(name)
	if err != nil {
		return err
	}

	a.session = session

	// Reset conversation to just system message
	systemPrompt := buildSystemPrompt(a.workingDir, a.storage, a.mode, a.memoryBudget())
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	return nil
}

// LoadSession loads a previous session by ID
func (a *Assistant) LoadSession(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.GetSession(id)
	if err != nil {
		return err
	}

	a.session = session
	_ = a.storage.SetActiveSession(id)

	// Rebuild conversation from session messages
	systemPrompt := buildSystemPrompt(a.workingDir, a.storage, a.mode, a.memoryBudget())
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	// Add messages from session, properly restoring tool calls
	for _, msg := range session.Messages {
		switch msg.Role {
		case "user":
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: msg.Content,
			})

		case "assistant":
			assistantMsg := openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: msg.Content,
			}

			// Restore tool calls if present (native function calling)
			if len(msg.ToolCalls) > 0 {
				assistantMsg.ToolCalls = convertToOpenAIToolCalls(msg.ToolCalls)
			}

			a.conversation = append(a.conversation, assistantMsg)

		case "tool":
			// Tool response message (native function calling)
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
			})

		case "system":
			// Skip system messages as we've already added our own
			continue
		}
	}

	return nil
}

// convertToOpenAIToolCalls converts storage tool call records to OpenAI format
func convertToOpenAIToolCalls(records []storage.ToolCallRecord) []openai.ToolCall {
	var toolCalls []openai.ToolCall
	for _, rec := range records {
		// Marshal params to JSON for the Arguments field
		argsJSON, _ := json.Marshal(rec.Params)

		toolCalls = append(toolCalls, openai.ToolCall{
			ID:   rec.ID,
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{
				Name:      rec.Tool,
				Arguments: string(argsJSON),
			},
		})
	}
	return toolCalls
}

// GenerateSummary creates an AI-generated summary of the current session
// Returns the summary string, or an error if generation fails.
// This is designed to be called on session exit and should not block for too long.
func (a *Assistant) GenerateSummary() (string, error) {
	if a.session == nil || len(a.session.Messages) < 2 {
		return "", nil // Not enough messages to summarize
	}

	// If already has a summary, skip
	if a.session.Summary != "" {
		return a.session.Summary, nil
	}

	// Build a condensed version of the conversation for summarization
	var conversationText strings.Builder
	for _, msg := range a.session.Messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			// Truncate long messages to keep context manageable
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&conversationText, "%s: %s\n", msg.Role, content)
		}
	}

	// Create a short timeout context for the summary call
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 30*time.Second)
	defer cancel()

	// Build the summarization request
	summaryRequest := llm.Request{
		Model: a.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role: openai.ChatMessageRoleSystem,
				Content: "You are a helpful assistant. Summarize the following DevOps conversation in 1-2 sentences. Focus on " +
					"what was accomplished or discussed. Be concise.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: conversationText.String(),
			},
		},
		Options: llm.Options{NumPredict: 100},
	}

	res, err := a.llm.Chat(ctx, summaryRequest, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	// Track usage from summary generation - use the server numbers if it sent any, otherwise estimate
	a.addUsage(res.Usage, (150+conversationText.Len())/4, len(res.Content)/4)

	summary := strings.TrimSpace(res.Content)

	// Save the summary to storage
	if a.storage != nil && a.session != nil {
		_ = a.storage.UpdateSessionSummary(a.session.ID, summary)
		a.session.Summary = summary
	}

	return summary, nil
}

// addUsage folds one reply's token usage into the session total. Local servers do not always
// report usage, so estimates take over when the reply came back without numbers.
func (a *Assistant) addUsage(usage llm.Usage, estimatedPrompt, estimatedCompletion int) {
	prompt, completion := usage.PromptTokens, usage.CompletionTokens
	if prompt+completion == 0 {
		prompt, completion = estimatedPrompt, estimatedCompletion
	}
	a.sessionUsage.PromptTokens += prompt
	a.sessionUsage.CompletionTokens += completion
	a.sessionUsage.TotalTokens += prompt + completion
}
