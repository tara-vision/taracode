package cmd

import (
	"math"
	"strings"
	"testing"

	"github.com/manifoldco/promptui"

	"github.com/tara-vision/taracode/internal/evals"
)

// TestRunSelectRunsTheRealList: the default runSelect is promptui's list itself, read through
// readline's input (Ctrl-N moves down, Enter picks).
func TestRunSelectRunsTheRealList(t *testing.T) {
	readlineInput(t, "\x0e\r")
	idx, item, err := runSelect(&promptui.Select{Label: "Pick", Items: []string{"README.md", "main.go"}})
	if err != nil || idx != 1 || item != "main.go" {
		t.Fatalf("%d %q %v", idx, item, err)
	}
}

// TestRunPromptWithoutAnAnswerIsAnError: the default runPrompt is promptui's prompt itself, and a
// confirmation nobody answers is an error, which /upgrade now reads as cancelled.
func TestRunPromptWithoutAnAnswerIsAnError(t *testing.T) {
	readlineInput(t, "")
	if _, err := runPrompt(&promptui.Prompt{Label: "Upgrade now", IsConfirm: true}); err == nil {
		t.Fatal("no answer")
	}
}

func TestWritePartialResultsReportsResultsItCannotEncode(t *testing.T) {
	_, err := writePartialResults(t.TempDir(), evals.Results{Model: "gemma4:12b", Temperature: math.NaN()})
	if err == nil || !strings.HasPrefix(err.Error(), "encoding partial results for gemma4:12b: ") {
		t.Fatalf("err = %v", err)
	}
}
