package evals

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/models"
	"github.com/tara-vision/taracode/internal/provider"
)

// RunOptions configures Run (spec 7).
type RunOptions struct {
	Host, Model, Vendor, APIKey string
	Think                       string           // auto|off|on|low|medium|high
	Runs                        int              // repetitions per task, default 1
	Timeout                     time.Duration    // per task wall clock, default 10 minutes
	HostLabel                   string           // the results' host field, default "lab"; never a host name
	Version                     string           // the taracode version written to the results
	RunsDir                     string           // where transcripts go; "" = none
	Out                         io.Writer        // progress; nil = io.Discard
	Now                         func() time.Time // nil = time.Now
	NumPredict                  int              // tokens per completion; 0 = evalNumPredict, negative = no cap

	scope *runScope // set by Run for its own duration
}

// runScope is what one Run keeps across its tasks: the real home directory, read before HOME is
// isolated, so the results can be scrubbed of it (ruling P3-R45), and the once-only transcript warning.
type runScope struct {
	home       string
	transcript sync.Once
}

const (
	// engineCallTimeout bounds each check Run makes on the engine before the first task.
	engineCallTimeout = 30 * time.Second
	// joinGrace is how long a cancelled turn may take to return (ruling P3-R47).
	joinGrace = 30 * time.Second
	// warmUpTimeout is the warm-up's own limit: model load time, not the task limit.
	warmUpTimeout = 5 * time.Minute
	// evalNumPredict caps each completion of an eval run, so a runaway completion ends as a cut-off
	// answer rather than as a task timeout (ruling P3-R60); the interactive default stays uncapped.
	evalNumPredict = 4096
)

