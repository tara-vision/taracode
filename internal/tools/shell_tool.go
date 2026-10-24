package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
)

const (
	defaultShellTimeout = 60 * time.Second
	maxShellTimeout     = 600 * time.Second
	groupSettle         = 200 * time.Millisecond // a detaching or just-finished job leaves the group on its own
	groupGrace          = 2 * time.Second        // from TERM to KILL, and the wait after a KILL
)

// drainGrace is how long the output gets to reach its end once the group is gone, and the live copy to catch up;
// a variable so a test can shorten it.
var drainGrace = 2 * time.Second

// interruptGrace is how long an interrupted command (its deadline, or the operator's signal) gets to stop on its
// own before TERM; cancelGrace is that grace when the call's parent context ended the call (an eval turn
// cancelled: the caller waits little for its tool). Variables so a test can shorten them.
var (
	interruptGrace = 30 * time.Second
	cancelGrace    = 5 * time.Second
)

// forwarded are the signals a call passes to its command and quits on afterwards. A signal taracode started with
// ignored (a nohup, a background job of a script) stays ignored: it is neither forwarded nor acted on.
var (
	forwarded      = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
	ignoredAtStart = func() map[os.Signal]bool {
		ignored := map[os.Signal]bool{}
		for _, sig := range forwarded {
			if s, ok := sig.(syscall.Signal); ok && signal.Ignored(s) {
				ignored[sig] = true
			}
		}
		return ignored
	}()
)

// quit leaves taracode on a signal the call forwarded to its command, once the command's group has stopped: the
// signal does what it would have done at once (a test records the call instead). Tools run one at a time; two
// concurrent calls would let the first quit end the second mid-grace.
var quit = func(sig os.Signal) {
	signal.Reset(sig)
	if s, ok := sig.(syscall.Signal); ok {
		_ = syscall.Kill(os.Getpid(), s)
	}
}

// newPipe opens the pipe a call's output travels on, and waitFor waits for the command; variables so a test can
// refuse a pipe or fail a wait.
var (
	newPipe = os.Pipe
	waitFor = func(cmd *exec.Cmd) error { return cmd.Wait() }
)

// hasSetsid says whether the system has setsid(1), the way a command detaches a process from the call.
var hasSetsid = func() bool { _, err := exec.LookPath("setsid"); return err == nil }()

// leftBehindNote closes a result whose command left background processes in its process group: the call ended them.
const leftBehindNote = "[ended the background processes the command left behind]"

// detachHint tells the model how a process outlives a call on this system; it travels with the note, never with
// the tool's description, which is the same on every host.
func detachHint(setsid bool) string {
	if setsid {
		return "A process to outlive the call needs a session of its own and its output in a file: " +
			"setsid CMD >FILE 2>&1 </dev/null &"
	}
	return "This system has no setsid: a process to outlive the call needs a service manager " +
		"(launchctl, brew services) or a terminal multiplexer."
}

// withTimeout derives a per-call context; a zero d keeps the parent's deadline.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// ShellTool runs a command line with sh -c in the working directory. stream, when set, receives the
// output live (NewBuiltinRegistry hands it a redacting one, flushed when the command ends); the
// result carries the merged output and the exit status. The invocation's targets are the hosts the
// line names, the files it writes or removes and the clusters its kubectl and helm mutations act on,
// for the protected targets.
func ShellTool(stream io.Writer) *Tool {
	return &Tool{
		Name: "shell", ReadForm: true,
		Description: "Run a shell command in the working directory. Read-only commands (cat, grep, ps, df, dig, jq, date, " +
			"kubectl get, git status, ...) work in investigate mode; anything else needs operate mode. The command has no " +
			"terminal (a prompt fails at once) and the background processes it leaves end with the call; detach one " +
			"with setsid where the system has it, or start it through a service manager.",
		Params: []Param{
			{Name: "command", Type: "string", Description: "The command line", Required: true},
			{Name: "timeout", Type: "integer", Description: fmt.Sprintf("Seconds before the command is interrupted "+
				"(default 60, max 600); it then has %d s to stop on its own before TERM and KILL", int(interruptGrace.Seconds()))},
		},
		Classify: classifyShell,
		Run:      runShell(stream),
	}
}

