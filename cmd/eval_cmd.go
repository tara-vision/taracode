package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/tara-vision/taracode/internal/evals"
	"github.com/tara-vision/taracode/internal/llm"
	"github.com/tara-vision/taracode/internal/models"
)

// evalRunFlags are the flags of `taracode eval run`.
type evalRunFlags struct {
	host, apiKey, vendor, model string
	corpus, tasks, think        string
	runs                        int
	timeout                     time.Duration
	out, runsDir, hostLabel     string
}

var evalCmd = &cobra.Command{
	Use:          "eval",
	Short:        "Run, record and report the offline DevOps evals (docs/evals/README.md)",
	SilenceUsage: true,
}

var evalRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the corpus against one model and write docs/evals/results/<model>-<date>.json",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		f := evalRun
		f.host, f.apiKey, f.vendor, f.model = resolveDoctorTarget()
		if flagModel, _ := cmd.Flags().GetString("model"); flagModel != "" {
			f.model = flagModel
		}
		return runEvalRun(f, cmd.OutOrStdout())
	},
}

var evalRun evalRunFlags

var evalRecordCmd = &cobra.Command{
	Use:          "record",
	Short:        "Record fixtures for the recorded tasks against the live sandbox (runs on the lab VM)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		corpus, _ := cmd.Flags().GetString("corpus")
		scenarios, _ := cmd.Flags().GetString("scenarios")
		glob, _ := cmd.Flags().GetString("tasks")
		return runEvalRecord(corpus, scenarios, glob, cmd.OutOrStdout())
	},
}

var evalReportCmd = &cobra.Command{
	Use:          "report",
	Short:        "Regenerate docs/evals/scoreboard.md and scoreboard.json from the results",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		results, _ := cmd.Flags().GetString("results")
		outDir, _ := cmd.Flags().GetString("out-dir")
		corpus, _ := cmd.Flags().GetString("corpus")
		check, _ := cmd.Flags().GetBool("check-defaults")
		return runEvalReport(results, outDir, corpus, check, cmd.OutOrStdout())
	},
}

var evalLintCmd = &cobra.Command{
	Use:          "lint",
	Short:        "Check every task, its fixtures and its policy file",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		corpus, _ := cmd.Flags().GetString("corpus")
		return runEvalLint(corpus, cmd.OutOrStdout())
	},
}

func init() {
	evalRunCmd.Flags().StringVar(&evalRun.corpus, "corpus", "evals/tasks", "task directory")
	evalRunCmd.Flags().StringVar(&evalRun.tasks, "tasks", "", "glob over task ids (default: every task)")
	evalRunCmd.Flags().StringVar(&evalRun.think, "think", "auto", "reasoning mode: auto, off, on, low, medium, high")
	evalRunCmd.Flags().IntVar(&evalRun.runs, "runs", 1, "repetitions per task, averaged")
	evalRunCmd.Flags().DurationVar(&evalRun.timeout, "timeout", 10*time.Minute, "wall-clock limit per task")
	evalRunCmd.Flags().StringVar(&evalRun.out, "out", "docs/evals/results", "results directory")
	evalRunCmd.Flags().StringVar(&evalRun.runsDir, "runs-dir", "evals/runs", "transcripts directory (\"\" = none)")
	evalRunCmd.Flags().StringVar(&evalRun.hostLabel, "host-label", "lab",
		"label for the results' host field (never a host name)")
	evalRecordCmd.Flags().String("corpus", "evals/tasks", "task directory")
	evalRecordCmd.Flags().String("scenarios", "evals/scenarios", "scenario directory")
	evalRecordCmd.Flags().String("tasks", "", "glob over task ids (default: every recorded task)")
	evalReportCmd.Flags().String("results", "docs/evals/results", "results directory")
	evalReportCmd.Flags().String("out-dir", "docs/evals", "where scoreboard.md and scoreboard.json go")
	evalReportCmd.Flags().String("corpus", "evals/tasks", "task directory (for the task count)")
	evalReportCmd.Flags().Bool("check-defaults", false, "print the registry default and the top scorer per tier")
	evalLintCmd.Flags().String("corpus", "evals/tasks", "task directory")
	evalCmd.AddCommand(evalRunCmd, evalRecordCmd, evalReportCmd, evalLintCmd)
}

// runEvalRun is the testable core of `eval run`.
func runEvalRun(f evalRunFlags, out io.Writer) error {
	if f.host == "" {
		return noHost()
	}
	if f.model == "" {
		return errors.New("set --model: evals run one named model at a time")
	}
	if _, ok := llm.ParseThink(f.think); !ok {
		return fmt.Errorf("--think %q not recognized; use one of: auto, off, on, low, medium, high", f.think)
	}
	tasks, err := evals.LoadCorpus(f.corpus, f.tasks)
	if err != nil {
		return fmt.Errorf("loading the corpus from %s: %w", f.corpus, err)
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no task under %s matches %q", f.corpus, f.tasks)
	}
	res, err := evals.Run(context.Background(), tasks, evals.RunOptions{Host: f.host, APIKey: f.apiKey, Vendor: f.vendor,
		Model: f.model, Think: f.think, Runs: f.runs, Timeout: f.timeout, HostLabel: f.hostLabel, Version: Version,
		RunsDir: f.runsDir, Out: out})
	if err != nil {
		return reportStoppedRun(f, tasks, res, err, out)
	}
	path, err := evals.WriteResults(f.out, res)
	if err != nil {
		return fmt.Errorf("writing results to %s: %w", f.out, err)
	}
	_, _ = fmt.Fprintf(out, "results written to %s\n", path)
	if res.Summary.SafetyFailures > 0 {
		return fmt.Errorf("%d task(s) with a SAFETY FAILURE: the gate allowed a must_deny call; "+
			"fix taracode before publishing", res.Summary.SafetyFailures)
	}
	return nil
}

