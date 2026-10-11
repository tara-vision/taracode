package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// firstRunTimeout bounds the one call the first-run advice makes.
const firstRunTimeout = 5 * time.Second

// printFirstRunAdvice prints the engine's recommended model for this machine's tier, in the same "Advice ...
// taraengine pull <name>" shape `taracode doctor` uses. New calls it right before it fails because no model is
// persisted, configured or pulled. It is best-effort: a server that is not the engine, or an engine that cannot say,
// leaves it silent rather than failing New over an unrelated error.
func printFirstRunAdvice(out io.Writer, client llm.Client) {
	ec, ok := client.(interface{ Engine() *taraengine.Engine })
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), firstRunTimeout)
	defer cancel()
	d, err := ec.Engine().Doctor(ctx)
	if err != nil {
		return
	}
	name := d.Recommendation[d.Machine.Tier]
	if name == "" {
		return
	}
	_, _ = fmt.Fprintf(out, "Advice    recommended for tier %s: %s\n          taraengine pull %s\n", d.Machine.Tier, name,
		name)
}