// runShell runs a shell call's command line with sh -c in the working directory, with its timeout (default 60
// seconds, at most 600), streaming the output to stream when it is set; the result carries the merged output and
// the exit status. A call is a bounded action. The command runs in a session of its own: a process group to
// itself and no controlling terminal, so a prompt fails at once instead of waiting for input the model cannot
// give. The deadline, and a signal the operator sends taracode meanwhile, start a ladder on the group: an
// interrupt (what Ctrl-C does: terraform stops at a safe point), TERM after the grace, KILL after groupGrace; a
// repeated signal climbs it at once. The output is read until the group has stopped. Whatever the command leaves
// in the group when it returns is ended the same way, which the result says. After a forwarded signal taracode
// quits on it, as it would have at once.
func runShell(stream io.Writer) func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
	return func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
		command, err := required(args, "command")
		if err != nil {
			return "", err
		}
		timeout := shellTimeout(args)
		parent := ctx
		ctx, cancel := withTimeout(ctx, timeout)
		defer cancel()
		if err := ctx.Err(); err != nil {
			return "", err // a call already cancelled starts nothing
		}
		sigs := make(chan os.Signal, 8)
		signal.Notify(sigs, forwarded...) // before the start: the command inherits no ignore, and no signal falls in a gap
		defer signal.Stop(sigs)
		call, err := startShell(parent, command, workingDir, stream)
		if err != nil {
			return "", err
		}
		runErr := call.wait(ctx, sigs)
		leftBehind := call.end(sigs)
		call.noteAll(sigs) // a signal that landed after sh had exited still counts
		out := strings.TrimRight(call.output(), "\n")
		switch {
		case call.received != nil:
			quit(call.received)
			return out, fmt.Errorf("command interrupted by %s", call.received)
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return out, fmt.Errorf("command timed out after %s", timeout)
		case ctx.Err() != nil:
			return out, ctx.Err() // cancelled while the command ran: interrupted, and reported as the cancel
		}
		code, err := exitStatus(runErr)
		if err != nil {
			return out, err
		}
		lines := []string{}
		if out != "" {
			lines = append(lines, out)
		}
		if leftBehind {
			lines = append(lines, leftBehindNote+" "+detachHint(hasSetsid))
		}
		if code != 0 {
			lines = append(lines, fmt.Sprintf("[exit status %d]", code)) // last: the evals read the status there
		}
		if len(lines) == 0 {
			return "(no output)", nil
		}
		return strings.Join(lines, "\n"), nil
	}
}

// shellCall is one running sh -c: the process, the leader of a session of its own (so pgid is its pid), the pipe
// that carries its merged output, the copier draining that pipe into buf, the streamer tailing buf to the live
// stream, and the ladder's state.
type shellCall struct {
	cmd      *exec.Cmd
	pgid     int
	parent   context.Context // the caller's context: its end shortens an interrupted command's grace
	reader   *os.File
	mu       sync.Mutex
	more     *sync.Cond // signalled when buf grew, the copier ended or the live copy was cut
	buf      bytes.Buffer
	eof      bool
	cut      bool          // the live copy was cut: the terminal fell too far behind
	copied   chan struct{} // closed once the copier has drained the pipe
	streamed chan struct{} // closed once the streamer has written everything, or at once without a stream
	stage    int           // 0 running, 1 interrupted, 2 terminated, 3 killed
	due      time.Time     // when the current stage's grace ends
	received os.Signal     // the first operator signal forwarded
}

// startShell starts the command in its own session with its output on a pipe of the call's own, so the output can
// be read for as long as the call decides, not for as long as sh lives. The copier fills buf first, whatever the
// terminal does; the streamer copies buf to the live stream as far behind as the terminal makes it.
func startShell(parent context.Context, command, dir string, stream io.Writer) (*shellCall, error) {
	reader, writer, err := newPipe()
	if err != nil {
		return nil, err
	}
	//nolint:gosec // the shell tool runs what the model asked for, after the policy gate
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	_ = writer.Close() // the parent's copy: the pipe ends when the command's processes have let go of it
	call := &shellCall{
		cmd: cmd, pgid: cmd.Process.Pid, parent: parent, reader: reader,
		copied: make(chan struct{}), streamed: make(chan struct{}),
	}
	call.more = sync.NewCond(&call.mu)
	go call.copy()
	if stream == nil {
		close(call.streamed)
	} else {
		go call.tail(stream)
	}
	return call, nil
}

