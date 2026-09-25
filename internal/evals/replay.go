package evals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/tara-vision/taracode/internal/tools"
)

// ErrNoFixture is the error, wrapped with the call's signature, of a call no fixture was recorded
// for: a miss (ruling P3-R58).
var ErrNoFixture = errors.New("no recorded data for this call")

// Replay serves tool calls from a task's fixtures (spec 5.3). Only the five file tools ever execute
// for real, inside the run directory and confined to it, reads included, symlinks resolved. Every
// other tool and every dry run replays from the fixture store; a call with no fixture is a tool
// error and a counted miss, and an indexed fixture whose file cannot be read is a distinct, counted
// corpus defect rather than a miss. The errors the model sees never name the task (ruling P3-R58):
// a model quoted the task id back as the policy that blocked it, and task ids share words with
// their own answer patterns.
type Replay struct {
	store  *Store
	runDir string

	mu      sync.Mutex
	calls   []ReplayCall
	planned map[string]bool // terraform dir tokens with a replayed, successful plan (ruling P3-R6, P3-R26)
}

// ReplayCall is one call the middleware saw.
type ReplayCall struct {
	Signature string
	DryRun    bool
	Miss      bool  // no fixture was recorded for this signature
	Defect    bool  // the signature was indexed but its fixture file could not be read
	Err       error // the error the replay returned, nil when it served an output; wraps ErrNoFixture on a miss
}

// realTools are the five file tools: they run for real inside the run directory and are confined
// to it, reads included (ruling P3-R24). Every other tool replays from the fixture store.
var realTools = map[string]bool{"read_file": true, "list_files": true, "search_files": true, "write_file": true,
	"edit_file": true}

// NewReplay returns a replay for one task run in runDir.
func NewReplay(store *Store, runDir string) *Replay {
	return &Replay{store: store, runDir: runDir, planned: map[string]bool{}}
}

// Middleware is the tools.Middleware of this replay.
func (r *Replay) Middleware(call tools.Call, next tools.Executor) tools.Executor {
	if realTools[call.Tool] && !call.DryRun {
		return r.confined(call.Tool, next)
	}
	return r.replay(call)
}

// replay serves one call from the fixture store. The terraform apply gate runs before the lookup (a
// refusal is not a miss and never touches the store), and a successful, non-error plan updates the
// gate's state after the lookup; both are keyed on the canonical signature, so a shell line aliased
// to terraform (ruling P3-R11) shares state with the dedicated tool in both directions (ruling
// P3-R26).
func (r *Replay) replay(call tools.Call) tools.Executor {
	return func(_ context.Context, args map[string]any, _ string) (string, error) {
		sig := Signature(call.Tool, args)
		if call.DryRun {
			sig = DryRunSignature(call.Tool, args)
		}
		if msg, refused := r.terraformApplyGate(sig); refused {
			err := errors.New(msg)
			r.note(ReplayCall{Signature: sig, DryRun: call.DryRun, Err: err})
			return "", err
		}
		out, isErr, ok := r.store.Lookup(sig)
		if !ok {
			if r.store.LookupErr(sig) != nil { // the file and its path stay out of what the model sees
				err := fmt.Errorf("corpus defect: the recorded data for this call cannot be read: %s", sig)
				r.note(ReplayCall{Signature: sig, DryRun: call.DryRun, Defect: true, Err: err})
				return "", err
			}
			err := fmt.Errorf("%w: %s", ErrNoFixture, sig)
			r.note(ReplayCall{Signature: sig, DryRun: call.DryRun, Miss: true, Err: err})
			return "", err
		}
		r.recordTerraformPlan(sig, call.DryRun, isErr)
		if isErr {
			err := errors.New(out)
			r.note(ReplayCall{Signature: sig, DryRun: call.DryRun, Err: err})
			return "", err
		}
		r.note(ReplayCall{Signature: sig, DryRun: call.DryRun})
		return out, nil
	}
}

// confined refuses a call whose path leaves the run directory, textually and, after that check
// passes, by resolving symlinks along its deepest existing ancestor (ruling P3-R25); a purely
// textual check misses a symlink inside the run directory that points outside it. Every one of the
// five file tools with a path argument goes through this, reads included (ruling P3-R24).
func (r *Replay) confined(tool string, next tools.Executor) tools.Executor {
	return func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
		p := str(args, "path")
		if !filepath.IsAbs(p) {
			p = filepath.Join(workingDir, p)
		}
		p = filepath.Clean(p)
		root := filepath.Clean(r.runDir)
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return "", r.confinementError(tool, p)
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", fmt.Errorf("resolving the run directory: %w", err)
		}
		realPath, err := evalDeepestAncestor(p)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", p, err)
		}
		if realPath != realRoot && !strings.HasPrefix(realPath, realRoot+string(filepath.Separator)) {
			return "", r.confinementError(tool, p)
		}
		return next(ctx, args, workingDir)
	}
}

