package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/tara-vision/taracode/internal/evals"
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
	Use:   "eval",
	Short: "Run, record and report the offline DevOps evals (docs/evals/README.md)",
}

var evalRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the corpus against one model and write docs/evals/results/<model>-<date>.json",
	Args:  cobra.NoArgs,
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
	Use:   "record",
	Short: "Record fixtures for the recorded tasks against the live sandbox (runs on the lab VM)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		corpus, _ := cmd.Flags().GetString("corpus")
		scenarios, _ := cmd.Flags().GetString("scenarios")
		glob, _ := cmd.Flags().GetString("tasks")
		return runEvalRecord(corpus, scenarios, glob, cmd.OutOrStdout())
	},
}

var evalReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Regenerate docs/evals/scoreboard.md and scoreboard.json from the results",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		results, _ := cmd.Flags().GetString("results")
		outDir, _ := cmd.Flags().GetString("out-dir")
		corpus, _ := cmd.Flags().GetString("corpus")
		check, _ := cmd.Flags().GetBool("check-defaults")
		count, problems := evals.Lint(corpus)
		if len(problems) > 0 {
			return fmt.Errorf("the corpus does not lint (%d problem(s)); run taracode eval lint", len(problems))
		}
		return runEvalReport(results, outDir, count, check, cmd.OutOrStdout())
	},
}

var evalLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Check every task, its fixtures and its policy file",
	Args:  cobra.NoArgs,
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
		return errors.New("LLM server host not found; set --host, TARACODE_HOST, or hosts: in config.yaml")
	}
	if f.model == "" {
		return errors.New("set --model: evals run one named model at a time")
	}
	tasks, err := evals.LoadCorpus(f.corpus, f.tasks)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no task under %s matches %q", f.corpus, f.tasks)
	}
	res, err := evals.Run(context.Background(), tasks, evals.RunOptions{Host: f.host, APIKey: f.apiKey, Vendor: f.vendor,
		Model: f.model, Think: f.think, Runs: f.runs, Timeout: f.timeout, HostLabel: f.hostLabel, Version: Version,
		RunsDir: f.runsDir, Out: out})
	if err != nil {
		return err
	}
	path, err := evals.WriteResults(f.out, res)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "results written to %s\n", path)
	if res.Summary.SafetyFailures > 0 {
		return fmt.Errorf("%d task(s) with a SAFETY FAILURE: the gate allowed a must_deny call; "+
			"fix taracode before publishing", res.Summary.SafetyFailures)
	}
	return nil
}

// runEvalRecord records every recorded task matching glob.
func runEvalRecord(corpus, scenarios, glob string, out io.Writer) error {
	tasks, err := evals.LoadCorpus(corpus, glob)
	if err != nil {
		return err
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

// runEvalReport builds the scoreboard files from the results directory.
func runEvalReport(results, outDir string, corpusTasks int, checkDefaults bool, out io.Writer) error {
	all, err := evals.ReadResults(results)
	if err != nil {
		return err
	}
	reg, err := models.Load()
	if err != nil {
		return err
	}
	sb := evals.BuildScoreboard(all, reg, corpusTasks, Version)
	//nolint:gosec // outDir is repository content (docs/evals by default)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	//nolint:gosec // repository content
	if err := os.WriteFile(filepath.Join(outDir, "scoreboard.md"), []byte(sb.Markdown()), 0o644); err != nil {
		return err
	}
	data, err := sb.JSON()
	if err != nil {
		return err
	}
	//nolint:gosec // repository content
	if err := os.WriteFile(filepath.Join(outDir, "scoreboard.json"), data, 0o644); err != nil {
		return err
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
