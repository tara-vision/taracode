// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/legacytools"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdTools is the /tools command: list available legacytools.
func (r *repl) cmdTools(_ []string) {
	toolInfoList := legacytools.GetToolInfoList()

	// Group tools by category
	categories := make(map[string][]legacytools.ToolInfo)
	categoryOrder := []string{
		"file", "command", "git", "web", "utility", "kubernetes", "terraform", "docker", "cloud", "security",
	}
	categoryNames := map[string]string{
		"file":       "File Operations",
		"command":    "Command Execution",
		"git":        "Git",
		"web":        "Web",
		"utility":    "Utility",
		"kubernetes": "Kubernetes",
		"terraform":  "Terraform",
		"docker":     "Docker",
		"cloud":      "Cloud (AWS/Azure/GCP)",
		"security":   "Security",
	}

	for _, tool := range toolInfoList {
		categories[tool.Category] = append(categories[tool.Category], tool)
	}

	// Count MCP tools from registry
	mcpToolCount := 0
	if registry := r.asst.GetToolRegistry(); registry != nil {
		mcpToolsByServer := registry.GetMCPTools()
		for _, serverTools := range mcpToolsByServer {
			mcpToolCount += len(serverTools)
		}
	}

	totalTools := len(toolInfoList) + mcpToolCount
	fmt.Printf("Available Tools (%d total):\n\n", totalTools)

	for _, cat := range categoryOrder {
		toolList := categories[cat]
		if len(toolList) == 0 {
			continue
		}
		catName := categoryNames[cat]
		fmt.Printf("  %s:\n", catName)
		for _, t := range toolList {
			fmt.Printf("    %-20s %s\n", t.Name, t.Description)
		}
		fmt.Println()
	}

	// Show MCP tools if any are connected
	if registry := r.asst.GetToolRegistry(); registry != nil {
		mcpToolsByServer := registry.GetMCPTools()
		if len(mcpToolsByServer) > 0 {
			fmt.Printf("  MCP (Model Context Protocol):\n")
			for serverName, serverTools := range mcpToolsByServer {
				for _, toolName := range serverTools {
					fmt.Printf("    %-20s %s\n", toolName, fmt.Sprintf("[%s]", serverName))
				}
			}
			fmt.Println()
		}
	}
}

// initSearchOrchestrator initializes the search orchestrator with config from viper
func initSearchOrchestrator(renderer *ui.Renderer) {
	cfg := GetSearchConfig()

	// Parse timeout
	timeout := 10 * time.Second
	if cfg.Timeout != "" {
		if parsed, err := time.ParseDuration(cfg.Timeout); err == nil {
			timeout = parsed
		}
	}

	// Set up provider switch callback for UI feedback
	legacytools.SetProviderSwitchCallback(func(from, to string, reason error) {
		if renderer != nil {
			fmt.Println()
			fmt.Println(renderer.SearchFallbackMessage(from, to, reason))
		}
	})

	// Initialize the orchestrator with config
	legacytools.InitSearchOrchestrator(legacytools.SearchOrchestratorConfig{
		Primary:         cfg.Primary,
		Fallback:        cfg.Fallback,
		Timeout:         timeout,
		RetryCount:      cfg.RetryCount,
		SearXNGInstance: cfg.SearXNGInstance,
		BraveAPIKey:     cfg.BraveAPIKey,
	})
}

// initCommandStreaming initializes command output streaming
func initCommandStreaming() {
	// Check if streaming is disabled via config
	if viper.GetBool("no_stream_commands") {
		legacytools.DisableStreaming()
		return
	}

	// Enable streaming to stdout with sensible defaults
	legacytools.SetStreamingConfig(legacytools.StreamingConfig{
		Enabled:       true,
		Writer:        os.Stdout,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    256,
	})
}
