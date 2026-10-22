package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/evals"
)

// TestFactoryRunRefusesTheBoardResultsDirectory: a live or sampled run never writes where the board reads.
func TestFactoryRunRefusesTheBoardResultsDirectory(t *testing.T) {
	for _, f := range []evalRunFlags{
		{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto", live: true, out: "docs/evals/results"},
		{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto", temperature: 0.7, out: "docs/evals/results"},
		{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto", live: true, out: "./docs/evals/results/"},
	} {
		err := runEvalRun(f, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "board") || !strings.Contains(err.Error(), "--out") {
			t.Fatalf("flags %+v: err = %v, want a refusal naming --out and the board", f, err)
		}
	}
}

// TestEvalRunRefusesABadTemperature: the sampling temperature is a number between 0 and 2.
func TestEvalRunRefusesABadTemperature(t *testing.T) {
	for _, temperature := range []float64{-0.1, 2.5} {
		f := evalRunFlags{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "auto", temperature: temperature, out: t.TempDir()}
		if err := runEvalRun(f, io.Discard); err == nil || !strings.Contains(err.Error(), "--temperature") {
			t.Fatalf("temperature %v: err = %v", temperature, err)
		}
	}
}

// TestEvalReportRefusesFactoryResults: a live or sampled result in the results directory stops the report.
func TestEvalReportRefusesFactoryResults(t *testing.T) {
	results := t.TempDir()
	r := evals.Results{Taracode: "3.4.0", Model: "qwen3.8:27b", Tier: "32", Think: "auto", Date: "2026-10-22", Runs: 1,
		Host: "lab", Live: true}
	if _, err := evals.WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	err := runEvalReport(results, t.TempDir(), "evals/tasks", false, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "factory") || !strings.Contains(err.Error(), "qwen3.8-27b") {
		t.Fatalf("err = %v, want a refusal naming the factory result", err)
	}
}

// TestSandboxLabelIsReadOnlyForALiveRun: a board run records no sandbox even when the variable is set.
func TestSandboxLabelIsReadOnlyForALiveRun(t *testing.T) {
	t.Setenv("TARACODE_EVALS_LIVE_SANDBOX", "kind-taracode-evals")
	if got := sandboxLabel(false); got != "" {
		t.Fatalf("a board run got the label %q", got)
	}
	if got := sandboxLabel(true); got != "kind-taracode-evals" {
		t.Fatalf("a live run got %q", got)
	}
}

// TestScoreboardTargetLintsTheResults: make scoreboard refuses a factory result before it reports.
func TestScoreboardTargetLintsTheResults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "eval lint --results docs/evals/results") {
		t.Fatal("the scoreboard target does not lint the results directory")
	}
}
