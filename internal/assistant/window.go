package assistant

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/tara-vision/taracode/internal/llm"
)

const (
	defaultContextWindow = 32768
	minContextWindow     = 16384
)

// ErrModelWithoutTools is returned when the selected model cannot call tools.
var ErrModelWithoutTools = errors.New("model does not support tools")

// ResolveContextWindow turns the context.window setting ("auto" or a number) into the num_ctx to
// request. modelMax is the model's native maximum (0 = unknown).
func ResolveContextWindow(configured string, modelMax int) (int, string) {
	window := defaultContextWindow
	warning := ""
	if configured != "" && configured != "auto" {
		n, err := strconv.Atoi(configured)
		if err != nil || n <= 0 {
			warning = fmt.Sprintf("context.window %q is not a number; using auto", configured)
		} else {
			window = n
			if modelMax > 0 && window > modelMax {
				warning = fmt.Sprintf("context.window %d clamped to the model maximum %d", window, modelMax)
				window = modelMax
			}
			return window, warning
		}
	}
	if modelMax > 0 && modelMax < window {
		window = modelMax
	}
	if window < minContextWindow {
		warning = fmt.Sprintf(
			"context window %d is below %d; tool-heavy sessions will compact early", window, minContextWindow)
	}
	return window, warning
}

// SetThink changes the reasoning mode for later requests.
func (a *Assistant) SetThink(t llm.Think) { a.think = t }

// Think returns the current reasoning mode.
func (a *Assistant) Think() llm.Think { return a.think }

// applyModelDetails resolves the context window and gates on tool support after a model is chosen.
func (a *Assistant) applyModelDetails(details *llm.ModelDetails, err error) error {
	if err != nil { // OpenAI-compatible servers: no capability data, keep the JSON fallback path
		a.contextWindow = 0
		return nil
	}
	if !details.Has("tools") {
		return fmt.Errorf("%w: %s (pick one from `taracode doctor`)", ErrModelWithoutTools, a.model)
	}
	window, warning := ResolveContextWindow(a.configuredWindow, details.ContextLength)
	a.contextWindow = window
	if a.compactionCfg.MaxTokens <= 0 || a.compactionCfg.MaxTokens > window {
		a.compactionCfg.MaxTokens = window
	}
	if warning != "" {
		fmt.Printf("%s %s\n", "Warning:", warning)
	}
	return nil
}