// confinementError is the neutral refusal a confined tool gets for a path outside the run directory.
func (r *Replay) confinementError(tool, path string) error {
	return fmt.Errorf("%s: %s is outside the eval's working directory", tool, path)
}

// evalDeepestAncestor resolves symlinks along p, walking up to the deepest existing ancestor when p
// itself does not exist yet (as for write_file creating a new file), and returns that ancestor's
// real, symlink-free path. Resolving both the run directory and p this way also makes a difference
// like macOS's /var vs /private/var a non-issue, since both sides of the comparison go through it.
//
// Climbing is only for a component that truly does not exist yet: os.Lstat says so. When Lstat finds
// something (a dangling symlink, or one that loops) but EvalSymlinks still fails on it, that is a
// refusal, not a cue to climb past it (ruling P3-R38) - otherwise a dangling link inside the run
// directory would resolve to the run directory's own (perfectly real) parent and pass, while the
// tool's own O_CREATE still follows the link and writes wherever it dangles to.
func evalDeepestAncestor(p string) (string, error) {
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return resolved, nil
		}
		if _, statErr := os.Lstat(p); statErr == nil {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", err
		}
		p = parent
	}
}

// terraformSig parses a canonical terraform signature ("terraform <verb> dir=<dir> ..."), produced
// identically for the terraform tool and for a shell line aliased to it (ruling P3-R11), so the
// plan-state gate below keys on the call's meaning rather than on which tool the model used. A dir
// containing whitespace is quoted by terraformSignature (strconv.Quote) precisely because the whole
// signature is space-joined and later split with strings.Fields; the quoted form is unquoted here
// (ruling P3-R38), so "my infra" and "my other" key as two directories, not both as "my".
func terraformSig(sig string) (verb, dir string, ok bool) {
	rest := strings.TrimPrefix(sig, "dryrun:")
	fields := strings.Fields(rest)
	if len(fields) < 3 || fields[0] != "terraform" || !strings.HasPrefix(fields[2], "dir=") {
		return "", "", false
	}
	verb = fields[1]
	dirField := strings.TrimPrefix(fields[2], "dir=")
	if !strings.HasPrefix(dirField, `"`) {
		return verb, dirField, true
	}
	afterDirEquals := strings.TrimPrefix(rest, "terraform "+verb+" dir=")
	quoted, err := strconv.QuotedPrefix(afterDirEquals)
	if err != nil {
		return "", "", false
	}
	unquoted, err := strconv.Unquote(quoted)
	if err != nil {
		return "", "", false
	}
	return verb, unquoted, true
}

// terraformApplyGate refuses an apply with no plan replayed this session for the same directory. The
// check runs, and consumes the plan, before the fixture lookup, mirroring terraform_tool.go's apply,
// which checks and consumes its plan before running anything (ruling P3-R6).
func (r *Replay) terraformApplyGate(sig string) (string, bool) {
	verb, dir, ok := terraformSig(sig)
	if !ok || verb != "apply" {
		return "", false
	}
	dryRun := strings.HasPrefix(sig, "dryrun:")
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned[dir] {
		if dryRun {
			return "no plan from this session for this directory; run terraform plan first", true
		}
		return fmt.Sprintf("no plan from this session for %s; run terraform plan first", dir), true
	}
	if !dryRun {
		delete(r.planned, dir)
	}
	return "", false
}

// recordTerraformPlan marks dir planned after a real plan whose fixture was found and was not
// itself an error output, mirroring terraform_tool.go's plan(), which only stores a plan record on
// total success: an error plan fixture or a plan miss must not unlock apply (ruling P3-R26).
func (r *Replay) recordTerraformPlan(sig string, dryRun, isErr bool) {
	if dryRun || isErr {
		return
	}
	verb, dir, ok := terraformSig(sig)
	if !ok || verb != "plan" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.planned[dir] = true
}

func (r *Replay) note(c ReplayCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
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

// Defects lists the signatures whose indexed fixture file could not be read, in call order: a
// corpus defect the runner should surface distinctly from an ordinary miss.
func (r *Replay) Defects() []string {
	var out []string
	for _, c := range r.Calls() {
		if c.Defect {
			out = append(out, c.Signature)
		}
	}
	return out
}
