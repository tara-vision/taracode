package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/evals"
)

func TestEvalLintReportsProblems(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bad-task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte("id: bad-task\narea: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runEvalLint(root, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "1 problem") {
		t.Fatalf("err=%v", err)
	}
}

func TestEvalReportWritesTheBoard(t *testing.T) {
	results, out := t.TempDir(), t.TempDir()
	r := evals.Results{Taracode: "t", Model: "gemma4:12b", Tier: "16", Date: "2026-09-26",
		Summary: evals.Summary{PassRate: 1, MeanScore: 1, ByArea: map[string]evals.AreaSummary{}}}
	if _, err := evals.WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	if err := runEvalReport(results, out, 1, true, os.Stdout); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(out, "scoreboard.md"))
	if err != nil || !strings.Contains(string(md), "gemma4:12b (default)") {
		t.Fatalf("%v %q", err, md)
	}
	if _, err := os.Stat(filepath.Join(out, "scoreboard.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvalRunNeedsAModel(t *testing.T) {
	err := runEvalRun(evalRunFlags{host: "http://127.0.0.1:1", corpus: t.TempDir()}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "--model") {
		t.Fatalf("err=%v", err)
	}
}
