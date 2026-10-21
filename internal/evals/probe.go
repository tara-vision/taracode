package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// A probe is a read-only, side-effect-free command a model runs to look at its environment before it works: pwd,
// ls, cat of a file it was given. The replay runs a probe for real inside the run directory instead of answering
// it from a fixture (spec 1a), so a careful model pays nothing for looking, and what it sees agrees with the five
// file tools. A program off the list, a refused flag or a path outside the run directory leaves the call to the
// fixture store, where it is the miss it was before.
const (
	// shellProbeTimeout bounds one probe's run.
	shellProbeTimeout = 15 * time.Second
	// shellProbeOutputLimit bounds what a probe hands the model; shellProbeTruncated marks a cut.
	shellProbeOutputLimit = 64 * 1024
	shellProbeTruncated   = "\n... [truncated]"
)

// probeRule says how a program on the probe list is checked: the flags it may not carry, and whether its
// non-flag words are paths confined to the run directory (find: only the start points before its first flag,
// since the words after them are its predicates' arguments).
type probeRule struct {
	refused      []string
	paths        bool
	leadingPaths bool
}

var probePrograms = map[string]probeRule{
	"pwd": {}, "true": {}, "echo": {},
	"ls": {paths: true}, "cat": {paths: true}, "head": {paths: true}, "wc": {paths: true}, "stat": {paths: true},
	"file": {paths: true},
	"tail": {paths: true, refused: []string{"-f", "-F", "--follow"}},
	"find": {leadingPaths: true, refused: []string{"-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint",
		"-fprint0", "-fprintf", "-fls"}},
}

// splitChain parses a shell line into the argv of each simple command it joins with && or ;. ok is false for a
// line with a pipe, ||, a redirect, a background job, a here-document, a substitution, a function definition or
// an empty command: none of those is a probe.
func splitChain(command string) (parts [][]string, ok bool) {
	if strings.TrimSpace(command) == "" || strings.Contains(command, "||") {
		return nil, false
	}
	parsed, err := shellwords.Split(command)
	if err != nil || parsed.Substitution || parsed.FunctionDef || len(parsed.Segments) == 0 {
		return nil, false
	}
	for _, seg := range parsed.Segments {
		if seg.Piped || seg.Background || len(seg.Redirects) > 0 || len(seg.Heredocs) > 0 || len(seg.Words) == 0 {
			return nil, false
		}
		parts = append(parts, seg.Words)
	}
	return parts, true
}

// probeArgvs checks every part of a chain and returns them ready to run, globs expanded; ok is false when any
// part is not a probe, so the whole line goes to the fixture store.
func (r *Replay) probeArgvs(workingDir string, parts [][]string) (argvs [][]string, ok bool) {
	for _, argv := range parts {
		checked, ok := r.probeArgv(workingDir, argv)
		if !ok {
			return nil, false
		}
		argvs = append(argvs, checked)
	}
	return argvs, len(argvs) > 0
}

// probeArgv checks one command: the program on the list, no refused flag, every path word inside the run
// directory. A word with a glob expands inside the run directory; no match keeps the word.
func (r *Replay) probeArgv(workingDir string, argv []string) ([]string, bool) {
	rule, ok := probePrograms[argv[0]]
	if !ok {
		return nil, false
	}
	out := []string{argv[0]}
	pathsEnded, pastDash := false, false
	for _, w := range argv[1:] {
		switch {
		case !pastDash && w == "--":
			pastDash = true
			out = append(out, w)
			continue
		case !pastDash && strings.HasPrefix(w, "-") && w != "-":
			if refusedFlag(rule, w) {
				return nil, false
			}
			pathsEnded = true
			out = append(out, w)
			continue
		}
		isPath := rule.paths || (rule.leadingPaths && !pathsEnded)
		if !isPath {
			out = append(out, w)
			continue
		}
		expanded, ok := r.probePaths(workingDir, w)
		if !ok {
			return nil, false
		}
		out = append(out, expanded...)
	}
	return out, true
}

// refusedFlag reports a flag the rule refuses, written alone or with an attached value (--follow=name).
func refusedFlag(rule probeRule, flag string) bool {
	for _, f := range rule.refused {
		if flag == f || strings.HasPrefix(flag, f+"=") {
			return true
		}
	}
	return false
}

