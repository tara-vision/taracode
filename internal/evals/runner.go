package evals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
}

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
	return o
}

// Run runs every task against one model and returns the results. The caller exits non-zero when
// Summary.SafetyFailures is not zero. Run itself fails when the model cannot be used or the kube
// environment cannot be isolated (isolateKubeEnv), and after a task that hit a corpus defect, a
// fixture the repository indexes but the replay cannot read: it then stops, returning the results
// so far with the error, since a broken fixture is a bug in the repository (ruling P3-R39).
func Run(ctx context.Context, tasks []Task, opts RunOptions) (Results, error) {
	opts = withDefaults(opts)
	restore, err := isolateKubeEnv()
	if err != nil {
		return Results{}, fmt.Errorf("isolating the kube environment: %w", err)
	}
	defer restore()
	prov, err := provider.New(ctx, opts.Host, opts.Vendor, opts.APIKey)
	if err != nil {
		return Results{}, err
	}
	client := prov.LLM()
	details, err := client.Show(ctx, opts.Model)
	if err != nil {
		return Results{}, fmt.Errorf("model %s: %w", opts.Model, err)
	}
	if !details.Has("tools") {
		return Results{}, fmt.Errorf("model %s has no tools capability; evals need native tool calls", opts.Model)
	}
	version, _ := client.Version(ctx)
	res := Results{Taracode: opts.Version, Ollama: version, Model: opts.Model, Tier: tierOf(opts.Model), Think: opts.Think,
		Date: opts.Now().Format("2006-01-02"), Runs: opts.Runs, Host: opts.HostLabel}
	_, _ = fmt.Fprintf(opts.Out, "model %s (tier %s), %d tasks, %d run(s), think %s\n",
		opts.Model, res.Tier, len(tasks), opts.Runs, opts.Think)
	if err := warmUp(ctx, opts); err != nil {
		return Results{}, fmt.Errorf("warm-up: %w", err)
	}
	for _, t := range tasks {
		tr := runTaskRepeated(ctx, t, opts)
		res.Tasks = append(res.Tasks, tr)
		flags := ""
		for _, f := range []struct {
			on   bool
			name string
		}{{tr.TimedOut, "TIMED OUT"}, {tr.Truncated, "truncated"}, {tr.SafetyFailure, "SAFETY FAILURE"},
			{tr.Error != "", tr.Error}} {
			if f.on {
				flags += " " + f.name
			}
		}
		_, _ = fmt.Fprintf(opts.Out, "%-32s %s %.2f  iter=%d calls=%d denied=%d misses=%d %dms%s\n",
			tr.ID, passMark3(tr.Pass), tr.Score, tr.Iterations, tr.ToolCalls, tr.Denied, tr.FixtureMisses, tr.WallMs, flags)
		if isCorpusDefect(tr) {
			res.Summary = summarize(tasks, res.Tasks)
			return res, fmt.Errorf("stopped after task %s: %s", tr.ID, tr.Error)
		}
	}
	res.Summary = summarize(tasks, res.Tasks)
	_, _ = fmt.Fprintf(opts.Out, "pass rate %.0f%%, mean score %.2f, misses %.0f%%, safety failures %d\n",
		res.Summary.PassRate*100, res.Summary.MeanScore, res.Summary.FixtureMissRate*100, res.Summary.SafetyFailures)
	return res, nil
}

// kubeEnv are the variables through which the kubectl, helm and shell classifiers find the context
// and namespace a mutation does not name: kubectl config reads KUBECONFIG, and a helm command falls
// back to HELM_KUBECONTEXT and HELM_NAMESPACE (ruling P3-R29).
var kubeEnv = []string{"KUBECONFIG", "HELM_KUBECONTEXT", "HELM_NAMESPACE"}

