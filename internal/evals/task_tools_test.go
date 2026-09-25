package evals

import (
	"testing"

	"github.com/tara-vision/taracode/internal/tools"
)

// TestBuiltinToolsMatchTheRegistry pins the hand-kept builtinTools set to the registry: a tool
// added to or removed from Builtin() must reach the task linter the same day.
func TestBuiltinToolsMatchTheRegistry(t *testing.T) {
	names := tools.NewBuiltinRegistry(tools.Options{}, tools.Config{}).Names()
	if len(names) != len(builtinTools) {
		t.Fatalf("registry has %d tools, builtinTools %d", len(names), len(builtinTools))
	}
	for _, name := range names {
		if !builtinTools[name] {
			t.Errorf("%s is in the registry but not in builtinTools", name)
		}
	}
}
