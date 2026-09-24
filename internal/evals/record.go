package evals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

// privateNamesEnvVar overrides the recorder's private-name deny list entirely, as a comma-separated
// list (ruling P3-R35): set it when the automatic defaults miss a name a scenario's fixtures would
// otherwise leak, or on a machine where they are not trustworthy.
const privateNamesEnvVar = "TARACODE_EVALS_PRIVATE_NAMES"

// scriptWaitDelay bounds the wait for a script's output once it or its context has ended, the same
// value run.go's runCommand uses in the tools package: a background child that keeps the output pipe
// open cannot hold the run past this.
const scriptWaitDelay = 2 * time.Second

// ErrFixtureHostname says a recorded output or call signature carried a private name of the
// recorder's host (ruling P3-R35). The name is kept from when this checked only the bare host name.
var ErrFixtureHostname = errors.New("the fixture would carry a private name of the recorder's host; " +
	"fix the scenario or set " + privateNamesEnvVar)

// Recorder captures real tool results into a Store, redacted, for a task's fixtures (spec 6.1). It
// refuses to save anything whose redacted output, or whose call signature, carries one of a list of
// private names (ruling P3-R35): the recorder's own identity must never leak into a committed
// fixture.
type Recorder struct {
	store    *Store
	red      *redact.Redactor
	patterns []*regexp.Regexp // compiled once from the private-name deny list

	mu      sync.Mutex
	saved   []string
	refused error // the first private-name refusal; nil until one happens; sticky (P3-R35)
	saveErr error // the first error saving a fixture; nil until one happens (P3-R36)
}

// NewRecorder returns a recorder that redacts with red (nil = no redaction, tests only) and refuses
// any output or call signature carrying one of names, matched case-insensitively at a
// non-alphanumeric boundary, or the edge of the string, on both sides (ruling P3-R35). A nil or empty
// names disables the check.
func NewRecorder(store *Store, red *redact.Redactor, names []string) *Recorder {
	patterns := make([]*regexp.Regexp, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		patterns = append(patterns, regexp.MustCompile(`(?i)(?:^|[^a-zA-Z0-9])`+regexp.QuoteMeta(n)+`(?:$|[^a-zA-Z0-9])`))
	}
	return &Recorder{store: store, red: red, patterns: patterns}
}

// Middleware runs the tool for real and saves what came back under the call's signature: the
// output, or the error text with Error set, redacted before it touches the disk. A private-name
// refusal or a save error is still returned to the registry unchanged, so the model-facing text and
// the "nothing saved" behavior are as before, but the registry rebuilds every error as a fresh,
// unwrappable string on its way out, so both are also latched on the recorder itself (see Refused and
// SaveErr) for a caller to check directly. The refusal is sticky (ruling P3-R35): once one happens,
// every later call refuses immediately, without running or saving anything. Nothing is saved once the
// call's context is already done (ruling P3-R36): a fixture from a call the caller gave up on must
// not reach disk.
func (rec *Recorder) Middleware(call tools.Call, next tools.Executor) tools.Executor {
	return func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
		if refused := rec.Refused(); refused != nil {
			return "", refused
		}
		out, err := next(ctx, args, workingDir)
		sig := Signature(call.Tool, args)
		if call.DryRun {
			sig = DryRunSignature(call.Tool, args)
		}
		text, isErr := out, false
		if err != nil {
			text, isErr = err.Error(), true
		}
		if rec.red != nil {
			text = rec.red.Redact(text)
		}
		if rec.matchesPrivateName(text) || rec.matchesPrivateName(sig) {
			refusal := fmt.Errorf("%w: %s", ErrFixtureHostname, sig)
			rec.mu.Lock()
			if rec.refused == nil {
				rec.refused = refusal
			}
			rec.mu.Unlock()
			return "", refusal
		}
		if ctx.Err() != nil {
			return out, err
		}
		rec.mu.Lock()
		saveErr := rec.store.Save(sig, text, isErr)
		if saveErr == nil {
			rec.saved = append(rec.saved, sig)
		} else if rec.saveErr == nil {
			rec.saveErr = saveErr
		}
		rec.mu.Unlock()
		if saveErr != nil {
			return "", saveErr
		}
		return out, err
	}
}

// matchesPrivateName reports whether s carries one of the recorder's private names.
func (rec *Recorder) matchesPrivateName(s string) bool {
	for _, p := range rec.patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// Saved lists the signatures recorded, in order.
func (rec *Recorder) Saved() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.saved...)
}

