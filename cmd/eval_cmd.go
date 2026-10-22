package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tara-vision/taracode/internal/evals"
	"github.com/tara-vision/taracode/internal/llm"
)

// evalRunFlags are the flags of `taracode eval run`.
type evalRunFlags struct {
	host, apiKey, vendor, model string
	corpus, tasks, think        string
	runs                        int
	timeout                     time.Duration
	temperature                 float64
	live, transcriptsJSON       bool
	out, runsDir, hostLabel     string
	hardware, gpuProbe          string
	override                    string
}

var evalCmd = &cobra.Command{
	Use:          "eval",
	Short:        "Run, record and report the offline DevOps evals (docs/evals/README.md)",
	SilenceUsage: true,
}

var evalRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the corpus against one model and write docs/evals/results/<model>-<server>[-<machine>]-<date>.json",
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
		if common, _ := cmd.Flags().GetString("common"); common != "" {
			return evals.RecordCommon(context.Background(), common, evals.KubectlResolver, cmd.OutOrStdout())
		}
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
		if seal, _ := cmd.Flags().GetBool("seal"); seal {
			version, _ := cmd.Flags().GetString("version")
			return runEvalSeal(corpus, version, cmd.OutOrStdout())
		}
		results, _ := cmd.Flags().GetString("results")
		return runEvalLint(corpus, results, cmd.OutOrStdout())
	},
}

func init() {
	evalRunCmd.Flags().StringVar(&evalRun.corpus, "corpus", "evals/tasks", "task directory")
	evalRunCmd.Flags().StringVar(&evalRun.tasks, "tasks", "", "glob over task ids (default: every task)")
	evalRunCmd.Flags().StringVar(&evalRun.think, "think", "auto", "reasoning mode: auto, off, on, low, medium, high")
	evalRunCmd.Flags().IntVar(&evalRun.runs, "runs", 1, "repetitions per task, averaged")
	evalRunCmd.Flags().Float64Var(&evalRun.temperature, "temperature", 0,
		"sampling temperature; 0 is the board's deterministic setting")
	evalRunCmd.Flags().BoolVar(&evalRun.live, "live", false,
		"execute every tool for real in the run directory against the sandbox the environment points at; the result "+
			"is a factory run and never a board row")
	evalRunCmd.Flags().BoolVar(&evalRun.transcriptsJSON, "transcripts-json", false,
		"also write each run's transcript as JSON next to the text log: every message with its reasoning, tool calls "+
			"and results, the calls block and the score")
	evalRunCmd.Flags().DurationVar(&evalRun.timeout, "timeout", 10*time.Minute, "wall-clock limit per task")
	evalRunCmd.Flags().StringVar(&evalRun.out, "out", "docs/evals/results", "results directory")
	evalRunCmd.Flags().StringVar(&evalRun.runsDir, "runs-dir", "evals/runs", "transcripts directory (\"\" = none)")
	evalRunCmd.Flags().StringVar(&evalRun.hostLabel, "host-label", "lab",
		"label for the results' host field (never a host name)")
	evalRunCmd.Flags().StringVar(&evalRun.hardware, "hardware", "",
		"label for the machine the engine runs on, at most 60 characters, e.g. \"NVIDIA RTX 5090 (32 GB)\" "+
			"(never a host name or an address)")
	evalRunCmd.Flags().StringVar(&evalRun.override, "override", "",
		"the published override file the model runs from (docs/evals/overrides/<name>.registry.yaml), recorded in "+
			"the results so the board can mark the row; a registry-served model without one is not ranked")
	evalRunCmd.Flags().StringVar(&evalRun.gpuProbe, "gpu-probe", "",
		"a shell command that prints the GPU memory in use in MiB, run once after the model is loaded when the "+
			"engine only estimates its memory, e.g. "+
			"\"nvidia-smi --query-compute-apps=used_memory --format=csv,noheader,nounits\"; an engine that "+
			"measures it needs none. The run unloads its model when it ends")
	evalRecordCmd.Flags().String("corpus", "evals/tasks", "task directory")
	evalRecordCmd.Flags().String("scenarios", "evals/scenarios", "scenario directory")
	evalRecordCmd.Flags().String("tasks", "", "glob over task ids (default: every recorded task)")
	evalRecordCmd.Flags().String("common", "",
		"record the common store of this directory (its calls.yaml) instead of the tasks")
	evalReportCmd.Flags().String("results", "docs/evals/results", "results directory")
	evalReportCmd.Flags().String("out-dir", "docs/evals", "where scoreboard.md and scoreboard.json go")
	evalReportCmd.Flags().String("corpus", "evals/tasks", "task directory (for the task count)")
	evalReportCmd.Flags().Bool("check-defaults", false, "print each tier's default and its top scorer")
	evalLintCmd.Flags().String("corpus", "evals/tasks", "task directory")
	evalLintCmd.Flags().String("results", "",
		"results directory to check too: an override a results file names must be a published one")
	evalLintCmd.Flags().Bool("seal", false, "seal the corpus: write its version and hash to evals/VERSION")
	evalLintCmd.Flags().String("version", "", "the corpus version to seal, greater than the sealed one")
	evalCmd.AddCommand(evalRunCmd, evalRecordCmd, evalReportCmd, evalLintCmd)
}

