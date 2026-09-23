package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tara-vision/taracode/internal/agent"
)

// cmdInit is the /init command: analyze the project, write TARACODE.md and .taracode/ (creating the
// starter policy when none exists yet), then reinitialise the assistant so it picks up the new
// context. It always runs against the sandbox root (r.projectRoot), not wherever cd left the session
// (r.absDir): the storage manager and .taracode/ live at the project root, so /init after a cd into a
// subdirectory must not scatter project state there.
func (r *repl) cmdInit(_ []string) {
	if err := agent.InitProject(r.projectRoot, Version); err != nil {
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
	newAsst, err := agent.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reinitializing assistant: %v\n", err)
		return
	}
	r.opts = opts
	r.replaceAssistant(newAsst)
	fmt.Println("Assistant reloaded with project context.")
	fmt.Println()

	r.enableProject()
}

// cmdReload is the /reload command: rebuild the assistant from the current connection settings so
// it re-reads TARACODE.md.
func (r *repl) cmdReload(_ []string) {
	newAsst, err := agent.New(r.options())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reloading: %v\n", err)
		return
	}
	r.replaceAssistant(newAsst)
	fmt.Println("Project context reloaded.")
	if _, err := os.Stat(filepath.Join(r.projectRoot, "TARACODE.md")); os.IsNotExist(err) {
		fmt.Println("No TARACODE.md yet; run /init to generate one.")
	}
	fmt.Println()
}
