// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/storage"
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

// handleLine routes one input line: cd and pwd, the init gate, @file expansion, a slash command,
// or a prompt for the model.
func (r *repl) handleLine(line string) {
	if line == "cd" || strings.HasPrefix(line, "cd ") {
		r.changeDir(strings.TrimSpace(strings.TrimPrefix(line, "cd")))
		return
	}
	if line == "pwd" {
		r.printPwd()
		return
	}
	if !r.initialised && !r.allowedBeforeInit(line) {
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

// allowedBeforeInit keeps the old gate: only /init and /help run before the project is initialised.
func (r *repl) allowedBeforeInit(line string) bool {
	if strings.HasPrefix(line, "/") {
		name := strings.Fields(line)[0]
		if name == "/init" || name == "/help" {
			return true
		}
		fmt.Println("Project not initialized. Only /init, /help, and exit are available.")
		fmt.Println("Run /init to enable all features.")
	} else {
		fmt.Println("Project not initialized. Run /init to enable AI chat.")
	}
	fmt.Println()
	return false
}

// expandReferences replaces @file references in the line and collects referenced images.
func (r *repl) expandReferences(line *string) ([]*assistant.ImageData, bool) {
	if !strings.Contains(*line, "@") {
		return nil, true
	}
	if !r.initialised {
		fmt.Println("💡 Tip: Run /init to enable @ file references with Tab completion")
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

// refreshPrompt shows the context budget and the mode in the prompt when configured.
func (r *repl) refreshPrompt() {
	if !viper.GetBool("show_context_budget") {
		return
	}
	usage := r.asst.GetSessionUsage()
	if usage == nil {
		return
	}
	securityMode := r.asst.GetMode() == storage.ModeSecurity
	r.rl.SetPrompt(FormatPromptWithMode(r.relDir, usage.TotalTokens, viper.GetInt("max_context_tokens"), securityMode))
}

// changeDir moves inside the project sandbox.
func (r *repl) changeDir(target string) {
	if !r.initialised {
		fmt.Println("Project not initialized. Run /init first to enable cd.")
		fmt.Println()
		return
	}
	newRel, newAbs, err := SandboxedPath(target, r.relDir, r.projectRoot)
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		return
	}
	r.relDir, r.absDir = newRel, newAbs
	r.completer.UpdateWorkingDir(r.absDir)
	r.rl.SetPrompt(FormatPrompt(r.relDir))
	if r.relDir == "" {
		fmt.Println("Changed to project root")
	} else {
		fmt.Printf("Changed to: %s\n", r.relDir)
	}
	fmt.Println()
}

func (r *repl) printPwd() {
	if !r.initialised {
		fmt.Println("Project not initialized. Run /init first to enable pwd.")
		fmt.Println()
		return
	}
	fmt.Println(FormatPwd(r.relDir, r.projectRoot))
	fmt.Println()
}
