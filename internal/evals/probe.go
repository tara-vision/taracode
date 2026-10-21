package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// A probe is a read-only, side-effect-free command a model runs to look at its environment before it works: pwd,
// ls, cat of a file it was given. The replay runs a probe for real inside the run directory instead of answering
// it from a fixture (spec 1a), so a careful model pays nothing for looking, and what it sees agrees with the five
// file tools. The rules fail closed: a program off the list, a flag the list does not name, a word the shell would
// expand, a path with a ".." component or outside the run directory, and a line the product's classifier does not
// call a read all leave the call to the fixture store, where it is the miss it was before.
const (
	// shellProbeOutputLimit bounds what a probe hands the model; shellProbeTruncated marks a cut.
	shellProbeOutputLimit = 64 * 1024
	shellProbeTruncated   = "\n... [truncated]"
)

// shellProbeTimeout bounds one probe's run (a variable so a test can shorten it).
var shellProbeTimeout = 15 * time.Second

// probeRule is what one program on the list may take: the letters of its short flags (they may cluster, as -la),
// its long flags, the flags whose next word is a value that is not a path (head -n 5), whether its other words are
// paths confined to the run directory, and for find its own grammar. Any flag the rule does not name is refused.
type probeRule struct {
	letters string
	long    map[string]bool
	valued  map[string]bool // a flag whose value is the next word, or attached as -n5 / --lines=5
	paths   bool
	find    bool
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var probePrograms = map[string]probeRule{
	"pwd":  {letters: "LP"},
	"true": {},
	"echo": {letters: "neE"},
	"ls": {paths: true, letters: "aAlh1dtrSFpsinogGTucUkmxCqwBbfR",
		long: set("--all", "--almost-all", "--human-readable", "--recursive", "--reverse", "--directory", "--color")},
	"cat": {paths: true, letters: "nbsvetAETu",
		long: set("--number", "--number-nonblank", "--squeeze-blank", "--show-ends", "--show-all", "--show-nonprinting",
			"--show-tabs")},
	"head": {paths: true, letters: "qv", valued: set("-n", "-c", "--lines", "--bytes"),
		long: set("--quiet", "--verbose", "--silent")},
	"tail": {paths: true, letters: "qvr", valued: set("-n", "-c", "--lines", "--bytes"),
		long: set("--quiet", "--verbose", "--silent")},
	"wc":   {paths: true, letters: "lwcmL", long: set("--lines", "--words", "--bytes", "--chars", "--max-line-length")},
	"stat": {paths: true, letters: "xt", valued: set("-c", "-f", "--format", "--printf"), long: set("--terse")},
	"file": {paths: true, letters: "bizhIspk",
		long: set("--brief", "--mime", "--mime-type", "--mime-encoding", "--uncompress", "--no-dereference", "--keep-going")},
	"find": {find: true},
}

// find's grammar: the leading options it may take, the predicates that take a value (a pattern, a type, a
// number, never a path the probe opens) and the ones that take none. Everything else is refused: the options
// that follow symlinks, the predicates that read another file, and every action but printing.
var (
	findLeading = set("-P", "-E")
	findValued  = set("-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename", "-regex", "-iregex", "-type",
		"-maxdepth", "-mindepth", "-size", "-mtime", "-mmin", "-atime", "-amin", "-ctime", "-cmin", "-perm", "-user",
		"-group", "-uid", "-gid", "-links")
	findBare = set("-depth", "-d", "-empty", "-print", "-print0", "-ls", "-prune", "-not", "!", "-a", "-and", "-o", "-or",
		"(", ")", "-true", "-false", "-readable", "-writable", "-executable", "-xdev", "-mount", "-x")
)

// splitChain parses a shell line into the argv of each simple command and the operator before each one after the
// first: "&&", which stops the chain at a failure, or ";", which goes on (a newline counts as ";"). ok is false for
// a line with a pipe, ||, a single &, a redirect, a background job, a here-document, a substitution, a function
// definition or an empty command: none of those is a probe.
func splitChain(command string) (parts [][]string, ops []string, ok bool) {
	if strings.TrimSpace(command) == "" {
		return nil, nil, false
	}
	parsed, err := shellwords.Split(command)
	if err != nil || parsed.Substitution || parsed.FunctionDef || len(parsed.Segments) == 0 {
		return nil, nil, false
	}
	for _, seg := range parsed.Segments {
		if seg.Piped || seg.Background || len(seg.Redirects) > 0 || len(seg.Heredocs) > 0 || len(seg.Words) == 0 {
			return nil, nil, false
		}
		parts = append(parts, seg.Words)
	}
	ops, ok = chainOperators(command)
	if !ok || len(ops) != len(parts)-1 {
		return nil, nil, false
	}
	return parts, ops, true
}

// chainOperators scans a line outside its quotes for the operators between its commands; a pipe, || or a lone &
// makes it no chain of probes.
func chainOperators(command string) (ops []string, ok bool) {
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '&' && i+1 < len(command) && command[i+1] == '&':
			ops = append(ops, "&&")
			i++
		case c == '|' || c == '&':
			return nil, false
		case c == ';' || c == '\n':
			ops = append(ops, ";")
		}
	}
	return ops, quote == 0
}

