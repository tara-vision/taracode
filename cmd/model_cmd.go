// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/models"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdModel is the /model command: switch between available models.
func (r *repl) cmdModel(_ []string) {
	handleModelSwitch(r)
}

// modelWithHost combines model info with host name for multi-host display
type modelWithHost struct {
	provider.ModelInfo
	HostName string
	HostURL  string // URL for recreating assistant if host changes
	APIKey   string
	Vendor   string
}

// handleModelSwitch lists available models and allows switching; a host switch goes through
// r.replaceAssistant so the re-created assistant keeps the history and MCP tool wiring.
func handleModelSwitch(r *repl) {
	// Get current model
	currentModel := r.asst.GetCurrentModel()
	currentHost := ""
	if provInfo := r.asst.GetProviderInfo(); provInfo != nil {
		currentHost = provInfo.Host
	}
	fmt.Printf("Current model: %s\n\n", currentModel)

	allModels, ok := collectAvailableModels(r.asst, r.hostPool)
	if !ok {
		return
	}
	if len(allModels) == 0 {
		fmt.Println("No models available.")
		fmt.Printf("Pull a model with: ollama pull %s (16 GB) or %s (32 GB)\n",
			models.DefaultName(models.Tier16), models.DefaultName(models.Tier32))
		fmt.Println()
		return
	}

	items := buildModelSelectorItems(allModels, currentModel)
	idx, selected := runModelSelector(items)
	if !selected {
		return
	}

	chosen := allModels[idx]
	selectedModel := chosen.Name
	if selectedModel == currentModel && (chosen.HostURL == "" || chosen.HostURL == currentHost) {
		fmt.Printf("Already using %s\n", selectedModel)
		fmt.Println()
		return
	}

	applyModelSwitch(r, chosen, currentModel, currentHost)
}

// collectAvailableModels lists models from the host pool (multi-host) or from the current
// assistant (single host). ok is false when listing failed; the error is already printed.
func collectAvailableModels(
	asst *agent.Assistant, hostPool *provider.HostPool,
) (allModels []modelWithHost, ok bool) {
	if hostPool != nil {
		return collectModelsFromHostPool(hostPool), true
	}
	// Single host mode - use assistant's ListModels
	modelList, err := asst.ListModels()
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.EnhanceError(err))
		fmt.Println()
		return nil, false
	}
	for _, m := range modelList {
		allModels = append(allModels, modelWithHost{ModelInfo: m, HostName: ""})
	}
	return allModels, true
}

// collectModelsFromHostPool gathers the model list from every healthy host in the pool, preferring
// each provider's detailed ListModels when it supports it and falling back to the basic name list.
func collectModelsFromHostPool(hostPool *provider.HostPool) []modelWithHost {
	var allModels []modelWithHost
	for _, hostInfo := range hostPool.GetHostInfo() {
		if hostInfo.Status != provider.HostStatusHealthy {
			continue
		}
		conn, ok := hostPool.GetConnection(hostInfo.Name)
		if !ok || conn.Provider == nil {
			continue
		}

		// Get models from this host's provider
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		hostModels, err := conn.Provider.DetectModels(ctx)
		cancel()
		if err != nil {
			continue
		}

		// Get detailed model info if provider supports it
		if mm, ok := conn.Provider.(provider.ModelManager); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			detailedModels, err := mm.ListModels(ctx)
			cancel()

			if err == nil {
				for _, dm := range detailedModels {
					allModels = append(allModels, modelWithHost{
						ModelInfo: provider.ModelInfo{
							Name:   dm.Name,
							Size:   dm.Size,
							Params: dm.Params,
						},
						HostName: hostInfo.Name,
						HostURL:  conn.Config.URL,
						APIKey:   conn.Config.APIKey,
						Vendor:   conn.Config.Vendor,
					})
				}
				continue
			}
		}

		// Fall back to basic model list
		for _, modelName := range hostModels {
			allModels = append(allModels, modelWithHost{
				ModelInfo: provider.ModelInfo{Name: modelName},
				HostName:  hostInfo.Name,
				HostURL:   conn.Config.URL,
				APIKey:    conn.Config.APIKey,
				Vendor:    conn.Config.Vendor,
			})
		}
	}
	return allModels
}

// buildModelSelectorItems renders one selector line per model: a "*" marker for the current model,
// size/params when known, and the host name in multi-host mode.
func buildModelSelectorItems(allModels []modelWithHost, currentModel string) []string {
	items := make([]string, len(allModels))
	for i, m := range allModels {
		indicator := "  "
		if m.Name == currentModel {
			indicator = "* "
		}

		// Include host name for multi-host mode
		hostLabel := ""
		if m.HostName != "" {
			hostLabel = fmt.Sprintf(" [%s]", m.HostName)
		}

		if m.Params != "" && m.Size > 0 {
			items[i] = fmt.Sprintf("%s%s (%s, %s)%s", indicator, m.Name, m.Params, m.FormatSize(), hostLabel)
		} else if m.Params != "" {
			items[i] = fmt.Sprintf("%s%s (%s)%s", indicator, m.Name, m.Params, hostLabel)
		} else {
			items[i] = fmt.Sprintf("%s%s%s", indicator, m.Name, hostLabel)
		}
	}
	return items
}

// runModelSelector shows the interactive model picker. selected is false when the user cancelled.
func runModelSelector(items []string) (idx int, selected bool) {
	prompt := promptui.Select{
		Label: "Select a model",
		Items: items,
		Size:  15,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		// User cancelled
		fmt.Println("Model switch cancelled.")
		fmt.Println()
		return 0, false
	}
	return idx, true
}

// applyModelSwitch recreates the assistant on a new host when the selected model lives elsewhere
// (multi-host mode), or switches the model in place on the current host.
func applyModelSwitch(r *repl, selected modelWithHost, currentModel, currentHost string) {
	selectedModel := selected.Name
	// Check if we need to switch hosts (multi-host mode)
	needsHostSwitch := selected.HostURL != "" && selected.HostURL != currentHost

	if needsHostSwitch {
		// Update persisted model BEFORE creating new assistant
		// This prevents the warning about saved model not being available
		if storage := r.asst.GetStorage(); storage != nil {
			_ = storage.SetPreferredModel(selectedModel)
		}

		// Recreate assistant with new host
		fmt.Printf("Switching to %s on host %s...\n", selectedModel, selected.HostName)

		opts := r.options()
		opts.Host = selected.HostURL
		opts.APIKey = selected.APIKey
		opts.Model = selectedModel
		opts.Vendor = selected.Vendor
		newAsst, err := agent.New(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error switching host: %v\n", err)
			return
		}
		r.opts = opts
		r.replaceAssistant(newAsst)
		fmt.Printf("Now using: %s [%s]\n", selectedModel, selected.HostName)
	} else {
		// Same host, just switch model
		fmt.Printf("Switching from %s to %s...\n", currentModel, selectedModel)

		if err := r.asst.SwitchModel(selectedModel); err != nil {
			fmt.Fprintf(os.Stderr, "Error switching model: %v\n", err)
			return
		}
		fmt.Printf("Now using: %s\n", selectedModel)
	}
	fmt.Println()
}