// reportStoppedRun handles a Run error (ruling P3-R50). A corpus defect leaves res holding every task
// completed before the stop; those partial results must never reach the results directory the
// scoreboard reads (it skips only safety-failure files, so a partial file there would be published as
// the model's newest run). Instead they go to the runs directory, purely for an operator's own
// troubleshooting, clearly marked as partial. A hard failure before any task ran (a bad host, an
// unusable model) leaves res empty and nothing to write.
func reportStoppedRun(f evalRunFlags, tasks []evals.Task, res evals.Results, runErr error, out io.Writer) error {
	if len(res.Tasks) == 0 || f.runsDir == "" {
		return fmt.Errorf("running the corpus against %s: %w", f.model, runErr)
	}
	path, writeErr := writePartialResults(f.runsDir, res)
	if writeErr != nil {
		return fmt.Errorf("running the corpus against %s: %w (writing partial results to %s: %v)",
			f.model, runErr, f.runsDir, writeErr)
	}
	_, _ = fmt.Fprintf(out, "stopped after %d of %d task(s); partial results in %s\n", len(res.Tasks), len(tasks), path)
	return fmt.Errorf("running the corpus against %s: %w", f.model, runErr)
}

// writePartialResults writes a stopped run's partial task results as JSON to
// <runsDir>/<model-slug>-<date>-partial.json, mirroring evals.WriteResults' file shape.
func writePartialResults(runsDir string, res evals.Results) (string, error) {
	//nolint:gosec // runsDir is a git-ignored working directory (evals/runs by default)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", runsDir, err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding partial results for %s: %w", res.Model, err)
	}
	path := filepath.Join(runsDir, partialResultsName(res))
	//nolint:gosec // evals/runs is a git-ignored working directory
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// partialResultsName mirrors evals.WriteResults' file-naming scheme with a "-partial" marker, so the
// scoreboard - which only ever reads the results directory, never the runs directory - can never
// mistake this for a committed result.
func partialResultsName(res evals.Results) string {
	return evals.ModelSlug(res.Model) + "-" + res.Date + "-partial.json"
}

// runEvalRecord records every recorded task matching glob.
func runEvalRecord(corpus, scenarios, glob string, out io.Writer) error {
	tasks, err := evals.LoadCorpus(corpus, glob)
	if err != nil {
		return fmt.Errorf("loading the corpus from %s: %w", corpus, err)
	}
	recorded, failed := 0, 0
	for _, t := range tasks {
		if t.Record == nil {
			continue
		}
		if err := evals.RecordTask(context.Background(), t, scenarios, evals.KubectlResolver, out); err != nil {
			_, _ = fmt.Fprintf(out, "!! %v\n", err)
			failed++
			continue
		}
		recorded++
	}
	_, _ = fmt.Fprintf(out, "recorded %d task(s), %d failed\n", recorded, failed)
	if failed > 0 {
		return fmt.Errorf("%d task(s) failed to record", failed)
	}
	return nil
}

// runEvalReport builds the scoreboard files from the results directory. The corpus-lint gate lives
// here, not in the cobra command, so the testable core enforces it on its own.
func runEvalReport(results, outDir, corpus string, checkDefaults bool, out io.Writer) error {
	count, problems := evals.Lint(corpus)
	if len(problems) > 0 {
		return fmt.Errorf("the corpus does not lint (%d problem(s)); run taracode eval lint", len(problems))
	}
	all, err := evals.ReadResults(results)
	if err != nil {
		return fmt.Errorf("loading results from %s: %w", results, err)
	}
	reg, err := models.Load()
	if err != nil {
		return fmt.Errorf("loading the model registry: %w", err)
	}
	sb := evals.BuildScoreboard(all, reg, count, Version)
	//nolint:gosec // outDir is repository content (docs/evals by default)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	mdPath := filepath.Join(outDir, "scoreboard.md")
	//nolint:gosec // repository content
	if err := os.WriteFile(mdPath, []byte(sb.Markdown()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", mdPath, err)
	}
	data, err := sb.JSON()
	if err != nil {
		return fmt.Errorf("rendering the scoreboard json: %w", err)
	}
	jsonPath := filepath.Join(outDir, "scoreboard.json")
	//nolint:gosec // repository content
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", jsonPath, err)
	}
	_, _ = fmt.Fprintf(out, "scoreboard: %d model(s) in %d tier(s), %d result file(s) left out\n",
		countRows(sb), len(sb.Tiers), len(sb.Skipped))
	for _, s := range sb.Skipped {
		_, _ = fmt.Fprintf(out, "  left out: %s\n", s)
	}
	if checkDefaults {
		for _, line := range sb.CheckDefaults(reg) {
			_, _ = fmt.Fprintln(out, line)
		}
	}
	return nil
}

func countRows(sb evals.Scoreboard) int {
	n := 0
	for _, t := range sb.Tiers {
		n += len(t.Rows)
	}
	return n
}

// runEvalLint prints the problems and fails when there are any.
func runEvalLint(corpus string, out io.Writer) error {
	count, problems := evals.Lint(corpus)
	for _, p := range problems {
		_, _ = fmt.Fprintln(out, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d task(s), %d problem(s)", count, len(problems))
	}
	_, _ = fmt.Fprintf(out, "ok: %d task(s)\n", count)
	return nil
}
