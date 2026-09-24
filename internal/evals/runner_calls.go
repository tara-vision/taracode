package evals

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/tara-vision/taracode/internal/agent"
)

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
