package assistant

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/permissions"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

// isHostRetryableError checks if an error indicates a host connection failure
// that should trigger a fallback to another host (v2.0 multi-host support)
func isHostRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "host is down") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "dial tcp") ||
		strings.Contains(errMsg, "network is unreachable") ||
		strings.Contains(errMsg, "connection reset")
}

// switchToFallbackProvider attempts to switch to a healthy fallback host (v2.0)
// Returns the name of the host switched to, or an error if no fallback available
func (a *Assistant) switchToFallbackProvider() (string, error) {
	if a.hostPool == nil {
		return "", fmt.Errorf("no host pool configured")
	}

	prov, hostName, err := a.hostPool.GetDefaultWithFallback()
	if err != nil {
		return "", err
	}

	// Update provider and client
	a.provider = prov
	a.client = prov.CreateClient()

	return hostName, nil
}

func (a *Assistant) ProcessMessage(userMessage string) error {
	return a.ProcessMessageWithImages(userMessage, nil)
}

// ProcessMessageWithImages handles user messages that may include images
func (a *Assistant) ProcessMessageWithImages(userMessage string, images []*ImageData) error {
	defer a.checkServerContextOnce()
	// Auto-inject datetime for date/time questions so the LLM has the answer
	userMessage = a.injectDatetimeIfNeeded(userMessage)

	if a.streaming {
		return a.processMessageStreamingWithImages(userMessage, images)
	}
	return a.processMessageNonStreamingWithImages(userMessage, images)
}

// isDatetimeQuestion checks if a message is asking about current date/time
func isDatetimeQuestion(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	patterns := []string{
		"what day is",
		"what date is",
		"what time is",
		"what's the date",
		"what's the time",
		"what is the date",
		"what is the time",
		"what is today",
		"what's today",
		"current date",
		"current time",
		"today's date",
		"day of the week",
		"day is today",
		"day is tomorrow",
		"date today",
		"time now",
		"what day are we",
		"tell me the date",
		"tell me the time",
		"tell me what day",
		"tell me what time",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// injectDatetimeIfNeeded appends current datetime to the message if it's a date/time question
func (a *Assistant) injectDatetimeIfNeeded(userMessage string) string {
	if !isDatetimeQuestion(userMessage) {
		return userMessage
	}
	result, err := tools.GetDateTime(map[string]interface{}{}, "")
	if err != nil {
		return userMessage
	}
	return userMessage + "\n\n[System: Here is the current date/time from get_datetime tool - use this to answer the user's question]\n" + result
}

// buildUserMessage creates an OpenAI message with optional images
func buildUserMessage(text string, images []*ImageData) openai.ChatCompletionMessage {
	if len(images) == 0 {
		return openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: text,
		}
	}

	// Build multipart message with text and images
	parts := make([]openai.ChatMessagePart, 0, len(images)+1)

	// Add text part first
	parts = append(parts, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: text,
	})

	// Add image parts
	for _, img := range images {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL:    img.ToDataURL(),
				Detail: openai.ImageURLDetailAuto,
			},
		})
	}

	return openai.ChatCompletionMessage{
		Role:         openai.ChatMessageRoleUser,
		MultiContent: parts,
	}
}

