package ui

import (
	"strings"
	"sync"
	"testing"
)

// TestRenderMarkdownIsSafeForConcurrentUse renders from several goroutines at once, as a turn and a
// cancelled turn that is still returning can; under -race an unguarded renderer reports a data race
// on glamour's block stack and can panic on it.
func TestRenderMarkdownIsSafeForConcurrentUse(t *testing.T) {
	const doc = "# Root cause\n\n- the pod was **OOMKilled**\n- its limit is `64Mi`\n\n> raise the memory limit\n"
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if out := RenderMarkdown(doc); !strings.Contains(out, "OOMKilled") {
					t.Errorf("render lost the text: %q", out)
					return
				}
			}
		}()
	}
	wg.Wait()
}
