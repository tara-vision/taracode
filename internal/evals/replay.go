package evals

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tara-vision/taracode/internal/tools"
)

// Replay serves tool calls from a task's fixtures (spec 5.3). The five file tools and get_datetime
// run for real in the run directory, with writes confined to it; every other tool and every dry run
// replays; a call with no fixture is a tool error and a counted miss.
type Replay struct {
	store  *Store
	taskID string
	runDir string

	mu      sync.Mutex
	calls   []ReplayCall
	planned map[string]bool // terraform directories with a replayed plan (ruling R6)
}

// ReplayCall is one call the middleware saw.
type ReplayCall struct {
	Signature string
	DryRun    bool
	Miss      bool
}

// realTools run for real inside the run directory.
var realTools = map[string]bool{"read_file": true, "list_files": true, "search_files": true, "write_file": true,
	"edit_file": true, "get_datetime": true}

// NewReplay returns a replay for one task run.
func NewReplay(store *Store, taskID, runDir string) *Replay {
	return &Replay{store: store, taskID: taskID, runDir: runDir, planned: map[string]bool{}}
}

// Middleware is the tools.Middleware of this replay.
func (r *Replay) Middleware(call tools.Call, next tools.Executor) tools.Executor {
	if realTools[call.Tool] && !call.DryRun {
		if call.Tool == "write_file" || call.Tool == "edit_file" {
			return r.confined(next)
		}
		return next
	}
	return func(_ context.Context, args map[string]any, workingDir string) (string, error) {
		sig := Signature(call.Tool, args)
		if call.DryRun {
			sig = DryRunSignature(call.Tool, args)
		}
		if msg, refused := r.terraformState(call, args, workingDir); refused {
			r.note(sig, call.DryRun, false)
			return "", errors.New(msg)
		}
		out, isErr, ok := r.store.Lookup(sig)
		r.note(sig, call.DryRun, !ok)
		if !ok {
			return "", fmt.Errorf("no recorded data for this call in eval task %s: %s", r.taskID, sig)
		}
		if isErr {
			return "", errors.New(out)
		}
		return out, nil
	}
}

// confined refuses a write whose resolved path leaves the run directory.
func (r *Replay) confined(next tools.Executor) tools.Executor {
	return func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
		p := str(args, "path")
		if !filepath.IsAbs(p) {
			p = filepath.Join(workingDir, p)
		}
		p = filepath.Clean(p)
		root := filepath.Clean(r.runDir)
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return "", fmt.Errorf("eval task %s: refusing to write outside the task directory: %s", r.taskID, p)
		}
		return next(ctx, args, workingDir)
	}
}

// terraformState emulates the one piece of tool state a replay needs: apply demands a plan replayed
// earlier for the same directory, and consumes it, as the terraform tool does (ruling R6).
func (r *Replay) terraformState(call tools.Call, args map[string]any, workingDir string) (string, bool) {
	if call.Tool != "terraform" {
		return "", false
	}
	dir := filepath.Clean(filepath.Join(workingDir, str(args, "dir")))
	r.mu.Lock()
	defer r.mu.Unlock()
	switch str(args, "command") {
	case "plan":
		if !call.DryRun {
			r.planned[dir] = true
		}
	case "apply":
		if !r.planned[dir] {
			if call.DryRun {
				return "no plan from this session for this directory; run terraform plan first", true
			}
			return fmt.Sprintf("no plan from this session for %s; run terraform plan first", dir), true
		}
		if !call.DryRun {
			delete(r.planned, dir)
		}
	}
	return "", false
}

func (r *Replay) note(sig string, dryRun, miss bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, ReplayCall{Signature: sig, DryRun: dryRun, Miss: miss})
}

// Calls lists every replayed call in order.
func (r *Replay) Calls() []ReplayCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ReplayCall(nil), r.calls...)
}

// Misses lists the signatures that had no fixture, in call order.
func (r *Replay) Misses() []string {
	var out []string
	for _, c := range r.Calls() {
		if c.Miss {
			out = append(out, c.Signature)
		}
	}
	return out
}
