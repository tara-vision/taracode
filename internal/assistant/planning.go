package assistant

import (
	gocontext "context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
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
			Role:    openai.ChatMessageRoleSystem,
			Content: "You are a task planning assistant. Generate structured JSON plans for executing multi-step tasks. Be concise and practical.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: prompt,
		},
	}

	// Create request without tools - pure text generation for planning
	req := openai.ChatCompletionRequest{
		Model:    a.model,
		Messages: messages,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	// Use streaming to accumulate the response
	stream, err := a.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create planning stream: %w", err)
	}
	defer stream.Close()

	var response strings.Builder

	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("planning stream error: %w", err)
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				response.WriteString(delta.Content)
			}
		}

		// Track usage
		if chunk.Usage != nil {
			a.sessionUsage.PromptTokens += chunk.Usage.PromptTokens
			a.sessionUsage.CompletionTokens += chunk.Usage.CompletionTokens
			a.sessionUsage.TotalTokens += chunk.Usage.TotalTokens
		}
	}

	return response.String(), nil
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
	req := openai.ChatCompletionRequest{
		Model:    a.model,
		Messages: messages,
	}

	resp, err := a.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("analysis request failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	// Track usage - use API response if available, otherwise estimate
	if resp.Usage.TotalTokens > 0 {
		a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
		a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
		a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
	} else {
		// Estimate tokens when API doesn't return usage (common with Ollama vision)
		// Rough estimation: ~4 chars per token for text, ~1000 tokens per image
		promptTokens := len(prompt)/4 + len(images)*1000
		completionTokens := len(resp.Choices[0].Message.Content) / 4
		totalTokens := promptTokens + completionTokens

		a.sessionUsage.PromptTokens += promptTokens
		a.sessionUsage.CompletionTokens += completionTokens
		a.sessionUsage.TotalTokens += totalTokens
	}

	return resp.Choices[0].Message.Content, nil
}
