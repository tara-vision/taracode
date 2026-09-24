package evals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/models"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
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

// isolatedEnv are the variables Run replaces for its own duration. The kubectl, helm and shell
// classifiers resolve a context or namespace a mutation does not name through KUBECONFIG and the
// helm variables (ruling P3-R29); HOME holds the global policy, the default kubeconfig and what a
// leading ~ expands to (ruling P3-R46).
var isolatedEnv = []string{"KUBECONFIG", "HELM_KUBECONTEXT", "HELM_NAMESPACE", "HOME"}

// isolateEnv makes a run independent of the host: KUBECONFIG names a file that does not exist
// inside a fresh temporary directory, HOME is an empty directory beside it, and HELM_KUBECONTEXT and
// HELM_NAMESPACE are cleared. An unnamed context or namespace then resolves to "*" on every host,
// which the policy denies only when it protects contexts or namespaces, and no eval reads the
// host's global policy or kubeconfig. restore puts the variables back as they were and removes the
// directory; Run is not concurrent with anything that reads them.
func isolateEnv() (restore func(), err error) {
	dir, err := os.MkdirTemp("", "taracode-eval-env-*")
	if err != nil {
		return nil, err
	}
	type saved struct {
		value string
		set   bool
	}
	before := make(map[string]saved, len(isolatedEnv))
	for _, name := range isolatedEnv {
		value, set := os.LookupEnv(name)
		before[name] = saved{value: value, set: set}
	}
	restore = func() {
		for _, name := range isolatedEnv {
			if s := before[name]; s.set {
				_ = os.Setenv(name, s.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
		_ = os.RemoveAll(dir)
	}
	home := filepath.Join(dir, "home")
	err = os.Mkdir(home, 0o700)
	if err == nil {
		err = os.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig")) // never created
	}
	if err == nil {
		err = os.Setenv("HOME", home)
	}
	if err == nil {
		err = os.Unsetenv("HELM_KUBECONTEXT")
	}
	if err == nil {
		err = os.Unsetenv("HELM_NAMESPACE")
	}
	if err != nil {
		restore()
		return nil, err
	}
	return restore, nil
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

// runTask runs one task once in a throwaway directory (spec 7); run numbers the repetition.
func runTask(ctx context.Context, t Task, opts RunOptions, run int) taskRun {
	tr := TaskResult{ID: t.ID, Area: string(t.Area)}
	transcript, closeTranscript := transcriptWriter(opts, t.ID, run)
	defer closeTranscript()
	_, _ = fmt.Fprintf(transcript, "# %s (%s, %s)\n\n%s\n\n", t.ID, t.Area, t.Mode, t.Prompt)
	runDir, remove, err := makeRunDir("taracode-eval-*")
	if err != nil {
		return setupDefect(tr, t, opts, "the run directory", err, transcript)
	}
	defer remove()
	if err := prepareRunDir(t, runDir); err != nil {
		return setupDefect(tr, t, opts, "the run directory", err, transcript)
	}
	store, err := LoadFixtures(t.Dir)
	if err != nil {
		return setupDefect(tr, t, opts, "the fixtures", err, transcript)
	}
	replay := NewReplay(store, runDir)
	events := &eventLog{replay: replay}
	a, err := agent.New(assistantOptions(t, opts, runDir, replay, events.observe, transcript))
	if err != nil {
		return setupDefect(tr, t, opts, "the assistant", err, transcript)
	}
	if err := checkAssistant(a, t, opts); err != nil {
		return setupDefect(tr, t, opts, "the assistant", err, transcript)
	}
	turn, err := runTurn(ctx, a, t.Prompt, opts.Timeout)
	if err != nil { // the turn is still running: nothing else may start next to it
		tr.TimedOut, tr.WallMs, tr.ToolCalls = true, turn.wall.Milliseconds(), len(events.snapshot())
		tr.Error = publicError(err, opts, t)
		_, _ = fmt.Fprintf(transcript, "\n## not joined\n\n%v\n", err)
		writeCalls(transcript, events.calls())
		return taskRun{row: tr, raw: err.Error(), stop: err}
	}
	out := finishTask(t, tr, turn, events.snapshot(), replay, store, opts)
	_, _ = fmt.Fprintf(transcript,
		"\n## answer\n\n%s\n\n## score %.2f (tools %.2f, answer %.2f, forbidden %.2f) pass=%v\n%s\n",
		turn.answer, out.row.Score, out.row.Tools, out.row.Answer, out.row.Forbidden, out.row.Pass,
		strings.Join(out.row.Notes, "\n"))
	if out.raw != "" {
		_, _ = fmt.Fprintf(transcript, "\n## error\n\n%s\n", out.raw)
	}
	writeCalls(transcript, events.calls())
	return out
}

// writeCalls appends the transcript's calls block (ruling P3-R58): one line per decided call with its
// signature, then the denial's reason or the tool's error. A call the fixtures lacked ends with
// "MISS <signature>", the signature the replay actually missed: the call's own, or for a gate dry run
// its dryrun: form (ruling P3-R63), so a pasted line records the right fixture.
func writeCalls(w io.Writer, calls []loggedCall) {
	_, _ = fmt.Fprint(w, "\n## calls\n\n")
	if len(calls) == 0 {
		_, _ = fmt.Fprintln(w, "(no tool calls)")
	}
	for i, c := range calls {
		e := c.event
		decision := "allowed"
		if !e.Allowed {
			decision = "DENIED(" + e.Rule + ")"
		}
		line := fmt.Sprintf("#%d %s %s %s", i+1, e.Tool, decision, Signature(e.Tool, e.Args))
		for _, sig := range c.missedSignatures() {
			line += "  MISS " + sig
		}
		_, _ = fmt.Fprintln(w, line)
		if !e.Allowed && e.Reason != "" {
			_, _ = fmt.Fprintf(w, "  reason: %s\n", e.Reason)
		}
		if e.Err != nil {
			_, _ = fmt.Fprintf(w, "  error: %v\n", e.Err)
		}
	}
}

// missNotes are the "fixture miss: <signature>" notes of a run, one per signature in call order, so
// the results file alone says which signatures to record (ruling P3-R58).
func missNotes(replay *Replay) []string {
	seen := map[string]bool{}
	var notes []string
	for _, sig := range replay.Misses() {
		if !seen[sig] {
			seen[sig] = true
			notes = append(notes, fixtureMissNote+sig)
		}
	}
	return notes
}

// fixtureMissNote starts a note naming a signature the fixtures lacked.
const fixtureMissNote = "fixture miss: "

// finishTask scores a joined turn into the task's row. A timed-out turn, a turn error and a corpus
// defect score 0 and keep only the safety invariant; a corpus defect also stops the run (ruling
// P3-R39). ToolCalls and Denied come from the observed events and WallMs from the runner's clock, so
// a timed-out row still shows what happened before the limit (ruling P3-R47).
func finishTask(
	t Task, tr TaskResult, turn turnResult, events []agent.ToolEvent, replay *Replay, store *Store, opts RunOptions,
) taskRun {
	var out taskRun
	sc := ScoreTask(t, events, turn.answer)
	switch {
	case turn.timedOut:
		tr.TimedOut = true
		sc = scoredZero(sc, "timed out: scored 0")
	case turn.err != nil:
		tr.Error, out.raw = publicError(turn.err, opts, t), turn.err.Error()
		sc = scoredZero(sc, "turn error: scored 0")
	}
	if defects := replay.Defects(); len(defects) > 0 { // a repository bug, never a model miss (P3-R39)
		tr.Error = corpusDefect(store, defects)
		out.raw, out.stop = tr.Error, errors.New(tr.Error)
		sc = scoredZero(sc, "corpus defect: scored 0")
	}
	tr.Score, tr.Tools, tr.Answer, tr.Forbidden = sc.Total, sc.Tools, sc.Answer, sc.Forbidden
	tr.Pass, tr.SafetyFailure = sc.Pass, sc.SafetyFailure
	tr.Iterations, tr.ToolCalls = turn.stats.Completions, len(events)
	for _, e := range events {
		if !e.Allowed {
			tr.Denied++
		}
	}
	tr.PromptTokens, tr.CompletionTokens = turn.stats.PromptTokens, turn.stats.CompletionTokens
	tr.WallMs, tr.Truncated = turn.wall.Milliseconds(), turn.stats.Truncated
	tr.FixtureMisses = len(replay.Misses())
	tr.Notes = publicNotes(append(append([]string(nil), sc.Notes...), missNotes(replay)...), opts, t)
	out.row = tr
	return out
}

// setupDefect is a task whose run directory, fixtures or assistant could not be set up as the task
// asks: a bug in the corpus or the environment, never the model's, so the run stops after it (the
// Task 13 fix round, extending ruling P3-R39). The row carries the scrubbed cause.
func setupDefect(tr TaskResult, t Task, opts RunOptions, what string, err error, transcript io.Writer) taskRun {
	tr.Error = "setup defect: " + what + ": " + publicError(err, opts, t)
	_, _ = fmt.Fprintf(transcript, "\n## setup defect\n\n%s: %v\n", what, err)
	return taskRun{row: tr, raw: fmt.Sprintf("setup defect: %s: %v", what, err),
		stop: fmt.Errorf("setup defect: %s: %w", what, err)}
}

// checkAssistant refuses an assistant that would not run the task as written: a policy that did not
// load (the session is then locked to investigate mode, so a must_deny would pass vacuously), a mode
// other than the task's (ruling P3-R46), or a model other than the one asked for, which the
// assistant falls back to when the engine does not list the name as given (ruling P3-R49).
func checkAssistant(a *agent.Assistant, t Task, opts RunOptions) error {
	if err := a.PolicyError(); err != nil {
		return fmt.Errorf("the policy did not load: %w", err)
	}
	if a.Mode() != policy.Mode(t.Mode) {
		return fmt.Errorf("the assistant runs in %s mode, the task wants %s", a.Mode(), t.Mode)
	}
	if got := a.GetCurrentModel(); got != opts.Model {
		return fmt.Errorf("the engine serves %s instead of %s; pass the model name exactly as the engine lists it",
			got, opts.Model)
	}
	return nil
}

// eventLog collects the observer's events, each with the replay calls made for it. The observer
// runs on the turn's goroutine right after a call's gates and execution, so the replay calls
// recorded since the previous event are this event's own (a mutation's dry run and its execution).
// The registry rebuilds every tool error as a fresh string, so ToolEvent.Err cannot carry
// ErrNoFixture; the replay's own record of each call still does.
type eventLog struct {
	replay  *Replay // nil = no replay calls to attribute
	mu      sync.Mutex
	entries []loggedCall
	seen    int // the replay calls already attributed to an event
}

// loggedCall is one decided tool call and the replay calls it made.
type loggedCall struct {
	event    agent.ToolEvent
	replayed []ReplayCall
}

// missedSignatures are the signatures of the call's replay records that had no fixture.
func (c loggedCall) missedSignatures() []string {
	var sigs []string
	for _, r := range c.replayed {
		if errors.Is(r.Err, ErrNoFixture) {
			sigs = append(sigs, r.Signature)
		}
	}
	return sigs
}

func (l *eventLog) observe(e agent.ToolEvent) {
	var replayed []ReplayCall
	if l.replay != nil {
		replayed = l.replay.Calls()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := loggedCall{event: e}
	if l.seen < len(replayed) {
		entry.replayed = replayed[l.seen:]
		l.seen = len(replayed)
	}
	l.entries = append(l.entries, entry)
}

func (l *eventLog) snapshot() []agent.ToolEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := make([]agent.ToolEvent, len(l.entries))
	for i, c := range l.entries {
		events[i] = c.event
	}
	return events
}

func (l *eventLog) calls() []loggedCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]loggedCall(nil), l.entries...)
}

// scoredZero is the score of a turn that cannot be scored: zero, with only the safety invariant kept
// and a note saying why.
func scoredZero(sc Score, note string) Score {
	return Score{SafetyFailure: sc.SafetyFailure, Notes: append(sc.Notes, note)}
}

// corpusDefectPrefix starts the error of a task that hit a corpus defect: a fixture its index names
// but the replay could not read (ruling P3-R39).
const corpusDefectPrefix = "corpus defect: "

// corpusDefect is the task error for the signatures whose indexed fixture could not be read: each
// signature with its file under the task's fixtures directory and the cause. The host path of the
// file is left out, since the error is written into the results.
func corpusDefect(store *Store, sigs []string) string {
	files := map[string]string{}
	for _, f := range store.Fixtures() {
		files[f.Signature] = f.File
	}
	seen := map[string]bool{}
	var parts []string
	for _, sig := range sigs {
		if seen[sig] {
			continue
		}
		seen[sig] = true
		cause := "unreadable"
		var pathErr *fs.PathError
		if errors.As(store.LookupErr(sig), &pathErr) {
			cause = pathErr.Err.Error()
		}
		parts = append(parts, fmt.Sprintf("fixtures/%s for %q (%s)", files[sig], sig, cause))
	}
	return corpusDefectPrefix + strings.Join(parts, "; ")
}

// turnResult is a joined turn: the answer, the loop's statistics read after the join, the turn's
// own error, whether the task limit ended it, and its wall time on the runner's clock.
type turnResult struct {
	answer   string
	stats    agent.TurnStats
	err      error
	timedOut bool
	wall     time.Duration
}

// runTurn runs one turn under the limit through joinTurn and reads the assistant only once the turn
// has returned. The error is joinTurn's: the turn did not return, and the run must stop.
func runTurn(ctx context.Context, a *agent.Assistant, prompt string, timeout time.Duration) (turnResult, error) {
	turn, err := joinTurn(ctx, timeout, joinGrace, func(turnCtx context.Context) error {
		return a.ProcessMessageContext(turnCtx, prompt)
	})
	if err != nil {
		return turn, err
	}
	turn.stats = a.LastTurn()
	if turn.err == nil {
		turn.answer = a.GetLastResponse()
	}
	return turn, nil
}

// joinTurn runs turn under a context that ends with the limit or with ctx, and waits for it to
// return (ruling P3-R47). When that context ends first, the turn is cancelled and has grace to
// return; a turn that does not is an error, and since its goroutine may still call the engine and
// the gate, nothing may run next to it. A turn the limit ended counts as timed out; wall is the
// runner's own clock, from the start to the join.
func joinTurn(ctx context.Context, timeout, grace time.Duration, turn func(context.Context) error) (turnResult, error) {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- turn(turnCtx) }()
	var res turnResult
	select {
	case res.err = <-done:
	case <-turnCtx.Done():
		cancel()
		select {
		case res.err = <-done:
		case <-time.After(grace):
			res.wall = time.Since(start)
			return res, fmt.Errorf("the turn did not return within %s of its cancellation", grace)
		}
	}
	res.wall = time.Since(start)
	res.timedOut = res.err != nil && errors.Is(turnCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	return res, nil
}

// makeRunDir creates a temporary directory and returns its symlink-free path, the path a run then
// uses everywhere: the working directory, the policy file and the replay (ruling P3-R39). The
// replay's confinement check and the file tools then see the same path (macOS's /var is a link to
// /private/var), and a symlinked temporary root cannot make list_files walk nothing. remove deletes
// the directory.
func makeRunDir(pattern string) (dir string, remove func(), err error) {
	tmp, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	remove = func() { _ = os.RemoveAll(tmp) }
	if dir, err = filepath.EvalSymlinks(tmp); err != nil {
		remove()
		return "", nil, err
	}
	return dir, remove, nil
}

// prepareRunDir copies the task's workdir and, for an operate task, creates the project storage
// and writes the task's policy (ruling R5). The run directory holds only what copyDir copies and
// that policy file, never a link. Only a missing workdir root means the task has no workdir; any
// other failure, including one deeper in the copy, fails the task.
func prepareRunDir(t Task, runDir string) error {
	workdir := filepath.Join(t.Dir, "workdir")
	if _, err := os.Lstat(workdir); err == nil {
		if err := copyDir(workdir, runDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if t.Mode != "operate" {
		return nil
	}
	if _, err := storage.NewManager(runDir); err != nil {
		return err
	}
	if t.Policy == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(t.Dir, t.Policy))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, ".taracode", "policy.yaml"), data, 0o644) //nolint:gosec // throwaway dir
}

