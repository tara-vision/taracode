package assistant

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tara-vision/taracode/internal/llm"
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

	// Update provider and the llm client bound to it
	a.provider = prov
	a.llm = prov.LLM()

	return hostName, nil
}

// ProcessMessage sends userMessage through the assistant with no images attached.
func (a *Assistant) ProcessMessage(userMessage string) error {
	return a.ProcessMessageWithImages(userMessage, nil)
}

// ProcessMessageWithImages runs one user turn: request, tool calls, tool results, repeat, until
// the model answers without asking for a tool or the iteration budget runs out. Both streaming
// settings take this path; only how the answer reaches the screen differs.
func (a *Assistant) ProcessMessageWithImages(userMessage string, images []*ImageData) error {
	defer a.checkServerContextOnce()
	// Auto-inject datetime for date/time questions so the LLM has the answer
	userMessage = a.injectDatetimeIfNeeded(userMessage)

	a.recordMessage(storage.ConversationMessage{Role: "user", Content: userMessage, Timestamp: time.Now()})
	a.conversation = append(a.conversation, buildUserMessage(userMessage, images))

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	a.lastResponse = ""
	nudged := false
	for i := 0; i < a.maxIterations; i++ {
		a.compactIfNeeded(ctx)

		res, err := a.complete(ctx)
		if err != nil {
			return err
		}

		calls, display := toolCallsOf(res)
		if len(calls) == 0 && display == "" && !nudged {
			// An empty reply gets one nudge instead of ending the turn in silence (v2.0.4).
			nudged = true
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: emptyReplyNudge,
			})
			continue
		}

		a.appendAssistantTurn(res, calls, display)
		a.printAnswer(display)
		if len(calls) == 0 {
			a.lastResponse = display
			return nil
		}
		a.runToolCalls(calls)
	}

	fmt.Printf("\n%s Stopped after %d tool iterations (context.max_tool_iterations)\n",
		ui.IconWarning, a.maxIterations)
	return nil
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
	result, err := tools.DateTimeTool().Run(gocontext.Background(), nil, "")
	if err != nil {
		return userMessage
	}
	return userMessage +
		"\n\n[System: Here is the current date/time from get_datetime tool - use this to answer the user's question]\n" +
		result
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

// emptyReplyNudge is sent once when the model answers with nothing at all (v2.0.4).
const emptyReplyNudge = "Please answer the question directly."

// requestOptions builds the per-request options from the assistant's configuration.
func (a *Assistant) requestOptions() llm.Options {
	options := llm.Options{NumCtx: a.contextWindow, KeepAlive: a.keepAlive, Think: a.think}
	options.Temperature, options.TopP, options.NumPredict = a.modelOptions.LLMValues()
	return options
}

// complete sends the conversation once and returns the reply, handling the host failover and the
// session token accounting.
func (a *Assistant) complete(ctx gocontext.Context) (*llm.Result, error) {
	req := llm.Request{Model: a.model, Messages: a.conversation, Options: a.requestOptions(), Tools: a.toolDefs}

	res, err := a.chat(ctx, req)
	if err != nil && a.hostPool != nil && isHostRetryableError(err) {
		res, err = a.retryOnFallbackHost(ctx, req, err)
	}
	if err != nil {
		return nil, err
	}

	a.sessionUsage.PromptTokens += res.Usage.PromptTokens
	a.sessionUsage.CompletionTokens += res.Usage.CompletionTokens
	a.sessionUsage.TotalTokens += res.Usage.PromptTokens + res.Usage.CompletionTokens
	return res, nil
}

// retryOnFallbackHost switches to a healthy host from the pool and repeats the request. When no
// fallback is available the original error survives (v2.0 multi-host support).
func (a *Assistant) retryOnFallbackHost(
	ctx gocontext.Context, req llm.Request, cause error,
) (*llm.Result, error) {
	hostName, err := a.switchToFallbackProvider()
	if err != nil {
		fmt.Printf("\n%s Primary host unavailable, no fallback available: %v\n", ui.IconWarning, err)
		return nil, cause
	}
	fmt.Printf("\n%s Primary host unavailable, switched to: %s\n", ui.IconWarning, hostName)
	return a.chat(ctx, req)
}

