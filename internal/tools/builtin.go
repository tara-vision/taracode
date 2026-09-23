package tools

import (
	"io"

	"github.com/tara-vision/taracode/internal/search"
)

// Config carries the process-level wiring the built-in tools need.
type Config struct {
	Stream          io.Writer            // live shell output; nil = none
	Search          *search.Orchestrator // nil = web_search reports it is not configured
	DefaultSeverity string               // scan severity filter when the call names none
}

// Builtin returns the sixteen tools in the order spec 5.3 lists them.
func Builtin(cfg Config) []*Tool {
	return append(FileTools(),
		ShellTool(cfg.Stream), GitTool(), KubectlTool(), HelmTool(), TerraformTool(), DockerTool(), CloudTool(),
		ScanTool(cfg.DefaultSeverity), WebSearchTool(cfg.Search), WebFetchTool(), DateTimeTool())
}

// NewBuiltinRegistry is the registry the binary uses: every built-in tool, with the options.
func NewBuiltinRegistry(opts Options, cfg Config) *Registry {
	r := NewRegistry(opts)
	for _, t := range Builtin(cfg) {
		r.Register(t)
	}
	return r
}