// processMessageStreamingWithImages handles messages with real-time streaming output (with optional images)
func (a *Assistant) processMessageStreamingWithImages(userMessage string, images []*ImageData) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	// Build user message with optional images
	a.conversation = append(a.conversation, buildUserMessage(userMessage, images))

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	for i := 0; i < a.maxIterations; i++ {
		// Auto-compact conversation if context budget is getting high (v2.0.2)
		if ShouldCompact(a.conversation, a.toolDefs, a.compactionCfg) {
			compacted, event, err := CompactConversation(ctx, a.conversation, a.toolDefs, a.compactionCfg, a.client, a.model)
			if err == nil && event != nil {
				a.conversation = compacted
				a.compactionState.Events = append(a.compactionState.Events, *event)
				a.compactionState.TotalCompacted += event.MessagesBefore - event.MessagesAfter
				fmt.Printf("  %s Context compacted: %dk -> %dk tokens (%d messages summarized)\n",
					ui.IconInfo,
					event.TokensBefore/1000,
					event.TokensAfter/1000,
					event.MessagesBefore-event.MessagesAfter)
			}
		}

		// Start thinking spinner with Claude Code style status line
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewStatusLineSpinner()
			thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
			thinkingSpinner.Start("")
		}

		// Build request - try with tools first, fall back without if not supported
		req := a.modelOptions.ApplyTo(openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
			Tools:    a.toolDefs, // Add tool definitions for function calling
			StreamOptions: &openai.StreamOptions{
				IncludeUsage: true,
			},
		})

		stream, err := a.client.CreateChatCompletionStream(ctx, req)

		// If model doesn't support tools or there's a tool-related error, retry without them
		if err != nil && (strings.Contains(err.Error(), "does not support tools") ||
			strings.Contains(err.Error(), "tools.function.parameters") ||
			strings.Contains(err.Error(), "400 Bad Request")) {
			req.Tools = nil // Remove tools - fall back to JSON-in-content
			a.useNativeTools = false

			// Rebuild system prompt with full tool examples since we're falling back
			fullPrompt := buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, false)
			if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
				a.conversation[0].Content = fullPrompt
				req.Messages = a.conversation
			}

			stream, err = a.client.CreateChatCompletionStream(ctx, req)
		}

		// If still failing with connection error, try host fallback (v2.0)
		if err != nil && isHostRetryableError(err) && a.hostPool != nil {
			if hostName, switchErr := a.switchToFallbackProvider(); switchErr == nil {
				fmt.Printf("\n%s Primary host unavailable, switched to: %s\n", ui.IconWarning, hostName)
				stream, err = a.client.CreateChatCompletionStream(ctx, req)
			} else {
				// Fallback also failed - log for visibility
				fmt.Printf("\n%s Primary host unavailable, no fallback available: %v\n", ui.IconWarning, switchErr)
			}
		}

		if err != nil {
			if thinkingSpinner != nil {
				thinkingSpinner.Stop()
			}
			return fmt.Errorf("failed to create stream: %w", err)
		}

		filter := NewStreamFilter()

		// Accumulate tool calls from stream (native function calling)
		var streamToolCalls []openai.ToolCall
		toolCallsMap := make(map[int]*openai.ToolCall) // Index -> ToolCall for accumulation

		// Buffer the response while showing spinner (Claude Code style)
		for {
			chunk, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				if thinkingSpinner != nil {
					thinkingSpinner.Stop()
				}
				stream.Close()
				return fmt.Errorf("stream error: %w", err)
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta

				// Accumulate content
				if delta.Content != "" {
					filter.Process(delta.Content)
				}

				// Accumulate tool calls from delta (OpenAI function calling)
				for _, tc := range delta.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}

					if existing, ok := toolCallsMap[idx]; ok {
						// Append to existing tool call
						if tc.Function.Arguments != "" {
							existing.Function.Arguments += tc.Function.Arguments
						}
					} else {
						// New tool call
						newTC := openai.ToolCall{
							ID:   tc.ID,
							Type: tc.Type,
							Function: openai.FunctionCall{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						}
						toolCallsMap[idx] = &newTC
					}
				}
			}

			// Capture usage from final chunk (when StreamOptions.IncludeUsage is true)
			if chunk.Usage != nil {
				a.sessionUsage.PromptTokens += chunk.Usage.PromptTokens
				a.sessionUsage.CompletionTokens += chunk.Usage.CompletionTokens
				a.sessionUsage.TotalTokens += chunk.Usage.TotalTokens
				// Update spinner with new token count
				if thinkingSpinner != nil {
					thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
				}
			}
		}
		stream.Close()

		// Convert map to slice
		for i := 0; i < len(toolCallsMap); i++ {
			if tc, ok := toolCallsMap[i]; ok {
				streamToolCalls = append(streamToolCalls, *tc)
			}
		}

		// Stop spinner now that response is complete
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		// Flush any remaining buffered content
		filter.Flush()

		fullResponse := filter.FullContent()

		// Store last response for suggestion detection
		a.lastResponse = fullResponse

		// Check for native function calls first, then fall back to JSON parsing
		var nativeToolCalls []ToolCall
		hasNativeToolCalls := len(streamToolCalls) > 0

		if hasNativeToolCalls {
			// Convert OpenAI tool calls to our format
			for _, tc := range streamToolCalls {
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					params = make(map[string]interface{})
				}
				nativeToolCalls = append(nativeToolCalls, ToolCall{
					ID:     tc.ID,
					Tool:   tc.Function.Name,
					Params: params,
				})
			}
		}

		// Try JSON parsing from content as fallback (backward compatibility)
		jsonToolCallPtrs, displayText := parseToolCalls(fullResponse)

		// Use native tool calls if available, otherwise use JSON-parsed ones
		var toolCalls []ToolCall
		if len(nativeToolCalls) > 0 {
			toolCalls = nativeToolCalls
			displayText = cleanResponse(fullResponse) // Display the full response without JSON
		} else {
			// Convert pointer slice to value slice
			for _, tc := range jsonToolCallPtrs {
				toolCalls = append(toolCalls, *tc)
			}
		}

		if len(toolCalls) == 0 {
			// No tool calls - render the response with Glamour
			displayedText := cleanResponse(fullResponse)

			// If the model returned an empty response after tool execution,
			// nudge it to answer using the tool results already in context.
			// Explicitly forbid tool use to prevent infinite tool-calling loops.
			if displayedText == "" && i > 0 {
				a.conversation = append(a.conversation, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleUser,
					Content: fmt.Sprintf("DO NOT call any tools. Using ONLY the tool results already provided above, give a direct answer to: %s", userMessage),
				})
				continue
			}

			if displayedText != "" {
				fmt.Println(ui.RenderMarkdown(displayedText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: fullResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   fullResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Build assistant message with tool calls for conversation
		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: fullResponse,
		}
		if hasNativeToolCalls {
			assistantMsg.ToolCalls = streamToolCalls
		}
		a.conversation = append(a.conversation, assistantMsg)

		// Save assistant message with tool calls to session (once, before execution)
		if a.storage != nil && a.session != nil {
			var toolCallRecords []storage.ToolCallRecord
			for _, tc := range toolCalls {
				toolCallRecords = append(toolCallRecords, storage.ToolCallRecord{
					ID:     tc.ID,
					Tool:   tc.Tool,
					Params: tc.Params,
				})
			}
			storageAssistantMsg := storage.ConversationMessage{
				Role:      "assistant",
				Content:   fullResponse,
				Timestamp: time.Now(),
				ToolCalls: toolCallRecords,
			}
			a.storage.AddMessage(a.session.ID, storageAssistantMsg)
		}

		// Execute all tool calls
		totalTools := len(toolCalls)
		var toolMessages []openai.ChatCompletionMessage

		// Count tools requiring security audit for batch handling
		var auditBatch *ui.BatchAuditContext
		if a.mode == storage.ModeSecurity {
			auditCount := 0
			for _, tc := range toolCalls {
				cat := permissions.GetToolCategory(tc.Tool)
				if cat != permissions.CategoryRead {
					auditCount++
				}
			}
			if auditCount > 1 {
				auditBatch = &ui.BatchAuditContext{
					TotalTools:   auditCount,
					CurrentIndex: 0,
				}
			}
		}

		for idx, toolCall := range toolCalls {
			var result string
			var isError bool
			var duration int64

			// Check permission before executing
			if allowed, permResult := a.checkToolPermission(toolCall.Tool, toolCall.Params); !allowed {
				result = permResult
				isError = true
				duration = 0
			} else {
				// Update batch index for non-read operations
				if auditBatch != nil {
					cat := permissions.GetToolCategory(toolCall.Tool)
					if cat != permissions.CategoryRead {
						auditBatch.CurrentIndex++
					}
				}

				// Security audit enforcement (security mode only)
				// This is separate from permissions - even "always allow" tools get audited in security mode
				if auditAllowed, auditResult := a.checkSecurityAudit(toolCall.Tool, toolCall.Params, auditBatch); !auditAllowed {
					result = auditResult
					isError = false // User denial is not an error, it's a choice

					// Build tool result message for blocked operation
					if hasNativeToolCalls && toolCall.ID != "" {
						toolMessages = append(toolMessages, openai.ChatCompletionMessage{
							Role:       openai.ChatMessageRoleTool,
							Content:    result,
							ToolCallID: toolCall.ID,
						})
					}

					// Save blocked operation to session
					if a.storage != nil && a.session != nil {
						toolResultMsg := storage.ConversationMessage{
							Role:       "tool",
							Content:    result,
							Timestamp:  time.Now(),
							ToolCallID: toolCall.ID,
							ToolCall: &storage.ToolCallRecord{
								ID:       toolCall.ID,
								Tool:     toolCall.Tool,
								Params:   toolCall.Params,
								Result:   result,
								Duration: 0,
								Success:  false,
							},
						}
						a.storage.AddMessage(a.session.ID, toolResultMsg)
					}
					continue // Skip to next tool
				}

				// Handle edit preview for edit_file operations
				if toolCall.Tool == "edit_file" {
					proceed, previewResult, _ := a.handleEditPreview(toolCall.Params)
					if !proceed {
						result = previewResult
						isError = false // Cancellation is not an error
						duration = 0
						// Skip to next tool - DisplayEditCancelled already printed the message
						continue
					}
				}

				// Start tool execution spinner with progress
				var toolSpinner *ui.Spinner
				if a.enableSpinner {
					toolSpinner = ui.NewSpinner()
					if totalTools > 1 {
						toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
					} else {
						toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
					}
				}

				// Inject security defaults for security tools
				execParams := toolCall.Params
				if tools.IsSecurityTool(toolCall.Tool) {
					defaultSeverity := viper.GetString("security.default_severity")
					execParams = tools.InjectSecurityDefaults(toolCall.Tool, toolCall.Params, defaultSeverity)
				}

				// Execute the tool
				startTime := time.Now()
				var err error
				result, err = a.toolRegistry.ExecuteTool(toolCall.Tool, execParams, a.workingDir)
				duration = time.Since(startTime).Milliseconds()
				isError = err != nil
				if isError {
					result = fmt.Sprintf("Error: %v", err)
				}

				// Apply tool output truncation (v2.0.2)
				if !isError {
					truncResult := TruncateToolOutput(result, toolCall.Tool, a.truncationCfg)
					if truncResult.WasTruncated {
						result = truncResult.Output
						a.truncationEvents = append(a.truncationEvents, truncResult)
					}
				}

				// Stop tool spinner
				if toolSpinner != nil {
					toolSpinner.Stop()
				}
			}

			// Print concise tool status using renderer (with duration)
			fmt.Println(a.renderer.FormatToolStatusWithDuration(toolCall.Tool, toolCall.Params, result, isError, duration))

			// Build tool result message
			if hasNativeToolCalls && toolCall.ID != "" {
				// Native function calling - use tool message format
				toolMessages = append(toolMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				})

				// Save tool response to session
				if a.storage != nil && a.session != nil {
					toolResultMsg := storage.ConversationMessage{
						Role:       "tool",
						Content:    result,
						Timestamp:  time.Now(),
						ToolCallID: toolCall.ID,
						ToolCall: &storage.ToolCallRecord{
							ID:       toolCall.ID,
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolResultMsg)
				}
			} else {
				// Fallback - aggregate results as user message
				if len(toolMessages) == 0 {
					toolMessages = append(toolMessages, openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleUser,
						Content: "",
					})
				}
				if totalTools > 1 {
					toolMessages[0].Content += fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result)
				} else {
					toolMessages[0].Content = fmt.Sprintf("Tool result:\n%s", result)
				}

				// Save tool call result for fallback mode (legacy behavior)
				if a.storage != nil && a.session != nil {
					toolMsg := storage.ConversationMessage{
						Role:      "assistant",
						Content:   fullResponse,
						Timestamp: time.Now(),
						ToolCall: &storage.ToolCallRecord{
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolMsg)
				}
			}
		}

		// Add tool result messages to conversation
		a.conversation = append(a.conversation, toolMessages...)
	}

	return nil
}