func withDefaults(o RunOptions) RunOptions {
	if o.Runs <= 0 {
		o.Runs = 1
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Minute
	}
	if o.HostLabel == "" {
		o.HostLabel = "lab"
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Think == "" {
		o.Think = "auto"
	}
	if o.NumPredict == 0 {
		o.NumPredict = evalNumPredict
	}
	return o
}

// Run runs every task against one model and returns the results. The caller exits non-zero when
// Summary.SafetyFailures is not zero. Run itself fails when the environment cannot be isolated, when
// the engine does not serve the model with native tool calls, when the warm-up fails, and after a
// task whose failure is not the model's: a corpus or setup defect (rulings P3-R39, P3-R46), a model
// other than the one asked for (P3-R49), or a turn that did not return when cancelled (P3-R47). It
// then stops, returning the results so far with the error. A cancelled ctx also stops it, with the
// tasks finished before the cancellation and ctx's error.
func Run(ctx context.Context, tasks []Task, opts RunOptions) (Results, error) {
	opts = withDefaults(opts)
	home, _ := os.UserHomeDir()
	opts.scope = &runScope{home: home}
	restore, err := isolateEnv()
	if err != nil {
		return Results{}, fmt.Errorf("isolating the environment: %w", err)
	}
	defer restore()
	res, err := checkEngine(ctx, opts)
	if err != nil {
		return Results{}, err
	}
	_, _ = fmt.Fprintf(opts.Out, "model %s (tier %s), %d tasks, %d run(s), think %s\n",
		opts.Model, res.Tier, len(tasks), opts.Runs, opts.Think)
	if err := warmUp(ctx, opts); err != nil {
		return Results{}, fmt.Errorf("warm-up: %w", err)
	}
	for _, t := range tasks {
		if err := ctx.Err(); err != nil {
			return withSummary(res, tasks), err
		}
		run := runTaskRepeated(ctx, t, opts)
		if err := ctx.Err(); err != nil {
			return withSummary(res, tasks), err // the interrupted task is not recorded
		}
		res.Tasks = append(res.Tasks, run.row)
		printRow(opts.Out, run)
		if run.stop != nil {
			return withSummary(res, tasks), fmt.Errorf("stopped after task %s: %w", t.ID, run.stop)
		}
	}
	res = withSummary(res, tasks)
	_, _ = fmt.Fprintf(opts.Out, "pass rate %.0f%%, mean score %.2f, misses %.0f%%, safety failures %d\n",
		res.Summary.PassRate*100, res.Summary.MeanScore, res.Summary.FixtureMissRate*100, res.Summary.SafetyFailures)
	return res, nil
}

// withSummary is res with the summary of the tasks it holds.
func withSummary(res Results, tasks []Task) Results {
	res.Summary = summarize(tasks, res.Tasks)
	return res
}

// checkEngine verifies that the engine serves the model with native tool calls, each call bounded by
// engineCallTimeout, and returns the results header.
func checkEngine(ctx context.Context, opts RunOptions) (Results, error) {
	prov, err := provider.New(ctx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return Results{}, err
	}
	client := prov.LLM()
	showCtx, cancelShow := context.WithTimeout(ctx, engineCallTimeout)
	details, err := client.Show(showCtx, opts.Model)
	cancelShow()
	if err != nil {
		return Results{}, fmt.Errorf("model %s: %w", opts.Model, err)
	}
	if !details.Has("tools") {
		return Results{}, fmt.Errorf("model %s has no tools capability; evals need native tool calls", opts.Model)
	}
	versionCtx, cancelVersion := context.WithTimeout(ctx, engineCallTimeout)
	version, _ := client.Version(versionCtx)
	cancelVersion()
	return Results{Taracode: opts.Version, Ollama: version, Model: opts.Model, Tier: tierOf(opts.Model),
		Think: opts.Think, Temperature: 0, // every request sends temperature 0 (TemperatureZero)
		Date: opts.Now().Format("2006-01-02"), Runs: opts.Runs, Host: opts.HostLabel}, nil
}

func passMark3(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}

// tierOf is the registry tier of a model, or "other".
func tierOf(model string) string {
	reg, err := models.Load()
	if err != nil {
		return "other"
	}
	if e, ok := reg.Find(model); ok && e.Name == model {
		return string(e.Tier)
	}
	return "other"
}

// warmUp sends one unscored prompt so the model is loaded before the first task is timed.
func warmUp(ctx context.Context, opts RunOptions) error {
	dir, remove, err := makeRunDir("taracode-eval-warmup-*")
	if err != nil {
		return err
	}
	defer remove()
	t := Task{ID: "warm-up", Mode: "investigate", Permission: "allow", Expect: Expect{MaxIterations: 1}}
	replay := NewReplay(&Store{byKey: map[string]Fixture{}}, dir)
	a, err := agent.New(assistantOptions(t, opts, dir, replay, nil, io.Discard))
	if err != nil {
		return err
	}
	if err := checkAssistant(a, t, opts); err != nil {
		return err
	}
	turn, err := runTurn(ctx, a, "Reply with the single word ready.", warmUpTimeout)
	if err != nil {
		return err
	}
	return turn.err
}

// taskRun is one task's outcome for Run: the row the results carry, the raw error for the terminal
// (the row carries it scrubbed, ruling P3-R45), and stop, set when the run must not go on after this
// task because its failure is not the model's.
type taskRun struct {
	row  TaskResult
	raw  string
	stop error
}

// printRow prints a task's line on the terminal, with the raw error.
func printRow(out io.Writer, run taskRun) {
	tr := run.row
	errText := run.raw
	if errText == "" {
		errText = tr.Error
	}
	flags := ""
	for _, f := range []struct {
		on   bool
		name string
	}{{tr.TimedOut, "TIMED OUT"}, {tr.Truncated, "truncated"}, {tr.SafetyFailure, "SAFETY FAILURE"},
		{errText != "", errText}} {
		if f.on {
			flags += " " + f.name
		}
	}
	_, _ = fmt.Fprintf(out, "%-32s %s %.2f  iter=%d calls=%d denied=%d misses=%d %dms%s\n",
		tr.ID, passMark3(tr.Pass), tr.Score, tr.Iterations, tr.ToolCalls, tr.Denied, tr.FixtureMisses, tr.WallMs, flags)
}

// runTaskRepeated runs a task opts.Runs times and folds the runs into one row (averageRuns). A run
// that must stop the run, or a cancelled ctx, ends the repetitions and is returned as it is.
func runTaskRepeated(ctx context.Context, t Task, opts RunOptions) taskRun {
	var runs []taskRun
	for i := 1; i <= opts.Runs; i++ {
		run := runTask(ctx, t, opts, i)
		if run.stop != nil || ctx.Err() != nil {
			return run
		}
		runs = append(runs, run)
	}
	if len(runs) == 1 {
		return runs[0]
	}
	return averageRuns(runs)
}

// averageRuns folds the runs of one task into one row: the scores and every count are per-run means
// (ruling P3-R48), the counts rounded for display while the row keeps the unrounded means the
// summary's rates come from (ruling P3-R56); a flag is set when any run set it, and the error is the
// first one seen.
func averageRuns(runs []taskRun) taskRun {
	mean := func(f func(TaskResult) float64) float64 {
		s := 0.0
		for _, r := range runs {
			s += f(r.row)
		}
		return s / float64(len(runs))
	}
	count := func(f func(TaskResult) int) int {
		return int(math.Round(mean(func(r TaskResult) float64 { return float64(f(r)) })))
	}
	out := runs[0]
	avg := &out.row
	avg.Score = round3(mean(func(r TaskResult) float64 { return r.Score }))
	avg.Tools = round3(mean(func(r TaskResult) float64 { return r.Tools }))
	avg.Answer = round3(mean(func(r TaskResult) float64 { return r.Answer }))
	avg.Forbidden = round3(mean(func(r TaskResult) float64 { return r.Forbidden }))
	avg.Iterations = count(func(r TaskResult) int { return r.Iterations })
	avg.ToolCalls = count(func(r TaskResult) int { return r.ToolCalls })
	avg.Denied = count(func(r TaskResult) int { return r.Denied })
	avg.FixtureMisses = count(func(r TaskResult) int { return r.FixtureMisses })
	avg.PromptTokens = count(func(r TaskResult) int { return r.PromptTokens })
	avg.CompletionTokens = count(func(r TaskResult) int { return r.CompletionTokens })
	avg.WallMs = int64(math.Round(mean(func(r TaskResult) float64 { return float64(r.WallMs) })))
	avg.means = &runMeans{ // unrounded, for the summary's rates (ruling P3-R56)
		iterations:    mean(func(r TaskResult) float64 { return float64(r.Iterations) }),
		toolCalls:     mean(func(r TaskResult) float64 { return float64(r.ToolCalls) }),
		fixtureMisses: mean(func(r TaskResult) float64 { return float64(r.FixtureMisses) }),
	}
	avg.Notes = missNotesOf(runs)
	for _, r := range runs[1:] {
		avg.Truncated = avg.Truncated || r.row.Truncated
		avg.TimedOut = avg.TimedOut || r.row.TimedOut
		avg.SafetyFailure = avg.SafetyFailure || r.row.SafetyFailure
		if avg.Error == "" {
			avg.Error, out.raw = r.row.Error, r.raw
		}
	}
	avg.Pass = avg.Score >= passMark && !avg.SafetyFailure
	return out
}

// missNotesOf is the union of the runs' "fixture miss" notes, in first-seen order: the scorer's other
// notes differ from run to run and are dropped, but a signature to record is one in any run.
func missNotesOf(runs []taskRun) []string {
	seen := map[string]bool{}
	var notes []string
	for _, r := range runs {
		for _, n := range r.row.Notes {
			if strings.HasPrefix(n, fixtureMissNote) && !seen[n] {
				seen[n] = true
				notes = append(notes, n)
			}
		}
	}
	return notes
}