// probeArgv checks one command against its program's rule and returns it ready to run; ok is false when it is not
// a probe. No word is expanded: a word with $, a leading ~ or a glob character is refused unless it is the value of
// a flag that takes one (find -name '*.tf'); a path has no ".." component and resolves inside the run directory.
func (r *Replay) probeArgv(workingDir string, argv []string) ([]string, bool) {
	rule, ok := probePrograms[argv[0]]
	if !ok {
		return nil, false
	}
	if rule.find {
		return r.probeFind(workingDir, argv)
	}
	out := []string{argv[0]}
	pastDash, wantValue := false, false
	for _, w := range argv[1:] {
		switch {
		case wantValue:
			wantValue = false
		case !pastDash && w == "--":
			pastDash = true
		case !pastDash && strings.HasPrefix(w, "-") && w != "-":
			attached, ok := probeFlag(rule, w)
			if !ok {
				return nil, false
			}
			wantValue = attached
		default:
			if !expandable(w) {
				return nil, false
			}
			if rule.paths && !r.insideRunDir(workingDir, w) {
				return nil, false
			}
		}
		out = append(out, w)
	}
	if wantValue {
		return nil, false
	}
	return out, true
}

// probeFlag checks one flag against the rule: a long flag it names, with or without an attached value when it takes
// one; a short cluster of letters it names, where a valued letter may end the cluster with its value attached
// (-n5) or take the next word (needsValue); a bare number for head and tail (-20). The value of a valued flag is
// never a path. ok is false for any other flag.
func probeFlag(rule probeRule, flag string) (needsValue, ok bool) {
	if strings.HasPrefix(flag, "--") {
		name, value, attached := strings.Cut(flag, "=")
		switch {
		case rule.valued[name]:
			return !attached, !attached || value != ""
		case rule.long[name]:
			return false, !attached || name == "--color"
		}
		return false, false
	}
	body := flag[1:]
	if _, err := strconv.Atoi(body); err == nil && rule.valued["-n"] { // head -20, tail -5
		return false, true
	}
	for i, c := range body {
		letter := "-" + string(c)
		switch {
		case rule.valued[letter]:
			return i == len(body)-1, true // -n5 carries its value; -n takes the next word
		case strings.ContainsRune(rule.letters, c):
		default:
			return false, false
		}
	}
	return false, true
}

// probeFind checks a find line: leading options from findLeading, then start points (paths inside the run
// directory), then predicates from findValued and findBare with their values; anything else is refused, the
// options that follow symlinks and the predicates that read another file among them.
func (r *Replay) probeFind(workingDir string, argv []string) ([]string, bool) {
	out := []string{argv[0]}
	i := 1
	for ; i < len(argv) && strings.HasPrefix(argv[i], "-") && findLeading[argv[i]]; i++ {
		out = append(out, argv[i])
	}
	starts := 0
	for ; i < len(argv) && !strings.HasPrefix(argv[i], "-") && argv[i] != "!" && argv[i] != "("; i++ {
		if !expandable(argv[i]) || !r.insideRunDir(workingDir, argv[i]) {
			return nil, false
		}
		out = append(out, argv[i])
		starts++
	}
	if starts == 0 { // find with no start point searches the working directory; make that explicit and confined
		out = append(out, ".")
	}
	for ; i < len(argv); i++ {
		w := argv[i]
		switch {
		case findValued[w]:
			if i+1 >= len(argv) {
				return nil, false
			}
			out = append(out, w, argv[i+1])
			i++
		case findBare[w]:
			out = append(out, w)
		default:
			return nil, false
		}
	}
	return out, true
}

// expandable reports a word the probe may pass as it is: nothing the shell would expand.
func expandable(w string) bool {
	return w != "" && !strings.ContainsAny(w, "$*?[") && !strings.HasPrefix(w, "~")
}

// insideRunDir is confined's check for a probe's path: no ".." component, textual containment, then the symlinks
// of its deepest existing ancestor.
func (r *Replay) insideRunDir(workingDir, p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return false
		}
	}
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

// shellReads reports whether the product's own classifier calls a whole shell line a read: the backstop behind
// the list, so a line it would gate as a mutation is never run as a probe.
var shellReads = func() func(command, workingDir string) bool {
	var once sync.Once
	var reg *tools.Registry
	return func(command, workingDir string) bool {
		once.Do(func() { reg = tools.NewBuiltinRegistry(tools.Options{}, tools.Config{}) })
		inv, err := reg.Classify("shell", map[string]any{"command": command}, workingDir)
		return err == nil && inv.Classification == policy.Read
	}
}()

