package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/tara-vision/taracode/internal/evals"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
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

// TestEvalCommandsSilenceUsageOnFailure pins the eval command and its four subcommands to
// SilenceUsage, so a failing `eval lint` or `eval run` prints only the error, never cobra's usage
// block. SilenceErrors stays unset, like the rest of cmd/ (main.go's own comment on Execute() relies
// on cobra printing the error itself).
func TestEvalCommandsSilenceUsageOnFailure(t *testing.T) {
	for _, c := range []*cobra.Command{evalCmd, evalRunCmd, evalRecordCmd, evalReportCmd, evalLintCmd} {
		if !c.SilenceUsage {
			t.Errorf("%s: SilenceUsage is false; a failing run would print the usage block too", c.Use)
		}
		if c.SilenceErrors {
			t.Errorf("%s: SilenceErrors is true; the rest of cmd/ leaves it unset", c.Use)
		}
	}
}

// TestEvalLintCommandSilencesUsageOnFailure exercises the real cobra command, not just runEvalLint
// (its testable core): a failing `eval lint` prints the error but never cobra's usage block.
func TestEvalLintCommandSilencesUsageOnFailure(t *testing.T) {
	// rootCmd.Execute runs initConfig, which creates ~/.taracode and loads its config: a throwaway HOME
	// keeps the real one untouched and the real config out of the package's viper.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, "bad-task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte("id: bad-task\narea: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	rootCmd.SetArgs([]string{"eval", "lint", "--corpus", root})
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "1 problem") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Fatalf("the usage block printed on a lint failure: %q", out.String())
	}
}

func TestEvalReportWritesTheBoard(t *testing.T) {
	results, out := t.TempDir(), t.TempDir()
	r := evals.Results{Taracode: "t", Model: "gemma4:12b", Tier: "16", Date: "2026-09-26",
		Summary: evals.Summary{PassRate: 1, MeanScore: 1, ByArea: map[string]evals.AreaSummary{}}}
	if _, err := evals.WriteResults(results, r); err != nil {
		t.Fatal(err)
	}
	if err := runEvalReport(results, out, t.TempDir(), true, os.Stdout); err != nil {
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

// TestEvalReportRefusesADirtyCorpus pins the corpus-lint gate to runEvalReport itself (the testable
// core), not the cobra RunE closure.
func TestEvalReportRefusesADirtyCorpus(t *testing.T) {
	results, out := t.TempDir(), t.TempDir()
	corpus := t.TempDir()
	dir := filepath.Join(corpus, "bad-task")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte("id: bad-task\narea: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runEvalReport(results, out, corpus, false, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "run taracode eval lint") {
		t.Fatalf("err=%v", err)
	}
}

func TestEvalRunNeedsAModel(t *testing.T) {
	err := runEvalRun(evalRunFlags{host: "http://127.0.0.1:1", corpus: t.TempDir()}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "--model") {
		t.Fatalf("err=%v", err)
	}
}

// TestEvalRunRejectsABadThink pins the up-front --think validation: a bad value is rejected before
// any engine call, so a bogus host and an empty corpus never come into play.
func TestEvalRunRejectsABadThink(t *testing.T) {
	err := runEvalRun(evalRunFlags{host: "http://127.0.0.1:1", model: "gemma4:12b", think: "sideways",
		corpus: t.TempDir()}, os.Stdout)
	if err == nil || !strings.Contains(err.Error(), "--think") {
		t.Fatalf("err=%v", err)
	}
}

// fakeEvalOllama starts an ollamatest fake serving gemma4:12b, scripted with the warm-up "ready"
// answer followed by turns.
func fakeEvalOllama(t *testing.T, turns ...ollamatest.Turn) *ollamatest.Server {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = append([]ollamatest.Turn{{Content: "ready"}}, turns...)
	return srv
}

func evalToolCall(name string, args map[string]any) ollamatest.Turn {
	return ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: name, Args: args}}}
}

// partialDefectTask is an authored, single-fixture task: no scenario or recorder needed, only an
// indexed fixture the test makes unreadable to force the corpus-defect stop.
const partialDefectTask = `id: partial-defect
area: kubernetes
mode: investigate
provenance: authored
prompt: Check the pods in namespace shop and report what you find.
expect:
  max_iterations: 4
`

// TestEvalRunKeepsAStoppedRunsPartialResultsOutOfTheResultsDirectory pins ruling P3-R50: when Run
// stops on a corpus defect, eval run must not write the partial results where the scoreboard would
// find and publish them; instead they land in the runs directory, clearly marked partial, and the
// command still fails, naming the task.
func TestEvalRunKeepsAStoppedRunsPartialResultsOutOfTheResultsDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	corpus := t.TempDir()
	dir := filepath.Join(corpus, "partial-defect")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte(partialDefectTask), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := evals.LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	const sig = "kubectl get pod -n shop"
	if err := store.Save(sig, "NAME         READY   STATUS\ncheckout-1   0/1     CrashLoopBackOff", false); err != nil {
		t.Fatal(err)
	}
	var file string
	for _, f := range store.Fixtures() {
		if f.Signature == sig {
			file = f.File
		}
	}
	fixturePath := filepath.Join(store.Dir(), file)
	if err := os.Chmod(fixturePath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(fixturePath, 0o644) })

	srv := fakeEvalOllama(t,
		evalToolCall("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}),
		ollamatest.Turn{Content: "done"},
	)
	out, runsDir := t.TempDir(), t.TempDir()
	f := evalRunFlags{host: srv.URL, model: "gemma4:12b", corpus: corpus, think: "auto", runs: 1,
		timeout: 30 * time.Second, out: out, runsDir: runsDir, hostLabel: "lab"}

	err = runEvalRun(f, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "partial-defect") {
		t.Fatalf("err=%v", err)
	}
	if entries, statErr := os.ReadDir(out); statErr != nil || len(entries) != 0 {
		t.Fatalf("--out should stay empty: entries=%v err=%v", entries, statErr)
	}
	// runsDir also holds the run's own transcript subdirectory (evals.Run writes that regardless);
	// only the partial-results file itself is this test's concern.
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("reading runs dir: %v", err)
	}
	partials := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-partial.json") {
			partials++
		}
	}
	if partials != 1 {
		t.Fatalf("partial results file: entries=%v", entries)
	}
}
