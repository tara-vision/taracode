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
// request. modelMax is the model's native maximum (0 = unknown). The below-floor warning applies
// on every path, including an explicit numeric window, and combines with a clamp warning when a
// value carries both (never clamped upward).
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
			return withFloorWarning(window, warning)
		}
	}
	if modelMax > 0 && modelMax < window {
		window = modelMax
	}
	return withFloorWarning(window, warning)
}

// withFloorWarning appends the below-floor warning when window is too small for tool-heavy
// sessions, combining it with an existing warning rather than replacing it.
func withFloorWarning(window int, warning string) (int, string) {
	if window >= minContextWindow {
		return window, warning
	}
	floor := fmt.Sprintf(
		"context window %d is below %d; tool-heavy sessions will compact early", window, minContextWindow)
	if warning != "" {
		return window, warning + "; " + floor
	}
	return window, floor
}

// SetThink changes the reasoning mode for later requests, downgrading it to auto when the current
// model has no thinking capability (the same gate applyModelDetails runs on every model switch), so
// a runtime /think cannot put a level on the wire that Ollama will reject with a 400. Returns the
// mode that will actually be sent, which the caller should report instead of the one requested.
func (a *Assistant) SetThink(t llm.Think) llm.Think {
	a.think = t
	a.downgradeThinkIfUnsupported()
	return a.think
}

// Think returns the current reasoning mode.
func (a *Assistant) Think() llm.Think { return a.think }

// applyModelDetails resolves the context window and gates on tool support after a model is chosen.
func (a *Assistant) applyModelDetails(details *llm.ModelDetails, err error) error {
	if err != nil {
		if errors.Is(err, llm.ErrNotSupported) {
			// OpenAI-compatible servers: no capability data; the tools go out and the server decides.
			a.contextWindow = 0
			a.thinkingSupported = true // capabilities unknown; SetThink must not downgrade blindly
			return nil
		}
		// A transient Show failure still requests a window instead of silently disabling num_ctx
		// for the whole session; the tools gate is skipped since capabilities are unknown.
		fmt.Println(a.renderer.WarningMessage(fmt.Sprintf("Could not read model capabilities: %v", err)))
		a.thinkingSupported = true // capabilities unknown; SetThink must not downgrade blindly
		a.resolveAndApplyWindow(0)
		return nil
	}
	if !details.Has("tools") {
		return fmt.Errorf("%w: %s (pick one from `taracode doctor`)", ErrModelWithoutTools, a.model)
	}
	a.resolveAndApplyWindow(details.ContextLength)
	a.thinkingSupported = details.Has("thinking")
	a.downgradeThinkIfUnsupported()
	return nil
}

// resolveAndApplyWindow resolves the num_ctx to request against modelMax, keeps the compaction
// budget in step, and prints any warning ResolveContextWindow has.
func (a *Assistant) resolveAndApplyWindow(modelMax int) {
	window, warning := ResolveContextWindow(a.configuredWindow, modelMax)
	a.contextWindow = window
	if a.compactionCfg.MaxTokens <= 0 || a.compactionCfg.MaxTokens > window {
		a.compactionCfg.MaxTokens = window
	}
	if warning != "" {
		fmt.Println(a.renderer.WarningMessage(warning))
	}
}

// downgradeThinkIfUnsupported resets a configured think level to auto when the current model has no
// thinking capability: sending think on such a model makes Ollama reject every turn with a 400. It
// is a no-op when capabilities are unknown (thinkingSupported defaults to true then) or the level is
// already auto/off, and warns once when it actually changes something. Shared by applyModelDetails
// (every model switch) and SetThink (the runtime /think command), so neither path can bypass it.
func (a *Assistant) downgradeThinkIfUnsupported() {
	if a.thinkingSupported || a.think == llm.ThinkAuto || a.think == llm.ThinkOff {
		return
	}
	fmt.Println(a.renderer.WarningMessage(fmt.Sprintf("%s does not support thinking; using auto", a.model)))
	a.think = llm.ThinkAuto
}
