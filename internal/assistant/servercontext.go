package assistant

import (
	"context"
	"fmt"
	"time"

	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
)

// serverContextHeadroom is the room the conversation itself needs after the system
// prompt and the tool schemas have been sent.
const serverContextHeadroom = 2048

// ServerContextAdvice compares the context window the server actually runs the model
// with against what taracode needs. It returns a warning and true when the user should
// act, otherwise an empty string and false. A serverCtx of 0 means unknown. threshold is
// the compaction threshold (fraction of configuredMax); values outside (0, 1] mean 1.
func ServerContextAdvice(serverCtx, systemTokens, toolTokens, configuredMax int, threshold float64) (string, bool) {
	if serverCtx <= 0 {
		return "", false
	}
	need := systemTokens + toolTokens + serverContextHeadroom
	if serverCtx < need {
		return fmt.Sprintf(
			"Ollama runs this model with a %d-token context, but the system prompt and tool schemas need about %d. "+
				"Tool definitions will be cut off. Fix: start Ollama with OLLAMA_CONTEXT_LENGTH=32768 "+
				"(or raise Context length in the Ollama app settings) and restart it.",
			serverCtx, need), true
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 1
	}
	compactAt := int(float64(configuredMax) * threshold)
	if configuredMax > 0 && serverCtx < compactAt {
		return fmt.Sprintf(
			"Ollama runs this model with a %d-token context, but compaction only starts at %d tokens "+
				"(max_context_tokens %d). Set max_context_tokens: %d or raise OLLAMA_CONTEXT_LENGTH.",
			serverCtx, compactAt, configuredMax, serverCtx), true
	}
	return "", false
}

// checkServerContextOnce asks an Ollama server for the context window it loaded the
// current model with and prints one warning per session when it is too small.
// It runs after a message has been processed, so the model is loaded by then.
func (a *Assistant) checkServerContextOnce() {
	if a.serverContextChecked || a.provider == nil {
		return
	}
	reporter, ok := a.provider.(provider.ContextReporter)
	if !ok {
		a.serverContextChecked = true
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverCtx, err := reporter.LoadedContextLength(ctx, a.model)
	if err != nil || serverCtx <= 0 {
		return // not loaded yet or server unreachable: try again after the next message
	}
	a.serverContextChecked = true
	a.serverContextTokens = serverCtx

	info := a.GetContextInfo()
	msg, warn := ServerContextAdvice(
		serverCtx, info.SystemPromptTokens, info.ToolDefsTokens, info.MaxTokens, info.CompactionThreshold)
	if warn {
		fmt.Printf("\n%s %s\n", ui.IconWarning, msg)
	}
}
