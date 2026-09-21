package assistant

import (
	gocontext "context"
	"fmt"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm"
)

// SendMessageForPlanning sends a prompt to the LLM for planning without tool execution.
// This is used by TaskPlanner to generate execution plans from natural language.
// The response is returned as a string without modifying the main conversation.
func (a *Assistant) SendMessageForPlanning(prompt string) (string, error) {
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	// Build a minimal conversation with just the planning prompt
	messages := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: "You are a task planning assistant. Generate structured JSON plans for executing " +
				"multi-step tasks. Be concise and practical.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: prompt,
		},
	}

	// Request without tools - pure text generation for planning. Streaming keeps the transport
	// the same as a normal turn; the plan is returned to the caller rather than printed, so the
	// callback has nothing to do.
	req := llm.Request{
		Model:    a.model,
		Messages: messages,
		Options:  llm.Options{NumCtx: a.contextWindow, KeepAlive: a.keepAlive},
	}

	res, err := a.llm.Chat(ctx, req, func(llm.Event) error { return nil })
	if err != nil {
		return "", fmt.Errorf("planning request failed: %w", err)
	}

	a.addUsage(res.Usage, 0, 0)

	return res.Content, nil
}

// AnalyzeImages sends images to the LLM for analysis without affecting the conversation
// This is used for screen monitoring where we need standalone analysis
func (a *Assistant) AnalyzeImages(prompt string, images []*ImageData) (string, error) {
	if len(images) == 0 {
		return "", fmt.Errorf("no images provided")
	}

	// Build message with images
	parts := make([]openai.ChatMessagePart, 0, len(images)+1)

	// Add text prompt
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: prompt,
	})

	// Add images
	for _, img := range images {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL:    img.ToDataURL(),
				Detail: openai.ImageURLDetailAuto,
			},
		})
	}

	// Create standalone message (not part of main conversation)
	messages := []openai.ChatCompletionMessage{
		{
			Role:         openai.ChatMessageRoleUser,
			MultiContent: parts,
		},
	}

	// Create context with timeout
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 2*time.Minute)
	defer cancel()

	// Make request without tools (simple analysis)
	req := llm.Request{
		Model:    a.model,
		Messages: messages,
		Options:  llm.Options{NumCtx: a.contextWindow, KeepAlive: a.keepAlive},
	}

	res, err := a.llm.Chat(ctx, req, nil)
	if err != nil {
		return "", fmt.Errorf("analysis request failed: %w", err)
	}

	// Track usage - estimates take over when the server sends none (common with Ollama vision):
	// ~4 chars per token for text, ~1000 tokens per image.
	a.addUsage(res.Usage, len(prompt)/4+len(images)*1000, len(res.Content)/4)

	return res.Content, nil
}