// copy drains the pipe into buf until its end, closes the reader and says so.
func (c *shellCall) copy() {
	defer close(c.copied)
	defer func() { _ = c.reader.Close() }()
	chunk := make([]byte, 32<<10)
	for {
		n, err := c.reader.Read(chunk)
		c.mu.Lock()
		c.buf.Write(chunk[:n])
		if err != nil {
			c.eof = true
		}
		c.mu.Unlock()
		c.more.Broadcast()
		if err != nil {
			return
		}
	}
}

// tail writes what buf gained to the live stream, best effort: a slow or failing terminal never costs the
// result. A copy the call cut off (the terminal too far behind) ends with a line saying so, and the stream is
// flushed when the copy ends, so the last partial line reaches the terminal before the result does.
func (c *shellCall) tail(stream io.Writer) {
	defer close(c.streamed)
	defer func() {
		if f, ok := stream.(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
	}()
	sent, atLineStart := 0, true
	for {
		c.mu.Lock()
		for c.buf.Len() == sent && !c.eof && !c.cut {
			c.more.Wait()
		}
		chunk := append([]byte(nil), c.buf.Bytes()[sent:]...)
		done, cut := c.eof && len(chunk) == 0, c.cut
		c.mu.Unlock()
		if cut {
			note := "[live output cut; the result has it all]\n"
			if !atLineStart {
				note = "\n" + note
			}
			_, _ = io.WriteString(stream, note)
			return
		}
		if done {
			return
		}
		_, _ = stream.Write(chunk)
		sent += len(chunk)
		atLineStart = len(chunk) == 0 || chunk[len(chunk)-1] == '\n'
	}
}

// output is everything the command wrote, once the copier is done.
func (c *shellCall) output() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// wait waits for sh to exit. The deadline and every forwarded signal start or climb the ladder on the group, so
// the wait is bounded: KILL takes sh with it. The call's own deadline after an operator's interrupt climbs it
// too: the operator's wait is over. The caller's context ending shortens an interrupted command's grace. A signal
// taracode started with ignored is dropped; a SIGQUIT (Ctrl-backslash) goes straight to KILL.
func (c *shellCall) wait(ctx context.Context, sigs <-chan os.Signal) error {
	waited := make(chan error, 1)
	go func() { waited <- waitFor(c.cmd) }()
	deadline, parentDone := ctx.Done(), c.parent.Done()
	var due <-chan time.Time
	for {
		select {
		case err := <-waited:
			return err
		case <-deadline:
			deadline = nil
			due = c.escalate(c.grace())
		case <-parentDone:
			parentDone = nil
			if c.shorten() {
				due = time.After(time.Until(c.due))
			}
		case sig := <-sigs:
			if !c.note(sig) {
				continue
			}
			due = c.escalate(interruptGrace)
		case <-due:
			due = c.escalate(interruptGrace)
		}
	}
}

// grace is an interrupted command's time to stop on its own: the short one once the caller's context has ended.
func (c *shellCall) grace() time.Duration {
	if c.parent.Err() != nil {
		return cancelGrace
	}
	return interruptGrace
}

// shorten cuts the grace of a command at its interrupt to cancelGrace, the caller's context having ended;
// reports whether the due time moved.
func (c *shellCall) shorten() bool {
	if c.stage != 1 {
		return false
	}
	if sooner := time.Now().Add(cancelGrace); sooner.Before(c.due) {
		c.due = sooner
		return true
	}
	return false
}

// noteAll notes the signals waiting in the channel without blocking.
func (c *shellCall) noteAll(sigs <-chan os.Signal) {
	for {
		select {
		case sig := <-sigs:
			c.note(sig)
		default:
			return
		}
	}
}

// note records an operator's signal: false for one taracode started with ignored, which changes nothing. A
// SIGQUIT moves the ladder so that the next step is KILL.
func (c *shellCall) note(sig os.Signal) bool {
	if ignoredAtStart[sig] {
		return false
	}
	if c.received == nil {
		c.received = sig
	}
	if sig == syscall.SIGQUIT && c.stage < 2 {
		c.stage = 2
	}
	return true
}

// escalate sends the group the next signal of the ladder and returns the timer of the step after it: an
// interrupt, with grace to stop on its own; TERM, with groupGrace; then KILL, again every groupGrace while
// anything remains.
func (c *shellCall) escalate(grace time.Duration) <-chan time.Time {
	c.stage++
	switch c.stage {
	case 1:
		_ = syscall.Kill(-c.pgid, syscall.SIGINT)
		c.due = time.Now().Add(grace)
	case 2:
		_ = syscall.Kill(-c.pgid, syscall.SIGTERM)
		c.due = time.Now().Add(groupGrace)
	default:
		_ = syscall.Kill(-c.pgid, syscall.SIGKILL)
		c.due = time.Now().Add(groupGrace)
	}
	return time.After(time.Until(c.due))
}

// end ends what is left of the group once sh has exited, then drains the output; it reports whether anything was
// still there to end. A command that returned on its own gets groupSettle for a detaching or just-finished job to
// leave, then TERM and KILL; an interrupted one continues the ladder from where wait left it, with the time it
// has left. A signal arriving here cuts a wait short and climbs the ladder.
func (c *shellCall) end(sigs <-chan os.Signal) bool {
	left := true
	if c.stage == 0 {
		c.due = time.Now().Add(groupSettle)
		if c.gone(sigs) {
			left = false
		} else {
			c.stage = max(c.stage, 1) // what a returned command left behind gets no interrupt: TERM is next
			c.escalate(0)
		}
	}
	for left && !c.gone(sigs) && c.stage < 3 {
		c.escalate(0)
	}
	c.drain(sigs)
	return left
}

// gone polls until kill(-pgid, 0) fails or the due time passes; false as soon as a forwarded signal arrives, so
// the caller climbs the ladder at once; the caller's context ending shortens the grace. A failure of the probe
// means nothing is left that the call could signal: ESRCH when the group is empty, EPERM when only a zombie
// remains (what macOS answers) or a member the call may not signal (a setuid program); both are "gone" here, so
// never narrow this to ESRCH.
func (c *shellCall) gone(sigs <-chan os.Signal) bool {
	parentDone := c.parent.Done()
	for syscall.Kill(-c.pgid, 0) == nil {
		if !time.Now().Before(c.due) {
			return false
		}
		select {
		case sig := <-sigs:
			if c.note(sig) {
				return false
			}
		case <-parentDone:
			parentDone = nil
			c.shorten()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return true
}

// drain waits for the output to reach its end (every writer gone) for drainGrace once the group is gone, closes
// the reader otherwise (a write end a detached process still holds no longer reaches this call), then gives the
// live copy the same time to catch up and cuts it off otherwise. The result keeps everything that was read, and
// a signal arriving meanwhile is noted for it.
func (c *shellCall) drain(sigs <-chan os.Signal) {
	if !c.await(c.copied, sigs) {
		_ = c.reader.Close()
		<-c.copied
	}
	if !c.await(c.streamed, sigs) {
		c.mu.Lock()
		c.cut = true
		c.mu.Unlock()
		c.more.Broadcast()
	}
}

// await waits for done up to drainGrace, noting the signals that arrive meanwhile; reports whether done closed.
func (c *shellCall) await(done <-chan struct{}, sigs <-chan os.Signal) bool {
	deadline := time.After(drainGrace)
	for {
		select {
		case <-done:
			return true
		case <-deadline:
			return false
		case sig := <-sigs:
			c.note(sig)
		}
	}
}

// exitStatus is the command's exit status once it has run: zero, or an ExitError's; any other failure of the run
// is an error.
func exitStatus(runErr error) (int, error) {
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return 0, nil
	case errors.As(runErr, &exitErr):
		return exitErr.ExitCode(), nil
	}
	return 0, runErr
}

// shellTimeout is a shell call's timeout: the seconds its timeout argument names, the default when it names none
// or a non-positive number, and at most maxShellTimeout.
func shellTimeout(args map[string]any) time.Duration {
	timeout := time.Duration(argInt(args, "timeout", 0)) * time.Second
	switch {
	case timeout <= 0:
		return defaultShellTimeout
	case timeout > maxShellTimeout:
		return maxShellTimeout
	}
	return timeout
}

// classifyShell classifies a shell call: the line's verb and class, and its targets (the hosts it names, the
// files it writes or names and where a copy lands, the finds it runs and, for a mutation, the clusters its kubectl
// and helm mutations act on and the words of a line taracode cannot see into).
func classifyShell(args map[string]any, workingDir string) policy.Invocation {
	command := argString(args, "command")
	res := classify.Shell(command)
	glob := shellGlob(res.DotGlob)
	targets := policy.Targets{Hosts: res.Hosts, Named: shellTargets(res.Named, workingDir, glob),
		Paths: slices.Concat(shellTargets(res.Paths, workingDir, glob), landingTargets(res.Landings, workingDir, glob)),
		Finds: findStarts(res.Finds, workingDir)}
	if res.Classification == policy.Mutate && res.Opaque {
		targets.Named = slices.Concat(targets.Named, shellTargets(lineTokens(command), workingDir, nil),
			shellTargets(res.Words, workingDir, glob))
	}
	if res.Classification == policy.Mutate && len(res.Kube) > 0 {
		targets.KubeContext, targets.KubeNamespace, targets.KubeReason =
			shellKubeTargets(context.Background(), workingDir, res.Kube)
	}
	return policy.Invocation{Tool: "shell", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
		Command: command, Lines: res.Lines, Targets: targets}
}

// lineTokens are the words of an opaque line that may name a file: the line split at whitespace, quotes,
// brackets and the shell's operators, without its options.
func lineTokens(command string) []string {
	var out []string
	for _, token := range strings.FieldsFunc(command, tokenSeparator) {
		if !strings.HasPrefix(token, "-") {
			out = append(out, token)
		}
	}
	return out
}

func tokenSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\"'`()[]{},;=<>|&+\\", r)
}

