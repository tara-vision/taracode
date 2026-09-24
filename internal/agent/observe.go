package agent

import (
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

// ToolEvent is one tool call as the gate saw it, reported to Options.ToolObserver after the call
// ran or was refused. Denied calls never reach the tool registry, so an observer is the only place
// they show; the evals runner scores refusal tasks from these events. Rule is one of read, policy,
// mode, protected.<list>, deny.commands, dry_run, permission, user, classifier, unavailable and
// preview.
type ToolEvent struct {
	Tool           string
	Verb           string // the invocation's verb ("" for file tools)
	Args           map[string]any
	Classification policy.Classification
	Allowed        bool          // false when a gate refused the call
	Rule           string        // the gate that decided, one of the rules listed above
	Reason         string        // the denial reason, or the classifier's reason on a mutation
	Duration       time.Duration // time in the tool, 0 when it never ran
	Err            error         // the tool's error when it ran and failed
}

// TurnStats describes the last ProcessMessage turn.
type TurnStats struct {
	Completions      int // model requests in the turn
	ToolCalls        int // tool calls the gate decided, allowed or denied
	Denied           int
	PromptTokens     int
	CompletionTokens int
	Wall             time.Duration
	Truncated        bool // the turn stopped at the iteration cap
}

// LastTurn returns the statistics of the last ProcessMessage turn.
func (a *Assistant) LastTurn() TurnStats { return a.turn }

// observe counts the decided call in the turn and reports it to the observer, when there is one.
func (a *Assistant) observe(call *ToolCall, inv policy.Invocation, outcome toolOutcome) {
	a.turn.ToolCalls++
	if outcome.denied {
		a.turn.Denied++
	}
	if a.observer == nil {
		return
	}
	a.observer(ToolEvent{
		Tool: call.Tool, Verb: inv.Verb, Args: call.Params, Classification: inv.Classification,
		Allowed: !outcome.denied, Rule: outcome.rule, Reason: outcome.reason,
		Duration: time.Duration(outcome.durationMs) * time.Millisecond, Err: outcome.err,
	})
}