// corpusOf is the corpus block a run records: the seal's version with the corpus's current hash, which must be the
// sealed one; a corpus without a seal is version 1 with its hash. A corpus whose files cannot all be read hashes to
// nothing and runs on: the run itself reports the unreadable fixture as the corpus defect it is.
func corpusOf(tasksRoot string, out io.Writer) (*evals.Corpus, error) {
	seal, err := evals.ReadSeal(tasksRoot)
	if err != nil {
		return nil, err
	}
	hash, err := evals.CorpusHash(tasksRoot)
	if err != nil {
		_, _ = fmt.Fprintf(out, "warning: the corpus could not be hashed, the results carry no hash: %v\n", err)
		return &evals.Corpus{Version: seal.Version}, nil
	}
	if seal.Hash != "" && seal.Hash != hash {
		return nil, fmt.Errorf("the corpus changed since it was sealed as version %s: bump the version and run "+
			"taracode eval lint --seal --version <n> before a run", seal.Version)
	}
	if seal.Hash == "" {
		_, _ = fmt.Fprintf(out, "warning: the corpus is not sealed (no hash in evals/VERSION): the results say corpus 1; "+
			"run taracode eval lint --seal --version <n> before a board run\n")
	}
	return &evals.Corpus{Version: seal.Version, Hash: hash}, nil
}