// isolateKubeEnv makes kube target resolution independent of the host for the duration of a run
// (ruling P3-R29): KUBECONFIG names a file that does not exist inside a fresh temporary directory,
// and HELM_KUBECONTEXT and HELM_NAMESPACE are cleared, so no eval reads the host's kubeconfig. An
// unnamed context or namespace then resolves to "*" on every host, which the policy denies only
// when it protects contexts or namespaces. restore puts the three variables back as they were and
// removes the directory; Run is not concurrent with anything that reads them.
func isolateKubeEnv() (restore func(), err error) {
	dir, err := os.MkdirTemp("", "taracode-eval-kube-*")
	if err != nil {
		return nil, err
	}
	type saved struct {
		value string
		set   bool
	}
	before := make(map[string]saved, len(kubeEnv))
	for _, name := range kubeEnv {
		value, set := os.LookupEnv(name)
		before[name] = saved{value: value, set: set}
	}
	restore = func() {
		for _, name := range kubeEnv {
			if s := before[name]; s.set {
				_ = os.Setenv(name, s.value)
			} else {
				_ = os.Unsetenv(name)
			}
		}
		_ = os.RemoveAll(dir)
	}
	err = os.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig")) // never created
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
	replay := NewReplay(&Store{byKey: map[string]Fixture{}}, t.ID, dir)
	a, err := agent.New(assistantOptions(t, opts, dir, replay, nil, io.Discard))
	if err != nil {
		return err
	}
	// Its own limit: model load time, not the task limit.
	_, _, err = runTurn(ctx, a, "Reply with the single word ready.", 5*time.Minute)
	return err
}

// runTaskRepeated runs a task opts.Runs times and averages the numbers; flags are true when any
// run set them, the error is the first one seen. A run that hits a corpus defect ends the
// repetitions and is the task's result as it is: a broken fixture is not averaged (ruling P3-R39).
func runTaskRepeated(ctx context.Context, t Task, opts RunOptions) TaskResult {
	var runs []TaskResult
	for i := 0; i < opts.Runs; i++ {
		tr := runTask(ctx, t, opts)
		if isCorpusDefect(tr) {
			return tr
		}
		runs = append(runs, tr)
	}
	if len(runs) == 1 {
		return runs[0]
	}
	avg := runs[0]
	n := float64(len(runs))
	sum := func(f func(TaskResult) float64) float64 {
		s := 0.0
		for _, r := range runs {
			s += f(r)
		}
		return round3(s / n)
	}
	avg.Score = sum(func(r TaskResult) float64 { return r.Score })
	avg.Tools = sum(func(r TaskResult) float64 { return r.Tools })
	avg.Answer = sum(func(r TaskResult) float64 { return r.Answer })
	avg.Forbidden = sum(func(r TaskResult) float64 { return r.Forbidden })
	avg.Iterations = int(sum(func(r TaskResult) float64 { return float64(r.Iterations) }) + 0.5)
	avg.ToolCalls = int(sum(func(r TaskResult) float64 { return float64(r.ToolCalls) }) + 0.5)
	avg.WallMs = int64(sum(func(r TaskResult) float64 { return float64(r.WallMs) }))
	avg.Notes = nil
	for _, r := range runs[1:] {
		avg.Denied += r.Denied
		avg.FixtureMisses += r.FixtureMisses
		avg.PromptTokens += r.PromptTokens
		avg.CompletionTokens += r.CompletionTokens
		avg.Truncated = avg.Truncated || r.Truncated
		avg.TimedOut = avg.TimedOut || r.TimedOut
		avg.SafetyFailure = avg.SafetyFailure || r.SafetyFailure
		if avg.Error == "" {
			avg.Error = r.Error
		}
	}
	avg.Pass = avg.Score >= passMark && !avg.SafetyFailure
	return avg
}

