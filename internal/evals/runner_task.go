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

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/ui"
)

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
