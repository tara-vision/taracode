package agent

import (
	"fmt"
	"io"

	"github.com/tara-vision/taracode/internal/models"
)

// hostRAMGB and loadRegistry are models.HostRAMGB and models.Load; tests replace them to size a
// host and load a registry without depending on the machine.
var (
	hostRAMGB    = models.HostRAMGB
	loadRegistry = models.Load
)

// printFirstRunAdvice prints the registry's recommended model for this host's RAM tier, in the
// same "Advice ... ollama pull <name>" shape `taracode doctor` uses (spec 5.2: "First run with no
// model configured calls the same recommendation"). New calls this right before it fails because
// no model is persisted, configured, or installed on the server. It is best-effort: a host it
// cannot size, or a registry it cannot load, leaves it silent rather than failing New over an
// unrelated error.
func printFirstRunAdvice(out io.Writer) {
	ramGB, err := hostRAMGB()
	if err != nil {
		return
	}
	registry, err := loadRegistry()
	if err != nil {
		return
	}
	entry := registry.DefaultForTier(models.TierFor(ramGB))
	if entry.Name == "" {
		return
	}
	_, _ = fmt.Fprintf(out, "Advice    recommended for %d GB: %s (%.0f GB download)\n          ollama pull %s\n",
		ramGB, entry.Name, entry.DownloadGB, entry.Name)
}