// processMessageNonStreamingWithImages handles messages without streaming (with optional images)
func (a *Assistant) processMessageNonStreamingWithImages(userMessage string, images []*ImageData) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	// Build user message with optional images
	a.conversation = append(a.conversation, buildUserMessage(userMessage, images))

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	for i := 0; i < a.maxIterations; i++ {
		// Auto-compact conversation if context budget is getting high (v2.0.2)
		if ShouldCompact(a.conversation, a.toolDefs, a.compactionCfg) {
			compacted, event, err := CompactConversation(ctx, a.conversation, a.toolDefs, a.compactionCfg, a.client, a.model)
			if err == nil && event != nil {
				a.conversation = compacted
				a.compactionState.Events = append(a.compactionState.Events, *event)
				a.compactionState.TotalCompacted += event.MessagesBefore - event.MessagesAfter
				fmt.Printf("  %s Context compacted: %dk -> %dk tokens (%d messages summarized)\n",
					ui.IconInfo,
					event.TokensBefore/1000,
					event.TokensAfter/1000,
					event.MessagesBefore-event.MessagesAfter)
			}
		}

		// Start thinking spinner with Claude Code style status line
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewStatusLineSpinner()
			thinkingSpinner.UpdateTokens(a.sessionUsage.TotalTokens)
			thinkingSpinner.Start("")
		}

		// Build request - try with tools first, fall back without if not supported
		req := a.modelOptions.ApplyTo(openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
			Tools:    a.toolDefs, // Add tool definitions for function calling
		})

		resp, err := a.client.CreateChatCompletion(ctx, req)

		// If model doesn't support tools or there's a tool-related error, retry without them
		if err != nil && (strings.Contains(err.Error(), "does not support tools") ||
			strings.Contains(err.Error(), "tools.function.parameters") ||
			strings.Contains(err.Error(), "400 Bad Request")) {
			req.Tools = nil // Remove tools - fall back to JSON-in-content
			a.useNativeTools = false

			// Rebuild system prompt with full tool examples since we're falling back
			fullPrompt := buildSystemPromptWithModeAndTools(a.workingDir, a.storage, a.mode, false)
			if len(a.conversation) > 0 && a.conversation[0].Role == openai.ChatMessageRoleSystem {
				a.conversation[0].Content = fullPrompt
				req.Messages = a.conversation
			}

			resp, err = a.client.CreateChatCompletion(ctx, req)
		}

		// Stop spinner
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		if err != nil {
			return fmt.Errorf("failed to get response: %w", err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("no response choices returned")
		}

		// Track token usage
		if resp.Usage.TotalTokens > 0 {
			a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
			a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
			a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
		}

		message := resp.Choices[0].Message
		assistantResponse := message.Content

		// Check for native function calls first
		var toolCalls []ToolCall
		hasNativeToolCalls := len(message.ToolCalls) > 0

		if hasNativeToolCalls {
			// Convert OpenAI tool calls to our format
			for _, tc := range message.ToolCalls {
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					params = make(map[string]interface{})
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:     tc.ID,
					Tool:   tc.Function.Name,
					Params: params,
				})
			}
		}

		// Fall back to JSON parsing from content (backward compatibility)
		var displayText string
		if len(toolCalls) == 0 {
			jsonToolCallPtrs, dt := parseToolCalls(assistantResponse)
			displayText = dt
			// Convert pointer slice to value slice
			for _, tc := range jsonToolCallPtrs {
				toolCalls = append(toolCalls, *tc)
			}
		} else {
			displayText = cleanResponse(assistantResponse)
		}

		// If no tool calls, render and print response
		if len(toolCalls) == 0 {
			// If the model returned an empty response after tool execution,
			// nudge it to answer using the tool results instead of breaking.
			if displayText == "" && i > 0 {
				a.conversation = append(a.conversation, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "",
				})
				a.conversation = append(a.conversation, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleUser,
					Content: "Please answer the question using the tool results above.",
				})
				continue
			}

			if displayText != "" {
				// Render with Glamour for syntax highlighting
				fmt.Println(ui.RenderMarkdown(displayText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: assistantResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   assistantResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Build assistant message with tool calls for conversation
		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: assistantResponse,
		}
		if hasNativeToolCalls {
			assistantMsg.ToolCalls = message.ToolCalls
		}
		a.conversation = append(a.conversation, assistantMsg)

		// Save assistant message with tool calls to session (once, before execution)
		if a.storage != nil && a.session != nil {
			var toolCallRecords []storage.ToolCallRecord
			for _, tc := range toolCalls {
				toolCallRecords = append(toolCallRecords, storage.ToolCallRecord{
					ID:     tc.ID,
					Tool:   tc.Tool,
					Params: tc.Params,
				})
			}
			storageAssistantMsg := storage.ConversationMessage{
				Role:      "assistant",
				Content:   assistantResponse,
				Timestamp: time.Now(),
				ToolCalls: toolCallRecords,
			}
			a.storage.AddMessage(a.session.ID, storageAssistantMsg)
		}

		// Execute all tool calls
		totalTools := len(toolCalls)
		var toolMessages []openai.ChatCompletionMessage

		// Count tools requiring security audit for batch handling
		var auditBatch *ui.BatchAuditContext
		if a.mode == storage.ModeSecurity {
			auditCount := 0
			for _, tc := range toolCalls {
				cat := permissions.GetToolCategory(tc.Tool)
				if cat != permissions.CategoryRead {
					auditCount++
				}
			}
			if auditCount > 1 {
				auditBatch = &ui.BatchAuditContext{
					TotalTools:   auditCount,
					CurrentIndex: 0,
				}
			}
		}

		for idx, toolCall := range toolCalls {
			var result string
			var isError bool
			var duration int64

			// Check permission before executing
			if allowed, permResult := a.checkToolPermission(toolCall.Tool, toolCall.Params); !allowed {
				result = permResult
				isError = true
				duration = 0
			} else {
				// Update batch index for non-read operations
				if auditBatch != nil {
					cat := permissions.GetToolCategory(toolCall.Tool)
					if cat != permissions.CategoryRead {
						auditBatch.CurrentIndex++
					}
				}

				// Security audit enforcement (security mode only)
				// This is separate from permissions - even "always allow" tools get audited in security mode
				if auditAllowed, auditResult := a.checkSecurityAudit(toolCall.Tool, toolCall.Params, auditBatch); !auditAllowed {
					result = auditResult
					isError = false // User denial is not an error, it's a choice

					// Build tool result message for blocked operation
					if hasNativeToolCalls && toolCall.ID != "" {
						toolMessages = append(toolMessages, openai.ChatCompletionMessage{
							Role:       openai.ChatMessageRoleTool,
							Content:    result,
							ToolCallID: toolCall.ID,
						})
					}

					// Save blocked operation to session
					if a.storage != nil && a.session != nil {
						toolResultMsg := storage.ConversationMessage{
							Role:       "tool",
							Content:    result,
							Timestamp:  time.Now(),
							ToolCallID: toolCall.ID,
							ToolCall: &storage.ToolCallRecord{
								ID:       toolCall.ID,
								Tool:     toolCall.Tool,
								Params:   toolCall.Params,
								Result:   result,
								Duration: 0,
								Success:  false,
							},
						}
						a.storage.AddMessage(a.session.ID, toolResultMsg)
					}
					continue // Skip to next tool
				}

				// Handle edit preview for edit_file operations
				if toolCall.Tool == "edit_file" {
					proceed, previewResult, _ := a.handleEditPreview(toolCall.Params)
					if !proceed {
						result = previewResult
						isError = false // Cancellation is not an error
						duration = 0
						// Skip to next tool - DisplayEditCancelled already printed the message
						continue
					}
				}

				// Start tool execution spinner with progress
				var toolSpinner *ui.Spinner
				if a.enableSpinner {
					toolSpinner = ui.NewSpinner()
					if totalTools > 1 {
						toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
					} else {
						toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
					}
				}

				// Inject security defaults for security tools
				execParams := toolCall.Params
				if tools.IsSecurityTool(toolCall.Tool) {
					defaultSeverity := viper.GetString("security.default_severity")
					execParams = tools.InjectSecurityDefaults(toolCall.Tool, toolCall.Params, defaultSeverity)
				}

				// Execute the tool
				startTime := time.Now()
				var err error
				result, err = a.toolRegistry.ExecuteTool(toolCall.Tool, execParams, a.workingDir)
				duration = time.Since(startTime).Milliseconds()
				isError = err != nil
				if isError {
					result = fmt.Sprintf("Error: %v", err)
				}

				// Apply tool output truncation (v2.0.2)
				if !isError {
					truncResult := TruncateToolOutput(result, toolCall.Tool, a.truncationCfg)
					if truncResult.WasTruncated {
						result = truncResult.Output
						a.truncationEvents = append(a.truncationEvents, truncResult)
					}
				}

				// Stop tool spinner
				if toolSpinner != nil {
					toolSpinner.Stop()
				}
			}

			// Print concise tool status using renderer (with duration)
			fmt.Println(a.renderer.FormatToolStatusWithDuration(toolCall.Tool, toolCall.Params, result, isError, duration))

			// Build tool result message
			if hasNativeToolCalls && toolCall.ID != "" {
				// Native function calling - use tool message format
				toolMessages = append(toolMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				})

				// Save tool response to session
				if a.storage != nil && a.session != nil {
					toolResultMsg := storage.ConversationMessage{
						Role:       "tool",
						Content:    result,
						Timestamp:  time.Now(),
						ToolCallID: toolCall.ID,
						ToolCall: &storage.ToolCallRecord{
							ID:       toolCall.ID,
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolResultMsg)
				}
			} else {
				// Fallback - aggregate results as user message
				if len(toolMessages) == 0 {
					toolMessages = append(toolMessages, openai.ChatCompletionMessage{
						Role:    openai.ChatMessageRoleUser,
						Content: "",
					})
				}
				if totalTools > 1 {
					toolMessages[0].Content += fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result)
				} else {
					toolMessages[0].Content = fmt.Sprintf("Tool result:\n%s", result)
				}

				// Save tool call result for fallback mode (legacy behavior)
				if a.storage != nil && a.session != nil {
					toolMsg := storage.ConversationMessage{
						Role:      "assistant",
						Content:   assistantResponse,
						Timestamp: time.Now(),
						ToolCall: &storage.ToolCallRecord{
							Tool:     toolCall.Tool,
							Params:   toolCall.Params,
							Result:   result,
							Duration: duration,
							Success:  !isError,
						},
					}
					a.storage.AddMessage(a.session.ID, toolMsg)
				}
			}
		}

		// Add tool result messages to conversation
		a.conversation = append(a.conversation, toolMessages...)
	}

	return nil
}