// chat performs one request. Streaming assembles the answer behind the spinner and leaves the
// rendering to the caller, the way v2 did; the only thing shown live is the model's reasoning,
// dimmed, and that is also the only thing that makes the spinner step aside early.
func (a *Assistant) chat(ctx gocontext.Context, req llm.Request) (*llm.Result, error) {
	spinner := a.startTurnSpinner()
	stopSpinner := func() {
		if spinner != nil {
			spinner.Stop()
			spinner = nil
		}
	}
	defer stopSpinner()

	if !a.streaming {
		return a.llm.Chat(ctx, req, nil)
	}

	filter := NewStreamFilter()
	var answer strings.Builder
	reasoned := false
	res, err := a.llm.Chat(ctx, req, func(event llm.Event) error {
		switch event.Kind {
		case llm.EventThinking:
			// Reasoning is printed as it arrives, so the status line has to go first or the two
			// overwrite each other on the same line.
			stopSpinner()
			fmt.Print(a.renderer.Dim(event.Text))
			reasoned = true
		case llm.EventText:
			// Buffer the answer while the spinner runs (Claude Code style, as in v2); the filter
			// keeps <think> blocks from OpenAI-compatible servers out of what gets rendered.
			answer.WriteString(filter.Process(event.Text))
		case llm.EventUsage:
			if spinner != nil && event.Usage != nil {
				spinner.UpdateTokens(
					a.sessionUsage.TotalTokens + event.Usage.PromptTokens + event.Usage.CompletionTokens)
			}
		case llm.EventToolCall:
		}
		return nil
	})
	answer.WriteString(filter.Flush())
	stopSpinner()
	if reasoned {
		fmt.Println()
	}
	if res == nil {
		return nil, err
	}
	assembled := *res
	assembled.Content = answer.String()
	return &assembled, err
}

// startTurnSpinner shows the Claude Code style status line while the model thinks.
func (a *Assistant) startTurnSpinner() *ui.Spinner {
	if !a.enableSpinner {
		return nil
	}
	spinner := ui.NewStatusLineSpinner()
	spinner.UpdateTokens(a.sessionUsage.TotalTokens)
	spinner.Start("")
	return spinner
}

// toolCallsOf returns the native tool calls of a reply and the text to display. Tool calls written
// into the content as JSON are not parsed: 3.0 needs a model with native tool support.
func toolCallsOf(res *llm.Result) (calls []*ToolCall, display string) {
	if len(res.ToolCalls) == 0 {
		return nil, cleanResponse(res.Content)
	}
	calls = make([]*ToolCall, 0, len(res.ToolCalls))
	for _, tc := range res.ToolCalls {
		params := make(map[string]interface{})
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
			params = make(map[string]interface{})
		}
		calls = append(calls, &ToolCall{ID: tc.ID, Tool: tc.Function.Name, Params: params})
	}
	return calls, cleanResponse(res.Content)
}

// appendAssistantTurn stores the assistant reply in the conversation and in the session. Only
// cleaned text is stored, so reasoning never survives the turn that produced it.
func (a *Assistant) appendAssistantTurn(res *llm.Result, calls []*ToolCall, reply string) {
	message := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: reply}
	if len(res.ToolCalls) > 0 {
		message.ToolCalls = res.ToolCalls
	}
	a.conversation = append(a.conversation, message)

	record := storage.ConversationMessage{Role: "assistant", Content: reply, Timestamp: time.Now()}
	for _, call := range calls {
		record.ToolCalls = append(record.ToolCalls, storage.ToolCallRecord{
			ID:     call.ID,
			Tool:   call.Tool,
			Params: call.Params,
		})
	}
	a.recordMessage(record)
}

// printAnswer renders the assembled answer with glamour once the reply is complete, which is what
// both of v2's loops did: nothing of the answer reaches the screen before this point.
func (a *Assistant) printAnswer(display string) {
	if display == "" {
		return
	}
	fmt.Println(ui.RenderMarkdown(display))
}

