package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/llm/taraengine"
)

// awaitEngine waits for an engine that is still starting before the session's first call to it, and prints each step
// the engine reports, once. Another server, or an engine that answers, costs one health call and prints nothing. The
// wait runs under its own bounds, not the provider's short initialization timeout: an engine that fetches its runtime
// takes minutes. A session switch (/model) does not come through here: the engine runs by then.
func awaitEngine(client llm.Client, out io.Writer) error {
	ec, ok := client.(interface{ Engine() *taraengine.Engine })
	if !ok {
		return nil
	}
	return ec.Engine().AwaitReady(context.Background(), func(step string) {
		_, _ = fmt.Fprintf(out, "taraengine is starting: %s\n", step)
	})
}