// Refused is the first private-name refusal the middleware hit, or nil if none did. A caller driving
// calls through a registry (tools.Registry.Execute and DryRun redact and rebuild every error as a
// fresh string on the way out, so a wrapped sentinel like ErrFixtureHostname does not survive them)
// checks this after each call instead of the error the registry returns.
func (rec *Recorder) Refused() error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.refused
}

// SaveErr is the first error saving a fixture the middleware hit, or nil if none did. Checked the
// same way, and for the same reason, as Refused (ruling P3-R36): a failure writing a fixture must
// abort the recording, not be logged as an ordinary recorded tool error.
func (rec *Recorder) SaveErr() error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.saveErr
}

// Resolver answers a placeholder: kind is "pod" or "node", spec the text after it.
type Resolver func(ctx context.Context, kind, spec string) (string, error)

var placeholder = regexp.MustCompile(`\{\{\s*(pod|node)\b([^}]*)\}\}`)

// resolvePlaceholders replaces every placeholder in a call's arguments, however deeply nested in
// slices and maps, then fails if any string, at any depth, still contains "{{": a placeholder the
// pattern did not recognize must not silently reach a recorded tool call (ruling P3-R36).
func resolvePlaceholders(ctx context.Context, args map[string]any, resolve Resolver) (map[string]any, error) {
	out := make(map[string]any, len(args))
	for k, v := range args {
		resolved, err := resolveArgValue(ctx, v, resolve)
		if err != nil {
			return nil, fmt.Errorf("argument %s: %w", k, err)
		}
		out[k] = resolved
	}
	return out, nil
}

// resolveArgValue resolves every placeholder in v: a string is matched against the placeholder
// pattern and checked for a leftover "{{" afterward, a slice or map is walked recursively, anything
// else passes through unchanged.
func resolveArgValue(ctx context.Context, v any, resolve Resolver) (any, error) {
	switch val := v.(type) {
	case string:
		return resolveStringPlaceholders(ctx, val, resolve)
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			resolved, err := resolveArgValue(ctx, e, resolve)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			resolved, err := resolveArgValue(ctx, e, resolve)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}

// resolveStringPlaceholders resolves every placeholder in s and rejects a result that still contains
// "{{", whether or not s matched the placeholder pattern in the first place: a brace pair the pattern
// did not recognize as {{pod ...}} or {{node ...}} is left untouched by the replace below, so this is
// the only thing that catches it before it reaches a recorded tool call.
func resolveStringPlaceholders(ctx context.Context, s string, resolve Resolver) (string, error) {
	resolved := s
	if placeholder.MatchString(s) {
		if resolve == nil {
			return "", errors.New("has a placeholder but no resolver")
		}
		var firstErr error
		resolved = placeholder.ReplaceAllStringFunc(s, func(m string) string {
			sub := placeholder.FindStringSubmatch(m)
			val, err := resolve(ctx, sub[1], strings.TrimSpace(sub[2]))
			if err != nil && firstErr == nil {
				firstErr = err
			}
			return val
		})
		if firstErr != nil {
			return "", firstErr
		}
	}
	if strings.Contains(resolved, "{{") {
		return "", fmt.Errorf("%q still has an unresolved placeholder", resolved)
	}
	return resolved, nil
}

// KubectlResolver resolves placeholders against the live cluster: {{pod app=x ns=y}} is the first
// pod matching the label selector in the namespace, {{node}} the first node. The label terms join
// into one repeatable -l flag: kubectl's -l does not combine across repeated flags, so passing them
// separately would silently keep only the last one. kubectl's stderr is folded into the error.
func KubectlResolver(ctx context.Context, kind, spec string) (string, error) {
	argv := []string{"get", kind, "-o", "jsonpath={.items[0].metadata.name}"}
	var labels []string
	for _, f := range strings.Fields(spec) {
		if ns, ok := strings.CutPrefix(f, "ns="); ok {
			argv = append(argv, "-n", ns)
		} else {
			labels = append(labels, f)
		}
	}
	if len(labels) > 0 {
		argv = append(argv, "-l", strings.Join(labels, ","))
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", argv...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return "", fmt.Errorf("resolve {{%s %s}}: %w: %s", kind, spec, err, s)
		}
		return "", fmt.Errorf("resolve {{%s %s}}: %w", kind, spec, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("resolve {{%s %s}}: nothing matched", kind, spec)
	}
	return name, nil
}

// privateNames returns the recorder's private-name deny list (ruling P3-R35): privateNamesEnvVar's
// comma-separated entries (trimmed, empty ones dropped) when it is set, replacing the defaults
// entirely; otherwise defaultPrivateNames.
func privateNames() ([]string, error) {
	if raw, ok := os.LookupEnv(privateNamesEnvVar); ok {
		var names []string
		for _, n := range strings.Split(raw, ",") {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		return names, nil
	}
	return defaultPrivateNames()
}

// defaultPrivateNames builds privateNames' default list: the host's short and full names, the
// resolv.conf search domains, and the global unicast addresses of its physical network interfaces.
// The host's own short name (os.Hostname) must be known, or a recording cannot proceed at all; every
// other source is best effort, so a machine missing a CNAME record, a resolv.conf, or any interfaces
// still gets a usable, if shorter, deny list rather than failing the recording.
func defaultPrivateNames() ([]string, error) {
	short, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("look up the host name: %w", err)
	}
	names := []string{short}
	lookupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if full, cnameErr := net.DefaultResolver.LookupCNAME(lookupCtx, short); cnameErr == nil {
		if trimmed := strings.TrimSuffix(full, "."); trimmed != "" && !strings.EqualFold(trimmed, short) {
			names = append(names, trimmed)
		}
	}
	domains := resolvSearchDomains()
	for _, d := range domains {
		names = append(names, short+"."+d)
	}
	names = append(names, domains...)
	names = append(names, physicalInterfaceAddrs()...)
	return names, nil
}

// resolvSearchDomains reads the search and domain entries of /etc/resolv.conf, best effort: a
// missing or unreadable file yields none.
func resolvSearchDomains() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var domains []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "search":
			domains = append(domains, fields[1:]...)
		case "domain":
			domains = append(domains, fields[1])
		}
	}
	return domains
}

