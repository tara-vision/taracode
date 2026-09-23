// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/search"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

const (
	// defaultSearchTimeout applies when search.timeout is unset or does not parse.
	defaultSearchTimeout = 10 * time.Second
	// toolDescriptionWidth caps a description in the /tools listing.
	toolDescriptionWidth = 80
)

// cmdTools is the /tools command: every registered tool with its description, whether investigate
// mode exposes it, and the MCP server it came from.
func (r *repl) cmdTools(_ []string) {
	registry := r.asst.ToolRegistry()
	mode := r.asst.Mode()
	servers := map[string]string{}
	for server, names := range registry.MCPTools() {
		for _, name := range names {
			servers[name] = server
		}
	}
	names := registry.Names()
	fmt.Println("Tools (* = has a read form, available in investigate mode):")
	fmt.Println()
	for _, name := range names {
		t, ok := registry.Get(name)
		if !ok {
			continue
		}
		marker := " "
		if t.ReadForm {
			marker = "*"
		}
		line := fmt.Sprintf("  %s %-20s %s", marker, name, ui.TruncateString(t.Description, toolDescriptionWidth))
		if server, isMCP := servers[name]; isMCP {
			line += fmt.Sprintf(" [MCP: %s]", server)
		}
		fmt.Println(line)
	}
	fmt.Println()
	fmt.Printf("%d tools; %d exposed in %s mode\n", len(names), registry.Available(mode), mode)
	fmt.Println()
}

// toolConfig is the process-level wiring for the built-in tools: live shell output, the search
// provider chain and the scan severity default.
func toolConfig(renderer *ui.Renderer) tools.Config {
	cfg := GetSearchConfig()
	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil || timeout <= 0 {
		timeout = defaultSearchTimeout
	}
	primary, fallback := cfg.Primary, cfg.Fallback
	if cfg.BraveAPIKey != "" && primary == "" {
		// A Brave key and no explicit primary: Brave first, DuckDuckGo behind it (2.x behaviour).
		primary, fallback = "brave", "duckduckgo"
	}
	orch := search.NewOrchestrator(search.OrchestratorConfig{
		Primary: primary, Fallback: fallback, Timeout: timeout, RetryCount: cfg.RetryCount,
		CustomSearXNGInstance: cfg.SearXNGInstance, BraveAPIKey: cfg.BraveAPIKey,
		OnProviderSwitch: func(from, to string, reason error) {
			fmt.Println()
			fmt.Println(renderer.SearchFallbackMessage(from, to, reason))
		},
	})
	var stream io.Writer
	if !viper.GetBool("no_stream_commands") {
		stream = os.Stdout
	}
	return tools.Config{Stream: stream, Search: orch, DefaultSeverity: viper.GetString("security.default_severity")}
}