// globFunc expands a path glob to the files it matches now.
type globFunc func(pattern string) []string

// shellGlob expands a glob as the shell does: a * or ? does not match the leading dot of a name unless the
// pattern's element starts with a dot or the line turns dotglob on.
func shellGlob(dotglob bool) globFunc {
	return func(pattern string) []string {
		matches, _ := filepath.Glob(pattern)
		if dotglob {
			return matches
		}
		elems := strings.Split(filepath.ToSlash(pattern), "/")
		var out []string
		for _, m := range matches {
			if !dotHidden(elems, strings.Split(filepath.ToSlash(m), "/")) {
				out = append(out, m)
			}
		}
		return out
	}
}

// dotHidden reports a match the shell's glob leaves out: an element that starts with a dot where the pattern's
// element does not.
func dotHidden(pattern, match []string) bool {
	for i := range min(len(pattern), len(match)) {
		if strings.HasPrefix(match[i], ".") && !strings.HasPrefix(pattern[i], ".") {
			return true
		}
	}
	return false
}

// shellTargets resolves the files a shell line acts on: a leading ~, $HOME or ${HOME} to the home
// directory, a relative path against the working directory, and, given a glob function, a glob to
// itself and to the files it matches now. The words of an opaque line are resolved without one: a * in
// a program's text is no file name.
func shellTargets(paths []string, workingDir string, glob globFunc) []string {
	home, _ := os.UserHomeDir()
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		abs := resolvePath(expandHome(expandPWD(p, workingDir), home), workingDir)
		add(abs)
		if glob != nil && strings.ContainsAny(p, "*?[") {
			for _, m := range glob(abs) {
				add(m)
			}
		}
	}
	return out
}

