package evals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
)

// turnResult is a joined turn: the answer, the loop's statistics read after the join, the turn's
// own error, whether the task limit ended it, and its wall time on the runner's clock.
type turnResult struct {
	answer   string
	stats    agent.TurnStats
	err      error
	timedOut bool
	wall     time.Duration
}

// runTurn runs one turn under the limit through joinTurn and reads the assistant only once the turn
// has returned. The error is joinTurn's: the turn did not return, and the run must stop.
func runTurn(ctx context.Context, a *agent.Assistant, prompt string, timeout time.Duration) (turnResult, error) {
	turn, err := joinTurn(ctx, timeout, joinGrace, func(turnCtx context.Context) error {
		return a.ProcessMessageContext(turnCtx, prompt)
	})
	if err != nil {
		return turn, err
	}
	turn.stats = a.LastTurn()
	if turn.err == nil {
		turn.answer = a.GetLastResponse()
	}
	return turn, nil
}

// joinTurn runs turn under a context that ends with the limit or with ctx, and waits for it to
// return (ruling P3-R47). When that context ends first, the turn is cancelled and has grace to
// return; a turn that does not is an error, and since its goroutine may still call the engine and
// the gate, nothing may run next to it. A turn the limit ended counts as timed out; wall is the
// runner's own clock, from the start to the join.
func joinTurn(ctx context.Context, timeout, grace time.Duration, turn func(context.Context) error) (turnResult, error) {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- turn(turnCtx) }()
	var res turnResult
	select {
	case res.err = <-done:
	case <-turnCtx.Done():
		cancel()
		select {
		case res.err = <-done:
		case <-time.After(grace):
			res.wall = time.Since(start)
			return res, fmt.Errorf("the turn did not return within %s of its cancellation", grace)
		}
	}
	res.wall = time.Since(start)
	res.timedOut = res.err != nil && errors.Is(turnCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	return res, nil
}