// runEvalRun is the testable core of `eval run`.
func runEvalRun(f evalRunFlags, out io.Writer) error {
	if f.model == "" {
		return errors.New("set --model: evals run one named model at a time")
	}
	if _, ok := llm.ParseThink(f.think); !ok {
		return fmt.Errorf("--think %q not recognized; use one of: auto, off, on, low, medium, high", f.think)
	}
	hardware, err := evals.CleanHardware(f.hardware)
	if err != nil {
		return fmt.Errorf("--hardware: %w", err)
	}
	if f.override != "" {
		if err := evals.CheckOverride(f.override); err != nil {
			return fmt.Errorf("--override: %w", err)
		}
	}
	tasks, err := evals.LoadCorpus(f.corpus, f.tasks)
	if err != nil {
		return fmt.Errorf("loading the corpus from %s: %w", f.corpus, err)
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no task under %s matches %q", f.corpus, f.tasks)
	}
	corpus, err := corpusOf(f.corpus, out)
	if err != nil {
		return err
	}
	res, err := evals.Run(context.Background(), tasks, evals.RunOptions{Host: f.host, APIKey: f.apiKey, Vendor: f.vendor,
		Model: f.model, Think: f.think, Runs: f.runs, Timeout: f.timeout, HostLabel: f.hostLabel, Hardware: hardware,
		GPUProbe: f.gpuProbe, Version: Version, RunsDir: f.runsDir, CommonDir: evals.CommonDir(f.corpus), Corpus: corpus,
		Override: f.override, Temperature: f.temperature, Live: f.live, JSONTranscripts: f.transcriptsJSON, Out: out})
	if err != nil {
		return reportStoppedRun(f, tasks, res, err, out)
	}
	switch {
	case res.Engine != nil && res.Engine.Origin == "registry" && f.override == "":
		_, _ = fmt.Fprintf(out, "warning: the engine serves %s from a registry file and no --override names its "+
			"published copy: the row will not rank\n", f.model)
	case res.Engine != nil && res.Engine.Origin == "":
		_, _ = fmt.Fprintf(out, "warning: the engine named no origin for %s: the row will not rank (taraengine 0.5.0 "+
			"names one)\n", f.model)
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
// <runsDir>/<model>-<server>[-<machine>]-<date>-partial.json (evals.ResultsName plus the marker), mirroring
// evals.WriteResults' file shape.
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
	return evals.ResultsName(res) + "-partial.json"
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
	if err := evals.CheckCorpusVersions(all); err != nil {
		return err
	}
	sb := evals.BuildScoreboard(all, count, Version)
	for _, board := range sb.Boards { // a board on which nothing ranks is not written: the results need a re-run first
		if len(board.Rows) == 0 && len(board.Unpublished) > 0 {
			return fmt.Errorf("board %s: none of its %d rows ranks (%s); re-run them on taraengine 0.5.0 or later, "+
				"with --override for a model served from a registry file, or move them out of %s before the board is written",
				board.Hardware, len(board.Unpublished)+len(board.DidNotFit)+len(board.Partial), unrankedReasons(board), results)
		}
	}
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
	_, _ = fmt.Fprintf(out, "scoreboard: %d row(s) in %d tier(s), %d result file(s) left out\n",
		countRows(sb), len(sb.Tiers), len(sb.Skipped))
	for _, s := range sb.Skipped {
		_, _ = fmt.Fprintf(out, "  left out: %s\n", s)
	}
	for _, board := range sb.Boards {
		_, _ = fmt.Fprintf(out, "board %s: %d ranked, %d did not fit, %d on part of the corpus, %d not ranked\n",
			board.Hardware, len(board.Rows), len(board.DidNotFit), len(board.Partial), len(board.Unpublished))
	}
	if checkDefaults {
		for _, line := range sb.CheckDefaults() {
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

// unrankedReasons lists why a board's rows do not rank, each reason once, in order of first appearance.
func unrankedReasons(board evals.HardwareBoard) string {
	var reasons []string
	seen := map[string]bool{}
	for _, r := range board.Unpublished {
		if !seen[r.Unranked] {
			seen[r.Unranked] = true
			reasons = append(reasons, r.Unranked)
		}
	}
	return strings.Join(reasons, ", ")
}

// runEvalSeal is `eval lint --seal --version <n>`: the corpus's hash under a new version.
func runEvalSeal(corpus, version string, out io.Writer) error {
	if version == "" {
		return errors.New("--seal needs --version <n>, a whole number greater than the sealed version")
	}
	c, err := evals.Seal(corpus, version)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "sealed: corpus version %s, hash %s\n", c.Version, c.Hash)
	return nil
}

// runEvalLint prints the problems and fails when there are any.
func runEvalLint(corpus, results string, out io.Writer) error {
	count, problems := evals.Lint(corpus)
	if results != "" {
		problems = append(problems, evals.LintResults(results)...)
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(out, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d task(s), %d problem(s)", count, len(problems))
	}
	_, _ = fmt.Fprintf(out, "ok: %d task(s)\n", count)
	return nil
}