// landingTargets resolves where a copy, move or link puts each source, as shellTargets resolves a path, and
// keeps a landing under each directory its destination names now, a glob expanded as the shell expands it: a
// destination that is no directory is written by its own name.
func landingTargets(landings []string, workingDir string, glob globFunc) []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, landing := range landings {
		abs := resolvePath(expandHome(expandPWD(landing, workingDir), home), workingDir)
		dirs := []string{filepath.Dir(abs)}
		if wildcardLevels(dirs[0]) > maxLandingGlobLevels { // too deep to glob: the directory above it is written
			out = append(out, abs, aboveWildcards(dirs[0]))
			continue
		}
		if strings.ContainsAny(dirs[0], "*?[") {
			dirs = glob(dirs[0])
		}
		for _, dir := range dirs {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				out = append(out, shellTargets([]string{filepath.Join(dir, filepath.Base(abs))}, "", glob)...)
			}
		}
	}
	return out
}

// maxLandingGlobLevels bounds the wildcard levels of a landing's directory the shell tool globs: a deeper
// pattern (*/*/*/*/) would read the whole tree for each command.
const maxLandingGlobLevels = 3

// aboveWildcards is the directory an absolute path that holds a glob character names above its first such
// element: the root for /*/...
func aboveWildcards(p string) string {
	slashed := filepath.ToSlash(p)
	above := slashed[:strings.LastIndex(slashed[:strings.IndexAny(slashed, "*?[")], "/")]
	if above == "" {
		return string(filepath.Separator)
	}
	return filepath.FromSlash(above)
}

