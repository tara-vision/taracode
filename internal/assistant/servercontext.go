package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
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
			"Ollama loaded this model with a %d-token context, but the system prompt and tool schemas need about "+
				"%d. Tool definitions will be cut off. Raise context.window in config.yaml so taracode requests a "+
				"bigger window (OLLAMA_CONTEXT_LENGTH only matters for servers taracode does not control).",
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

// resetServerContextCheck clears the cached server context window so the next message
// re-checks it against the newly selected model. Called whenever the active model changes.
func (a *Assistant) resetServerContextCheck() {
	a.serverContextChecked = false
	a.serverContextTokens = 0
}

// checkServerContextOnce asks an Ollama server for the context window it loaded the
// current model with and prints one warning per session when it is too small.
// It runs after a message has been processed, so the model is loaded by then.
func (a *Assistant) checkServerContextOnce() {
	if a.serverContextChecked || a.llm == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	loaded, err := a.llm.Loaded(ctx)
	if errors.Is(err, llm.ErrNotSupported) {
		a.serverContextChecked = true // this backend cannot report it; never ask again
		return
	}
	if err != nil {
		return // server unreachable: try again after the next message
	}
	serverCtx := loadedContextLength(loaded, a.model)
	if serverCtx <= 0 {
		return // the model is not loaded yet: try again after the next message
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

// loadedContextLength returns the context window the server loaded model with, or 0 when it is not
// loaded. Ollama reports an untagged model ("foo") as "foo:latest".
func loadedContextLength(loaded []llm.LoadedModel, model string) int {
	want := model
	if !strings.Contains(model, ":") {
		want = model + ":latest"
	}
	for _, m := range loaded {
		if m.Name == model || m.Name == want {
			return m.ContextLength
		}
	}
	return 0
}
