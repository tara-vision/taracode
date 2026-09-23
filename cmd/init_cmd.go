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
	opts := r.options()
	// Preserve the live connection, which may have moved since startup (e.g. /model), rather than
	// whatever taracode started with.
	if provInfo := r.asst.GetProviderInfo(); provInfo != nil {
		opts.Host = provInfo.Host
		opts.Model = provInfo.Model
		opts.Vendor = provInfo.Type.String()
	}
	opts.Ephemeral = false // InitProject just created .taracode/, so storage is available now
	// Reinitialize assistant to pick up new context
	newAsst, err := assistant.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reinitializing assistant: %v\n", err)
		return
	}
	r.opts = opts
	r.replaceAssistant(newAsst)
	fmt.Println("Assistant reloaded with project context.")
	fmt.Println()

	if isInitializedProject(r.projectRoot) {
		r.enableProject()
	}
}

// cmdReload is the /reload command: rebuild the assistant from the current connection settings so
// it re-reads TARACODE.md.
func (r *repl) cmdReload(_ []string) {
	newAsst, err := assistant.New(r.options())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
		return
	}
	r.replaceAssistant(newAsst)
	fmt.Println("Project context reloaded.")
	fmt.Println()
}
