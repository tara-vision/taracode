// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/ui"
)

// startREPL builds the session state and runs the read-eval loop until exit.
func startREPL() {
	r, err := newREPL()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	defer r.close()
	r.run()
}

// run reads lines until EOF, Ctrl+C or exit.
func (r *repl) run() {
	for {
		line, err := r.rl.Readline()
		if err != nil { // io.EOF or Ctrl+C
			handleExitWithSummary(r.asst)
			return
		}
		r.completer.ClearSuggestion()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			handleExitWithSummary(r.asst)
			return
		}
		r.handleLine(line)
	}
}

// handleLine routes one input line: cd and pwd, @file expansion, a slash command, or a prompt for
// the model. Nothing here is gated on initialisation any more: an uninitialised project runs on an
// ephemeral assistant (nothing persisted), and /init is just one more slash command available at any
// time, not a precondition for the rest of the REPL.
func (r *repl) handleLine(line string) {
	if line == "cd" || strings.HasPrefix(line, "cd ") {
		r.changeDir(strings.TrimSpace(strings.TrimPrefix(line, "cd")))
		return
	}
	if line == "pwd" {
		r.printPwd()
		return
	}
	images, ok := r.expandReferences(&line)
	if !ok {
		return
	}
	if strings.HasPrefix(line, "/") {
		r.dispatch(line)
		return
	}
	r.ask(line, images)
}

// expandReferences replaces @file references in the line and collects referenced images.
func (r *repl) expandReferences(line *string) ([]*assistant.ImageData, bool) {
	if !strings.Contains(*line, "@") {
		return nil, true
	}
	expanded, err := expandFileReferencesWithImages(*line, r.projectRoot, r.absDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return nil, false
	}
	*line = expanded.Text
	return expanded.Images, true
}

// ask sends a prompt to the model, then shows the Tab suggestion, captures memories and refreshes
// the prompt line.
func (r *repl) ask(line string, images []*assistant.ImageData) {
	if err := r.asst.ProcessMessageWithImages(line, images); err != nil {
		fmt.Fprintln(os.Stderr, ui.EnhanceError(err))
	}
	fmt.Println()
	if suggestion := DetectSuggestion(r.asst.GetLastResponse()); suggestion != "" {
		r.completer.SetSuggestion(suggestion)
		fmt.Printf("\033[90m  Tab: %s\033[0m\n", suggestion)
	}
	if r.memory != nil && viper.GetBool("memory.auto_capture") {
		checkAutoCapture(r.memory, line)
	}
	r.refreshPrompt()
}

// formatPrompt builds the current prompt string: the operate-mode marker, the current directory and,
// when show_context_budget is on and usage is known, the token budget. refreshPrompt, openReadline
// and changeDir all build the prompt through this one helper, so the mode marker cannot go stale in
// one of them while it is current in another (ruling P2-R23: openReadline's first prompt and
// changeDir's prompt after cd used to build with the mode-blind FormatPrompt, so the [operate] marker
// was missing on session start in operate mode and after cd, even though refreshPrompt showed it).
// The marker itself does not depend on show_context_budget; that setting only gates the token count.
func (r *repl) formatPrompt() string {
	operate := r.asst.Mode() == policy.ModeOperate
	var usedTokens, maxTokens int
	if viper.GetBool("show_context_budget") {
		if usage := r.asst.GetSessionUsage(); usage != nil {
			usedTokens, maxTokens = usage.TotalTokens, viper.GetInt("max_context_tokens")
		}
	}
	return FormatPromptWithMode(r.relDir, usedTokens, maxTokens, operate)
}

// refreshPrompt updates the prompt line after the mode or the context usage may have changed. A repl
// built directly (as tests do, with no readline instance) leaves r.rl nil; that is a no-op here.
func (r *repl) refreshPrompt() {
	if r.rl == nil {
		return
	}
	r.rl.SetPrompt(r.formatPrompt())
}

// changeDir moves inside the project sandbox.
func (r *repl) changeDir(target string) {
	newRel, newAbs, err := SandboxedPath(target, r.relDir, r.projectRoot)
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		return
	}
	r.relDir, r.absDir = newRel, newAbs
	r.completer.UpdateWorkingDir(r.absDir)
	r.rl.SetPrompt(r.formatPrompt())
	if r.relDir == "" {
		fmt.Println("Changed to project root")
	} else {
		fmt.Printf("Changed to: %s\n", r.relDir)
	}
	fmt.Println()
}

func (r *repl) printPwd() {
	fmt.Println(FormatPwd(r.relDir, r.projectRoot))
	fmt.Println()
}