// toolOutcome is what one tool call produced, including the gates it had to pass.
type toolOutcome struct {
	result     string
	isError    bool  // the tool failed, or a gate could not complete (a failed dry run or backup)
	denied     bool  // a gate (policy, dry run, permission, edit preview) refused the call
	durationMs int64 // time spent in the tool itself, 0 when it never ran
}

// success reports whether the tool actually ran and returned a result.
func (o toolOutcome) success() bool { return !o.isError && !o.denied }

// toolRun is one call about to be executed and its position in the reply.
type toolRun struct {
	call  *ToolCall
	index int
	total int
}

// runToolCalls executes every call of one reply and appends one tool message per call. Every call
// gets a result, including the ones a gate refused, so the model is never left waiting.
func (a *Assistant) runToolCalls(calls []*ToolCall) {
	for idx, call := range calls {
		outcome := a.executeOne(toolRun{call: call, index: idx, total: len(calls)})
		if !outcome.denied {
			// A gate that refused already said so on its own; a status line here would claim the
			// operation happened.
			fmt.Println(a.renderer.FormatToolStatusWithDuration(
				call.Tool, call.Params, outcome.result, outcome.isError, outcome.durationMs))
		}
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:       openai.ChatMessageRoleTool,
			Content:    outcome.result,
			ToolCallID: call.ID,
		})
		a.recordToolResult(call, outcome)
	}
}

// startToolSpinner shows which tool is running, with its position in a multi-tool reply.
func (a *Assistant) startToolSpinner(run toolRun) *ui.Spinner {
	if !a.enableSpinner {
		return nil
	}
	spinner := ui.NewSpinner()
	if run.total > 1 {
		spinner.Start(fmt.Sprintf("Running %s (%d/%d)...", run.call.Tool, run.index+1, run.total))
	} else {
		spinner.Start(fmt.Sprintf("Running %s...", run.call.Tool))
	}
	return spinner
}

// recordToolResult saves the result of a tool call to the session.
func (a *Assistant) recordToolResult(call *ToolCall, outcome toolOutcome) {
	a.recordMessage(storage.ConversationMessage{
		Role:       "tool",
		Content:    outcome.result,
		Timestamp:  time.Now(),
		ToolCallID: call.ID,
		ToolCall: &storage.ToolCallRecord{
			ID:       call.ID,
			Tool:     call.Tool,
			Params:   call.Params,
			Result:   outcome.result,
			Duration: outcome.durationMs,
			Success:  outcome.success(),
		},
	})
}

// recordMessage persists one message when a session is active.
func (a *Assistant) recordMessage(msg storage.ConversationMessage) {
	if a.storage == nil || a.session == nil {
		return
	}
	if err := a.storage.AddMessage(a.session.ID, msg); err != nil {
		fmt.Printf("  %s Could not save the %s message to the session: %v\n", ui.IconWarning, msg.Role, err)
	}
}

// compactIfNeeded summarises older messages when the context budget is reached (v2.0.2). The
// summary request carries the session's own options (num_ctx, keep_alive) so it does not force
// Ollama to reload the runner with a different context window, with thinking off and a small
// token budget so a thinking model spends it on the summary instead of reasoning.
func (a *Assistant) compactIfNeeded(ctx gocontext.Context) {
	if !ShouldCompact(a.conversation, a.toolDefs, a.compactionCfg) {
		return
	}
	options := a.requestOptions()
	options.NumPredict = compactionSummaryTokens
	options.Think = llm.ThinkOff
	compacted, event, err := CompactConversation(ctx, a.conversation, a.toolDefs, a.compactionCfg, a.llm, a.model, options)
	if err != nil || event == nil {
		return
	}
	a.conversation = compacted
	a.compactionState.Events = append(a.compactionState.Events, *event)
	a.compactionState.TotalCompacted += event.MessagesBefore - event.MessagesAfter
	fmt.Printf("  %s Context compacted: %dk -> %dk tokens (%d messages summarized)\n",
		ui.IconInfo,
		event.TokensBefore/1000,
		event.TokensAfter/1000,
		event.MessagesBefore-event.MessagesAfter)
}
