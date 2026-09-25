// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"

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

// handleModelSwitch lists the models the host serves and switches the assistant to the chosen one
// in place, keeping the conversation.
func handleModelSwitch(r *repl) {
	currentModel := r.asst.GetCurrentModel()
	fmt.Printf("Current model: %s\n\n", currentModel)

	allModels, ok := collectAvailableModels(r.asst)
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

	selectedModel := allModels[idx].Name
	if selectedModel == currentModel {
		fmt.Printf("Already using %s\n", selectedModel)
		fmt.Println()
		return
	}

	fmt.Printf("Switching from %s to %s...\n", currentModel, selectedModel)
	if err := r.asst.SwitchModel(selectedModel); err != nil {
		fmt.Fprintf(os.Stderr, "Error switching model: %v\n", err)
		return
	}
	fmt.Printf("Now using: %s\n", selectedModel)
	fmt.Println()
}

// collectAvailableModels lists the models of the assistant's host. ok is false when listing
// failed; the error is already printed.
func collectAvailableModels(asst *agent.Assistant) (allModels []provider.ModelInfo, ok bool) {
	modelList, err := asst.ListModels()
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.EnhanceError(err))
		fmt.Println()
		return nil, false
	}
	return modelList, true
}

// buildModelSelectorItems renders one selector line per model: a "*" marker for the current model
// and the parameter count and size when the host reports them.
func buildModelSelectorItems(allModels []provider.ModelInfo, currentModel string) []string {
	items := make([]string, len(allModels))
	for i, m := range allModels {
		indicator := "  "
		if m.Name == currentModel {
			indicator = "* "
		}
		switch {
		case m.Params != "" && m.Size > 0:
			items[i] = fmt.Sprintf("%s%s (%s, %s)", indicator, m.Name, m.Params, m.FormatSize())
		case m.Params != "":
			items[i] = fmt.Sprintf("%s%s (%s)", indicator, m.Name, m.Params)
		default:
			items[i] = indicator + m.Name
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