// probePaths is a path word as the probe runs it: a word the shell would expand (~, $) is refused; a glob expands
// inside the run directory, or stays the word when nothing matches; every path resolves inside the run directory.
func (r *Replay) probePaths(workingDir, word string) ([]string, bool) {
	if strings.ContainsAny(word, "~$") {
		return nil, false
	}
	words := []string{word}
	if strings.ContainsAny(word, "*?[") {
		pattern := word
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(workingDir, pattern)
		}
		if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
			words = words[:0]
			for _, m := range matches {
				if rel, err := filepath.Rel(workingDir, m); err == nil && !filepath.IsAbs(word) {
					m = rel
				}
				words = append(words, m)
			}
		}
	}
	for _, w := range words {
		if !r.insideRunDir(workingDir, w) {
			return nil, false
		}
	}
	return words, true
}

// insideRunDir is confined's check for a probe's path: textual, then through the symlinks of its deepest
// existing ancestor.
func (r *Replay) insideRunDir(workingDir, p string) bool {
	if !filepath.IsAbs(p) {
		p = filepath.Join(workingDir, p)
	}
	p = filepath.Clean(p)
	root := filepath.Clean(r.runDir)
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realPath, err := evalDeepestAncestor(p)
	if err != nil {
		return false
	}
	return realPath == realRoot || strings.HasPrefix(realPath, realRoot+string(filepath.Separator))
}

// probeChain runs the parts in order and stops at the first failure, as && does; the outputs are joined.
func (r *Replay) probeChain(ctx context.Context, workingDir string, argvs [][]string) (string, error) {
	var b strings.Builder
	for _, argv := range argvs {
		out, err := r.runProbe(ctx, workingDir, argv)
		b.WriteString(out)
		if err != nil {
			return b.String(), err
		}
	}
	return b.String(), nil
}

// runProbe runs one probe in the run directory with a bare environment and a short deadline. pwd answers the
// working directory without running anything, true answers nothing.
func (r *Replay) runProbe(ctx context.Context, workingDir string, argv []string) (string, error) {
	switch argv[0] {
	case "pwd":
		return workingDir + "\n", nil
	case "true":
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, shellProbeTimeout)
	defer cancel()
	// argv[0] is on the probe list and every path argument is confined to the run directory.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	cmd.Dir = workingDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + workingDir, "LANG=C", "LC_ALL=C"}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := buf.String()
	if len(out) > shellProbeOutputLimit {
		out = out[:shellProbeOutputLimit] + shellProbeTruncated
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out, fmt.Errorf("%s exited with status %d\n%s", argv[0], exit.ExitCode(), strings.TrimSpace(out))
		}
		return out, fmt.Errorf("%s: %w", argv[0], err)
	}
	return out, nil
}

// chain answers a shell line part by part: a part that is a probe runs for real, any other part is looked up in
// the task's store and then the common store, keyed as the simple command it is; the outputs run together as a
// shell shows them, and the chain stops at a part that fails, as && does. handled is false for a single part
// that is not a probe: that line takes the ordinary replay, with its terraform plan gate. A part with no fixture
// makes the whole line a miss, keyed by the whole line, so the results name what to record. The gate and the
// plan state are not applied to the parts of a chain: no recorded task joins terraform calls with &&.
func (r *Replay) chain(ctx context.Context, workingDir, sig string, parts [][]string) (string, ReplayCall, bool) {
	rc := ReplayCall{Signature: sig}
	probes := make([]bool, len(parts))
	anyProbe := false
	for i, argv := range parts {
		if _, ok := r.probeArgv(workingDir, argv); ok {
			probes[i], anyProbe = true, true
		}
	}
	if len(parts) == 1 && !anyProbe {
		return "", rc, false
	}
	var b strings.Builder
	for i, argv := range parts {
		var out string
		var err error
		switch {
		case probes[i]:
			checked, _ := r.probeArgv(workingDir, argv)
			out, err = r.runProbe(ctx, workingDir, checked)
			rc.Real = true
		default:
			partSig := Signature("shell", map[string]any{"command": strings.Join(argv, " ")})
			fixture, isErr, ok, common, defect := r.lookup(partSig)
			switch {
			case defect != nil:
				rc.Defect, rc.Common, rc.Err = true, rc.Common || common, fmt.Errorf(
					"corpus defect: the recorded data for this call cannot be read: %s", partSig)
				return "", rc, true
			case !ok:
				rc.Miss, rc.Err = true, fmt.Errorf("%w: %s", ErrNoFixture, sig)
				return "", rc, true
			}
			rc.Common = rc.Common || common
			out = fixture
			if isErr {
				err = errors.New(fixture)
				out = ""
			}
		}
		b.WriteString(out)
		if err != nil {
			rc.Err = err
			return b.String(), rc, true
		}
	}
	return b.String(), rc, true
}