// physicalInterfaceAddrs returns the global unicast addresses of the machine's physical network
// interfaces, skipping loopback and the virtual interfaces containers and VMs create. Best effort:
// any failure yields none, rather than blocking a recording run.
func physicalInterfaceAddrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addrs []string
	for _, iface := range ifaces {
		if isVirtualInterfaceName(iface.Name) {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifaceAddrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || !ip.IsGlobalUnicast() {
				continue
			}
			addrs = append(addrs, ip.String())
		}
	}
	return addrs
}

// isVirtualInterfaceName reports whether name is the loopback interface or a container/VM virtual
// interface that a recorded fixture is never expected to name.
func isVirtualInterfaceName(name string) bool {
	if name == "lo" {
		return true
	}
	for _, prefix := range []string{"docker", "br-", "veth", "virbr"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// RecordTask records one task: it copies the workdir to a fresh directory, runs the scenario's
// setup there, executes every recorded call through a registry with the record middleware into a
// fresh fixture store, and runs the teardown whatever happened. scenariosRoot is evals/scenarios;
// out receives progress.
func RecordTask(ctx context.Context, t Task, scenariosRoot string, resolve Resolver, out io.Writer) error {
	if t.Record == nil {
		return fmt.Errorf("%s: no record block", t.ID)
	}
	scenario := filepath.Join(scenariosRoot, t.Record.Scenario)
	if _, err := os.Stat(filepath.Join(scenario, "setup.sh")); err != nil {
		return fmt.Errorf("%s: scenario %s has no setup.sh", t.ID, t.Record.Scenario)
	}
	names, err := privateNames()
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	runDir, err := os.MkdirTemp("", "taracode-record-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	// A missing workdir root is simply "no workdir"; anything else (a permission error, a symlink
	// copyDir refuses, ...) is a real failure the caller must see (ruling P3-R36).
	workdir := filepath.Join(t.Dir, "workdir")
	if _, err := os.Lstat(workdir); err == nil {
		if err := copyDir(workdir, runDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	env := append(os.Environ(), "EVAL_WORKDIR="+runDir, "EVAL_SCENARIO="+scenario, "EVAL_TASK="+t.ID)
	_, _ = fmt.Fprintf(out, "== %s: setup %s\n", t.ID, t.Record.Scenario)
	if err := runScript(ctx, scenario, "setup.sh", env, out); err != nil {
		_ = runScript(context.Background(), scenario, "teardown.sh", env, out)
		return fmt.Errorf("%s: setup: %w", t.ID, err)
	}
	defer func() {
		_, _ = fmt.Fprintf(out, "== %s: teardown\n", t.ID)
		_ = runScript(context.Background(), scenario, "teardown.sh", env, out)
	}()
	return recordCalls(ctx, t, runDir, resolve, names, out)
}

// recordCalls executes the task's calls with a recording registry in runDir, into a fresh temporary
// store that replaces the task's fixtures/ only when every call succeeds (ruling P3-R36): an aborted
// or repeated run never leaves the real fixtures mixed with stale or partial entries.
func recordCalls(ctx context.Context, t Task, runDir string, resolve Resolver, names []string, out io.Writer) error {
	tempParent, err := os.MkdirTemp(t.Dir, "fixtures-recording-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tempParent) }()
	store, err := LoadFixtures(tempParent)
	if err != nil {
		return err
	}
	red, err := redact.New(redact.Options{Environ: os.Environ()})
	if err != nil {
		return err
	}
	rec := NewRecorder(store, red, names)
	reg := tools.NewBuiltinRegistry(tools.Options{Redactor: red, Middleware: rec.Middleware}, tools.Config{})
	for i, call := range t.Record.Calls {
		if err := recordOneCall(ctx, reg, rec, t, i, call, resolve, runDir, out); err != nil {
			return err
		}
	}
	//nolint:gosec // store.Dir() is inside a fresh recorder run directory, made real even with zero fixtures
	if err := os.MkdirAll(store.Dir(), 0o755); err != nil {
		return err
	}
	if err := swapFixtures(filepath.Join(t.Dir, "fixtures"), store.Dir()); err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	_, _ = fmt.Fprintf(out, "== %s: %d fixtures\n", t.ID, store.Len())
	return nil
}

// recordOneCall resolves one recorded call's placeholders, runs it, and reports the outcome, or
// returns the reason recordCalls must abort: a placeholder failure, a private-name refusal, a save
// error, or the call's context ending mid-run. All three are checked after every call (ruling
// P3-R36), since the registry does not hand back a sentinel errors.Is can see for any of them.
func recordOneCall(
	ctx context.Context, reg *tools.Registry, rec *Recorder, t Task, i int, call RecordCall,
	resolve Resolver, runDir string, out io.Writer,
) error {
	args, err := resolvePlaceholders(ctx, call.Args, resolve)
	if err != nil {
		return fmt.Errorf("%s: call %d: %w", t.ID, i, err)
	}
	var text string
	if call.DryRun {
		text, err = reg.DryRun(ctx, call.Tool, args, runDir)
	} else {
		text, err = reg.Execute(ctx, call.Tool, args, runDir)
	}
	if refused := rec.Refused(); refused != nil {
		return fmt.Errorf("%s: %w", t.ID, refused)
	}
	if saveErr := rec.SaveErr(); saveErr != nil {
		return fmt.Errorf("%s: %w", t.ID, saveErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", t.ID, ctxErr)
	}
	sig := Signature(call.Tool, args)
	if call.DryRun {
		sig = DryRunSignature(call.Tool, args)
	}
	status := "ok"
	switch {
	case call.DryRun && errors.Is(err, tools.ErrNoDryRun):
		status, text = "skipped", err.Error()
	case err != nil:
		status, text = "error recorded", err.Error()
	}
	_, _ = fmt.Fprintf(out, "   %-14s %s (%d bytes)\n", status, sig, len(text))
	return nil
}

// swapFixtures replaces finalDir with newDir: the previous fixtures move aside, the new ones move
// in, and only then does the aside copy get removed, so a failure partway through leaves either the
// old fixtures or the new ones intact, never a mix (ruling P3-R36).
func swapFixtures(finalDir, newDir string) error {
	asideDir := finalDir + ".replaced"
	_ = os.RemoveAll(asideDir)
	if _, err := os.Lstat(finalDir); err == nil {
		if err := os.Rename(finalDir, asideDir); err != nil {
			return fmt.Errorf("set aside the previous fixtures: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(newDir, finalDir); err != nil {
		return fmt.Errorf("move the recorded fixtures into place: %w", err)
	}
	return os.RemoveAll(asideDir)
}

// runScript runs <dir>/<name> with sh in dir, streaming its output to out, under a five-minute
// limit. A missing script is not an error. The script runs in its own process group so a background
// job it starts (a stray `kubectl port-forward &` in a setup.sh, say) cannot outlive it and hang a
// later step: canceling the context (the five-minute limit here, or the caller's own) kills the whole
// group, not just the sh process, and WaitDelay bounds the wait for its output once that happens.
func runScript(ctx context.Context, dir, name string, env []string, out io.Writer) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", path)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
	cmd.WaitDelay = scriptWaitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd.Run()
}

// copyDir copies every file and directory under src into dst at the same relative path. A symlink
// anywhere under src, whether it names a file or a directory, is an error: a recorder run must never
// follow a link out of the task's own workdir (ruling P3-R36).
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: a symlink cannot be copied into a recorder run directory", p)
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755) //nolint:gosec // dst is a fresh recorder run directory
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // p walks the task's own workdir, a trusted corpus path
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) //nolint:gosec // dst is a fresh recorder run directory
	})
}