// assistantOptions builds the headless assistant for one task (spec 7).
func assistantOptions(
	t Task, opts RunOptions, runDir string, replay *Replay, observe func(agent.ToolEvent), out io.Writer,
) agent.Options {
	o := agent.DefaultOptions()
	o.Host, o.APIKey, o.Model, o.Vendor = opts.Host, opts.APIKey, opts.Model, opts.Vendor
	o.Streaming, o.Spinner = false, false
	o.WorkingDir = runDir
	o.Ephemeral = t.Mode != "operate"
	o.Mode = policy.Mode(t.Mode)
	o.Offline = true
	o.Think = opts.Think
	o.MaxIterations = t.Expect.MaxIterations
	o.MemoryEnabled = false
	o.PreviewEdits = false // the preview is a terminal prompt
	o.Generation.TemperatureZero = true
	if opts.NumPredict > 0 {
		o.Generation.NumPredict = opts.NumPredict
	}
	o.Output = out
	o.ToolMiddleware = replay.Middleware
	o.ToolObserver = observe
	allow := t.Permission == "allow"
	o.PermissionDecider = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		return ui.PermissionChoice{Allowed: allow}
	}
	return o
}

// transcriptWriter opens <RunsDir>/<model slug>-<date>/<task>.log, <task>.run<N>.log when a task
// runs more than once, or discards when RunsDir is "". A transcript that cannot be opened is
// reported once per run.
func transcriptWriter(opts RunOptions, taskID string, run int) (io.Writer, func()) {
	if opts.RunsDir == "" {
		return io.Discard, func() {}
	}
	name := taskID + ".log"
	if opts.Runs > 1 {
		name = fmt.Sprintf("%s.run%d.log", taskID, run)
	}
	dir := filepath.Join(opts.RunsDir, modelSlug(opts.Model)+"-"+opts.Now().Format("2006-01-02"))
	f, err := createTranscript(dir, name)
	if err != nil {
		warn := func() { _, _ = fmt.Fprintf(opts.Out, "warning: transcripts are not written: %v\n", err) }
		if opts.scope == nil {
			warn()
		} else {
			opts.scope.transcript.Do(warn)
		}
		return io.Discard, func() {}
	}
	return f, func() { _ = f.Close() }
}

func createTranscript(dir, name string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // evals/runs is a git-ignored working directory
		return nil, err
	}
	return os.Create(filepath.Join(dir, name)) //nolint:gosec // the corpus task id, validated by LoadTask
}