// runTask runs one task once in a throwaway directory (spec 7).
func runTask(ctx context.Context, t Task, opts RunOptions) TaskResult {
	tr := TaskResult{ID: t.ID, Area: string(t.Area)}
	runDir, remove, err := makeRunDir("taracode-eval-*")
	if err != nil {
		tr.Error = err.Error()
		return tr
	}
	defer remove()
	if err := prepareRunDir(t, runDir); err != nil {
		tr.Error = err.Error()
		return tr
	}
	store, err := LoadFixtures(t.Dir)
	if err != nil {
		tr.Error = err.Error()
		return tr
	}
	replay := NewReplay(store, t.ID, runDir)
	var mu sync.Mutex
	var events []agent.ToolEvent
	observe := func(e agent.ToolEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	transcript, closeTranscript := transcriptWriter(opts, t.ID)
	defer closeTranscript()
	_, _ = fmt.Fprintf(transcript, "# %s (%s, %s)\n\n%s\n\n", t.ID, t.Area, t.Mode, t.Prompt)
	a, err := agent.New(assistantOptions(t, opts, runDir, replay, observe, transcript))
	if err != nil {
		tr.Error = "assistant: " + err.Error()
		return tr
	}
	answer, stats, turnErr := runTurn(ctx, a, t.Prompt, opts.Timeout)
	mu.Lock()
	seen := append([]agent.ToolEvent(nil), events...)
	mu.Unlock()
	sc := ScoreTask(t, seen, answer)
	switch {
	case turnErr == errTimedOut:
		tr.TimedOut = true
		sc = scoredZero(sc, "timed out: scored 0")
	case turnErr != nil:
		tr.Error = turnErr.Error()
		sc = scoredZero(sc, "turn error: scored 0")
	}
	if defects := replay.Defects(); len(defects) > 0 { // a repository bug, never a model miss (P3-R39)
		tr.Error = corpusDefect(store, defects)
		sc = scoredZero(sc, "corpus defect: scored 0")
	}
	tr.Score, tr.Tools, tr.Answer, tr.Forbidden = sc.Total, sc.Tools, sc.Answer, sc.Forbidden
	tr.Pass, tr.SafetyFailure = sc.Pass, sc.SafetyFailure
	tr.Iterations, tr.ToolCalls, tr.Denied = stats.Completions, stats.ToolCalls, stats.Denied
	tr.PromptTokens, tr.CompletionTokens = stats.PromptTokens, stats.CompletionTokens
	tr.WallMs, tr.Truncated = stats.Wall.Milliseconds(), stats.Truncated
	tr.FixtureMisses = len(replay.Misses())
	tr.Notes = sc.Notes
	_, _ = fmt.Fprintf(transcript,
		"\n## answer\n\n%s\n\n## score %.2f (tools %.2f, answer %.2f, forbidden %.2f) pass=%v\n%s\n",
		answer, sc.Total, sc.Tools, sc.Answer, sc.Forbidden, sc.Pass, strings.Join(sc.Notes, "\n"))
	return tr
}

// scoredZero is the score of a turn that cannot be scored: zero, with only the safety invariant kept
// and a note saying why.
func scoredZero(sc Score, note string) Score {
	return Score{SafetyFailure: sc.SafetyFailure, Notes: append(sc.Notes, note)}
}

// corpusDefectPrefix starts the error of a task that hit a corpus defect: a fixture its index names
// but the replay could not read (ruling P3-R39).
const corpusDefectPrefix = "corpus defect: "

// isCorpusDefect reports whether a task result carries a corpus defect.
func isCorpusDefect(tr TaskResult) bool { return strings.HasPrefix(tr.Error, corpusDefectPrefix) }

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

// errTimedOut marks a turn that exceeded the task limit.
var errTimedOut = fmt.Errorf("the task exceeded its wall-clock limit")

// runTurn runs one ProcessMessage under the limit. A turn that overruns is abandoned: its goroutine
// ends when the loop's own request deadline fires, and its later events are ignored.
func runTurn(
	ctx context.Context, a *agent.Assistant, prompt string, timeout time.Duration,
) (string, agent.TurnStats, error) {
	done := make(chan error, 1)
	go func() { done <- a.ProcessMessage(prompt) }()
	select {
	case err := <-done:
		if err != nil {
			return "", a.LastTurn(), err
		}
		return a.GetLastResponse(), a.LastTurn(), nil
	case <-time.After(timeout):
		return "", agent.TurnStats{}, errTimedOut
	case <-ctx.Done():
		return "", agent.TurnStats{}, ctx.Err()
	}
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
	o.Output = out
	o.ToolMiddleware = replay.Middleware
	o.ToolObserver = observe
	allow := t.Permission == "allow"
	o.PermissionDecider = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		return ui.PermissionChoice{Allowed: allow}
	}
	return o
}

// transcriptWriter opens <RunsDir>/<model slug>-<date>/<task>.log, or discards when RunsDir is "".
func transcriptWriter(opts RunOptions, taskID string) (io.Writer, func()) {
	if opts.RunsDir == "" {
		return io.Discard, func() {}
	}
	dir := filepath.Join(opts.RunsDir, modelSlug(opts.Model)+"-"+opts.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // evals/runs is a git-ignored working directory
		return io.Discard, func() {}
	}
	f, err := os.Create(filepath.Join(dir, taskID+".log")) //nolint:gosec // the corpus task id, validated by LoadTask
	if err != nil {
		return io.Discard, func() {}
	}
	return f, func() { _ = f.Close() }
}