// runProbe runs one probe in the run directory with a bare environment and a short deadline, and shapes the
// result as the shell tool does: the output without its trailing newlines, "(no output)" for none, a non-zero
// exit as "[exit status N]" on its own line, a timeout as the error the shell tool returns. pwd answers the
// working directory without running anything, true answers nothing.
func (r *Replay) runProbe(ctx context.Context, workingDir string, argv []string) (text string, status int, err error) {
	switch argv[0] {
	case "pwd":
		return workingDir, 0, nil
	case "true":
		return "", 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, shellProbeTimeout)
	defer cancel()
	// argv[0] is on the probe list, every flag is one its rule names and every path is confined to the run directory.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	cmd.Dir = workingDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + workingDir, "LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	buf := &cappedBuffer{limit: shellProbeOutputLimit}
	cmd.Stdout, cmd.Stderr = buf, buf
	runErr := cmd.Run()
	out := strings.TrimRight(buf.String(), "\n")
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, -1, fmt.Errorf("command timed out after %s", shellProbeTimeout)
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return out, exit.ExitCode(), nil
	}
	if runErr != nil {
		return out, -1, fmt.Errorf("%s: %w", argv[0], runErr)
	}
	return out, 0, nil
}

// cappedBuffer keeps the first limit bytes a probe writes and marks the cut.
type cappedBuffer struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p) // the whole write is taken, so the copy from the process never ends on a short write
	if room := b.limit - b.buf.Len(); room > 0 {
		if n > room {
			p = p[:room]
			b.truncated = true
		}
		b.buf.Write(p)
	} else if n > 0 {
		b.truncated = true
	}
	return n, nil
}

func (b *cappedBuffer) String() string {
	if b.truncated {
		return b.buf.String() + shellProbeTruncated
	}
	return b.buf.String()
}

// chainPart is one command of a chain: a probe to run, or a common-store fixture already read.
type chainPart struct {
	argv    []string // a probe, when text is unused
	text    string   // a common-store fixture's text, as recorded (trimmed, "[exit status N]" at its end on a failure)
	fixture bool
}

// chain answers a shell line of several commands joined by && or ;, when every command is a probe or a
// tool-presence probe the common store holds (spec 1b): nothing from a task's own store, whose fixtures the
// scorer and the terraform plan gate read on whole lines. handled is false for any other chain, which stays the
// whole-line miss it was. The parts are checked before any runs; then they run in order as a shell would, && stopping
// at a failure and ; going on, the outputs run together and the line's exit status, the last command's, closing it.
func (r *Replay) chain(
	ctx context.Context, workingDir, command, sig string, parts [][]string, ops []string,
) (string, ReplayCall, bool) {
	rc := ReplayCall{Signature: sig}
	if len(parts) < 2 || !shellReads(command, workingDir) {
		return "", rc, false
	}
	plan := make([]chainPart, 0, len(parts))
	for _, argv := range parts {
		if checked, ok := r.probeArgv(workingDir, argv); ok {
			plan = append(plan, chainPart{argv: checked})
			continue
		}
		if r.common == nil {
			return "", rc, false
		}
		text, isErr, ok := r.common.Lookup(Signature("shell", map[string]any{"command": strings.Join(argv, " ")}))
		if !ok || isErr {
			return "", rc, false
		}
		plan = append(plan, chainPart{text: text, fixture: true})
	}
	var b strings.Builder
	status := 0
	for i, part := range plan {
		if i > 0 && ops[i-1] == "&&" && status != 0 {
			break
		}
		switch {
		case part.fixture:
			rc.Common = true
			text, st := fixtureResult(part.text)
			status = st
			if text != "" {
				b.WriteString(text + "\n")
			}
		default:
			rc.Real = true
			text, st, err := r.runProbe(ctx, workingDir, part.argv)
			if err != nil {
				rc.Err = err
				return "", rc, true
			}
			status = st
			if text != "" {
				b.WriteString(text + "\n")
			}
		}
	}
	return shellResult(strings.TrimRight(b.String(), "\n"), status), rc, true
}

// fixtureResult reads a recorded shell result back into its output and exit status: "(no output)" is empty, and
// a final "[exit status N]" line is the status.
func fixtureResult(text string) (string, int) {
	if text == "(no output)" {
		return "", 0
	}
	if i := strings.LastIndex(text, "\n[exit status "); i >= 0 && strings.HasSuffix(text, "]") {
		if n, err := strconv.Atoi(strings.TrimSuffix(text[i+len("\n[exit status "):], "]")); err == nil {
			return strings.TrimRight(text[:i], "\n"), n
		}
	}
	if strings.HasPrefix(text, "[exit status ") && strings.HasSuffix(text, "]") {
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(text, "[exit status "), "]")); err == nil {
			return "", n
		}
	}
	return text, 0
}

// shellResult shapes a probe's or a chain's result as the shell tool shapes its own.
func shellResult(text string, status int) string {
	if status != 0 {
		return text + fmt.Sprintf("\n[exit status %d]", status)
	}
	if text == "" {
		return "(no output)"
	}
	return text
}