// wildcardLevels counts the elements of a path that hold a glob character.
func wildcardLevels(p string) int {
	n := 0
	for _, e := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.ContainsAny(e, "*?[") {
			n++
		}
	}
	return n
}

// findStarts resolves the start point of each find a shell line runs as shellTargets resolves a path; the
// start point as written stays, since it begins every path find prints.
func findStarts(finds []policy.Find, workingDir string) []policy.Find {
	home, _ := os.UserHomeDir()
	out := make([]policy.Find, 0, len(finds))
	for _, f := range finds {
		found := f
		found.Start = resolvePath(expandHome(expandPWD(f.Start, workingDir), home), workingDir)
		out = append(out, found)
	}
	return out
}

// expandPWD replaces a leading ~+, the shell's $PWD, with the working directory.
func expandPWD(p, workingDir string) string {
	if rest, ok := strings.CutPrefix(p, "~+"); ok && (rest == "" || rest[0] == '/') {
		return workingDir + rest
	}
	return p
}

// expandHome replaces a leading ~, $HOME or ${HOME} with home.
func expandHome(p, home string) string {
	if home == "" {
		return p
	}
	for _, prefix := range []string{"~", "$HOME", "${HOME}"} {
		if p == prefix {
			return home
		}
		if rest, ok := strings.CutPrefix(p, prefix+"/"); ok {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// causeShellExpansion is why a context or namespace the shell expands before kubectl or helm reads
// it is "*".
const causeShellExpansion = "a context or namespace word is expanded by the shell"

// shellKubeTargets resolves the clusters the kubectl and helm mutations of a shell line act on, as
// the kubectl and helm tools do (what a command does not name is HELM_KUBECONTEXT or HELM_NAMESPACE
// for helm, then the current context or namespace of its kubeconfig, each kubeconfig read once), and
// reports "*" for a value the shell computes or expands at run time or when the line names more than
// one context or namespace.
func shellKubeTargets(ctx context.Context, workingDir string, refs []classify.KubeTarget) (
	kubeContext, namespace, reason string,
) {
	resolver := newKubeResolver(ctx, workingDir)
	contexts := make([]string, 0, len(refs))
	namespaces := make([]string, 0, len(refs))
	for _, ref := range refs {
		c, ns, cause := runTimeAsAll(ref.Context), runTimeAsAll(ref.Namespace), ref.Cause
		if cause == "" && (c != ref.Context || ns != ref.Namespace) {
			cause = causeShellExpansion
		}
		if ref.Helm {
			c, ns = helmEnvTargets(c, ns)
		}
		t := resolver.targets(c, ns, ref.Kubeconfig, cause)
		contexts = append(contexts, t.KubeContext)
		namespaces = append(namespaces, t.KubeNamespace)
		if reason == "" {
			reason = t.KubeReason
		}
	}
	return oneOrAll(contexts), oneOrAll(namespaces), reason
}

// runTimeAsAll maps a value the shell computes or expands at run time to "*", since it can be any: a
// $CTX or a backtick, and a glob or a brace (sh expands kube-sys{tem,} to kube-system kube-sys, and
// kube-syst* to the files it matches in the working directory). The classifier's words have their
// quotes removed, so a quoted glob or brace, which sh leaves alone, is "*" too: that fails closed, and
// no namespace can hold these characters.
func runTimeAsAll(value string) string {
	if strings.ContainsAny(value, "$`*?[{") {
		return "*"
	}
	return value
}

// oneOrAll is the value every entry shares, or "*" when they differ.
func oneOrAll(values []string) string {
	for _, v := range values[1:] {
		if v != values[0] {
			return "*"
		}
	}
	return values[0]
}
