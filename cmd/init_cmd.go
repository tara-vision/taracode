package cmd

import (
	"fmt"
	"os"

	"github.com/tara-vision/taracode/internal/assistant"
)

// cmdInit is the /init command: analyze the project, write TARACODE.md and .taracode/, then
// reinitialise the assistant so it picks up the new context.
func (r *repl) cmdInit(_ []string) {
	if err := assistant.InitProject(r.absDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	// Get current host and model from existing assistant to preserve state
	currentHost := r.host
	currentModel := r.model
	currentVendor := r.vendor
	if provInfo := r.asst.GetProviderInfo(); provInfo != nil {
		currentHost = provInfo.Host
		currentModel = provInfo.Model
		currentVendor = provInfo.Type.String()
	}
	// Reinitialize assistant to pick up new context
	newAsst, err := assistant.New(currentHost, r.apiKey, currentModel, currentVendor, r.streaming, r.spinner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reinitializing assistant: %v\n", err)
		return
	}
	r.asst = newAsst
	fmt.Println("Assistant reloaded with project context.")
	fmt.Println()

	if isInitializedProject(r.projectRoot) {
		r.enableProject()
	}
}

// cmdReload is the /reload command: rebuild the assistant from the current connection settings so
// it re-reads TARACODE.md.
func (r *repl) cmdReload(_ []string) {
	newAsst, err := assistant.New(r.host, r.apiKey, r.model, r.vendor, r.streaming, r.spinner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
		return
	}
	r.asst = newAsst
	fmt.Println("Project context reloaded.")
	fmt.Println()
}
