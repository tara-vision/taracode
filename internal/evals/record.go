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
	"slices"
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

// Middleware runs the tool for real and hands what came back, the output or the error text with
// Error set, to guardAndSave under the call's signature. A private-name refusal or a save error is
// still returned to the registry unchanged, so the model-facing text and the "nothing saved"
// behavior are as before, but the registry rebuilds every error as a fresh, unwrappable string on its
// way out, so both are also latched on the recorder itself (see Refused and SaveErr) for a caller to
// check directly. The refusal is sticky (ruling P3-R35): once one happens, every later call refuses
// immediately, without running or saving anything.
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
		if _, guardErr := rec.guardAndSave(ctx, sig, text, isErr, replaceExisting); guardErr != nil {
			return "", guardErr
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

// carriesPrivateName reports whether anything a fixture for sig would put on disk carries one of the
// recorder's private names: its redacted text, its signature (written into the index), or its file
// name. The file name is a slug of the signature, which can rebuild a hyphenated name the signature
// only spells with other separators ("kiosk 7" becomes kiosk-7 in the slug), so it is checked on its
// own (ruling P3-R55 item 2).
func (rec *Recorder) carriesPrivateName(sig, text string) bool {
	return rec.matchesPrivateName(text) || rec.matchesPrivateName(sig) ||
		rec.matchesPrivateName(fixtureFileName(sig))
}

// existingFixture says what guardAndSave does when the store already holds the signature it saves.
type existingFixture int

const (
	// replaceExisting is for a tool's own result: Save's normal replace path overwrites the entry.
	replaceExisting existingFixture = iota
	// keepExisting is for the placeholder recordOneCall saves when the registry answered a dry run
	// with ErrNoDryRun before the middleware ran. The placeholder only says that there was no dry run,
	// so it never replaces a fixture the store already holds under the same signature, such as the
	// kubectl tool's real diff, which a shell dry run of the same kubectl apply line aliases (ruling
	// P3-R55 item 3).
	keepExisting
)

// storeHolds reports whether the store's index already has an entry for sig.
func storeHolds(s *Store, sig string) bool {
	return slices.ContainsFunc(s.Fixtures(), func(f Fixture) bool { return f.Signature == sig })
}

// guardAndSave is the one guard-and-save sequence every recorded result goes through, whether
// Middleware hands it over or recordOneCall saves it directly (a dry run of a tool with no DryRun,
// which the registry answers before the middleware ever runs, ruling P3-R41 item 7). The two paths
// once had separate guards, and the direct one skipped the private-name check on the signature (fix
// round 4); sharing this method keeps them from drifting apart again. In order: text is redacted
// before anything else sees it; then, under the recorder's mutex, a refusal already latched is
// returned again without saving (sticky, ruling P3-R35); a private name in the redacted text, in sig
// or in the file name sig would be saved under (see carriesPrivateName) latches a refusal and
// returns it; a call whose context is already done saves nothing and returns nil (ruling P3-R36: a
// fixture from a call the caller gave up on must not reach disk); with keepExisting, a signature the
// store already holds is left as it is and kept is true (ruling P3-R55 item 3); otherwise the text is
// saved, the first save failure is latched for SaveErr (P3-R36), and sig joins Saved() the first time
// it is saved, so saving one signature twice never lists it twice. Every error this returns leaves
// Refused or SaveErr non-nil too, for a caller whose error does not survive the registry.
func (rec *Recorder) guardAndSave(
	ctx context.Context, sig, text string, isErr bool, existing existingFixture,
) (kept bool, err error) {
	if rec.red != nil {
		text = rec.red.Redact(text)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.refused != nil {
		return false, rec.refused
	}
	if rec.carriesPrivateName(sig, text) {
		rec.refused = fmt.Errorf("%w: %s", ErrFixtureHostname, sig)
		return false, rec.refused
	}
	if ctx.Err() != nil {
		return false, nil
	}
	if existing == keepExisting && storeHolds(rec.store, sig) {
		return true, nil
	}
	if saveErr := rec.store.Save(sig, text, isErr); saveErr != nil {
		if rec.saveErr == nil {
			rec.saveErr = saveErr
		}
		return false, saveErr
	}
	if !slices.Contains(rec.saved, sig) {
		rec.saved = append(rec.saved, sig)
	}
	return false, nil
}

// Saved lists the signatures recorded, in order.
func (rec *Recorder) Saved() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.saved...)
}

// Refused is the first private-name refusal guardAndSave hit, through the middleware or a direct
// save, or nil if none did. A caller driving calls through a registry (tools.Registry.Execute and
// DryRun redact and rebuild every error as a fresh string on the way out, so a wrapped sentinel like
// ErrFixtureHostname does not survive them) checks this after each call instead of the error the
// registry returns.
func (rec *Recorder) Refused() error {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.refused
}

// SaveErr is the first error saving a fixture guardAndSave hit, through the middleware or a direct
// save, or nil if none did. Checked the same way, and for the same reason, as Refused (ruling
// P3-R36): a failure writing a fixture must abort the recording, not be logged as an ordinary
// recorded tool error.
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
// pod matching the label selector in the namespace, {{node}} the first node, and a phase=<value> term
// keeps only the objects in that status.phase ({{pod app=x ns=y phase=Pending}} picks the pod that
// failed to start, ruling P3-R61 C2). The label terms join into one -l flag and the phase terms into
// one --field-selector: kubectl does not combine either across repeated flags, so passing them
// separately would silently keep only the last one. kubectl's stderr is folded into the error.
func KubectlResolver(ctx context.Context, kind, spec string) (string, error) {
	argv := []string{"get", kind, "-o", "jsonpath={.items[0].metadata.name}"}
	var labels, fields []string
	for _, f := range strings.Fields(spec) {
		if ns, ok := strings.CutPrefix(f, "ns="); ok {
			argv = append(argv, "-n", ns)
		} else if phase, ok := strings.CutPrefix(f, "phase="); ok {
			fields = append(fields, "status.phase="+phase)
		} else {
			labels = append(labels, f)
		}
	}
	if len(labels) > 0 {
		argv = append(argv, "-l", strings.Join(labels, ","))
	}
	if len(fields) > 0 {
		argv = append(argv, "--field-selector", strings.Join(fields, ","))
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

// namesFromHost builds the short-name-derived entries of the default deny list from short (a real
// os.Hostname, or a synthetic one in a test): the name itself, and (ruling P3-R41 item 1) the label
// before its first dot, when it has one -- a macOS ".local" host or an FQDN-named Linux host is
// addressed by both forms across different tools, so both must be denied.
func namesFromHost(short string) []string {
	names := []string{short}
	if label, _, ok := strings.Cut(short, "."); ok && label != "" {
		names = append(names, label)
	}
	return names
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
	names := namesFromHost(short)
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

// parseResolvConf extracts the search and domain entries of a resolv.conf's content, with any
// trailing dot trimmed (ruling P3-R41 item 1: some resolvers write a search domain as "example.com.").
// A pure function of its input so the trimming is unit-testable without a real /etc/resolv.conf.
func parseResolvConf(data string) []string {
	var domains []string
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "search":
			for _, d := range fields[1:] {
				domains = append(domains, strings.TrimSuffix(d, "."))
			}
		case "domain":
			domains = append(domains, strings.TrimSuffix(fields[1], "."))
		}
	}
	return domains
}

// resolvSearchDomains reads the search and domain entries of /etc/resolv.conf, best effort: a
// missing or unreadable file yields none.
func resolvSearchDomains() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	return parseResolvConf(string(data))
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

// RecordTask records one task: it creates the fixture store's temporary directory, copies the
// workdir to a fresh run directory, runs the scenario's setup there, executes every recorded call
// through a registry with the record middleware into that temporary store, and runs the teardown
// whatever happened. scenariosRoot is evals/scenarios; out receives progress. The task directory and
// scenariosRoot may be relative, as `make record` passes them: both are made absolute before anything
// uses them (ruling P3-R55 item 1). Every error this returns is prefixed with the task's id (ruling
// P3-R41 item 6).
func RecordTask(ctx context.Context, t Task, scenariosRoot string, resolve Resolver, out io.Writer) error {
	if t.Record == nil {
		return fmt.Errorf("%s: no record block", t.ID)
	}
	t, scenariosRoot, err := absoluteRoots(t, scenariosRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
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
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	// The fixture store's temporary directory is created before setup runs (ruling P3-R41 item 6): a
	// read-only task directory then fails here, before any script has a chance to run.
	tempParent, err := os.MkdirTemp(t.Dir, "fixtures-recording-*")
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	// A missing workdir root is simply "no workdir"; anything else (a permission error, a symlink
	// copyDir refuses, ...) is a real failure the caller must see (ruling P3-R36).
	workdir := filepath.Join(t.Dir, "workdir")
	if _, err := os.Lstat(workdir); err == nil {
		if err := copyDir(workdir, runDir); err != nil {
			_ = os.RemoveAll(tempParent)
			return fmt.Errorf("%s: %w", t.ID, err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(tempParent)
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	env := append(os.Environ(), "EVAL_WORKDIR="+runDir, "EVAL_SCENARIO="+scenario, "EVAL_TASK="+t.ID)
	_, _ = fmt.Fprintf(out, "== %s: setup %s\n", t.ID, t.Record.Scenario)
	setupPgid, err := runScript(ctx, scenario, "setup.sh", env, out)
	if err != nil {
		killProcessGroup(setupPgid)
		_, _ = runScript(context.Background(), scenario, "teardown.sh", env, out)
		_ = os.RemoveAll(tempParent)
		return fmt.Errorf("%s: setup: %w", t.ID, err)
	}
	defer func() {
		_, _ = fmt.Fprintf(out, "== %s: teardown\n", t.ID)
		_, _ = runScript(context.Background(), scenario, "teardown.sh", env, out)
		// Killed only now, after teardown has had its chance to run (ruling P3-R41 item 3): a
		// background job setup.sh left behind (a stray `kubectl port-forward &`, say) must not
		// outlive the whole recording.
		killProcessGroup(setupPgid)
	}()
	return recordCalls(ctx, t, runDir, tempParent, resolve, names, out)
}

// absoluteRoots returns a copy of t whose Dir is absolute, and scenariosRoot made absolute (ruling
// P3-R55 item 1): every path RecordTask derives from them (the scripts, which sh runs with their
// scenario directory as its working directory, EVAL_SCENARIO, copyDir's source and the temporary
// store) must mean the same place whatever working directory resolves it.
func absoluteRoots(t Task, scenariosRoot string) (Task, string, error) {
	dir, err := filepath.Abs(t.Dir)
	if err != nil {
		return t, "", err
	}
	root, err := filepath.Abs(scenariosRoot)
	if err != nil {
		return t, "", err
	}
	abs := t
	abs.Dir = dir
	return abs, root, nil
}

// recordCalls executes the task's calls with a recording registry in runDir, into the fresh
// temporary store at tempParent, which it replaces the task's fixtures/ with only when every call
// succeeds (ruling P3-R36): an aborted or repeated run never leaves the real fixtures mixed with
// stale or partial entries. tempParent is removed once it is no longer needed -- except when the
// final swap itself fails, in which case it is left on disk (ruling P3-R41 item 2): swapFixtures has
// already restored fixtures/ to what it was, and deleting the freshly recorded set on top of that
// would lose it for nothing.
func recordCalls(
	ctx context.Context, t Task, runDir, tempParent string, resolve Resolver, names []string, out io.Writer,
) error {
	keepTempParent := false
	defer func() {
		if !keepTempParent {
			_ = os.RemoveAll(tempParent)
		}
	}()
	store, err := LoadFixtures(tempParent)
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	red, err := redact.New(redact.Options{Environ: os.Environ()})
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
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
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	if err := swapFixtures(filepath.Join(t.Dir, "fixtures"), store.Dir()); err != nil {
		keepTempParent = true
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	_, _ = fmt.Fprintf(out, "== %s: %d fixtures\n", t.ID, store.Len())
	return nil
}

// recordOneCall resolves one recorded call's placeholders, runs it, and reports the outcome, or
// returns the reason recordCalls must abort: a placeholder failure, a private-name refusal, a save
// error, or the call's context ending mid-run. All are checked after every call (ruling P3-R36),
// since the registry does not hand back a sentinel errors.Is can see for any of them; each aborting
// path logs the call's signature before returning (ruling P3-R41 item 6), so the log names exactly
// which call ended the run. A result saved here directly goes through the same guardAndSave as the
// middleware's and then through these same checks, so its refusal, save failure or cancellation
// aborts the run exactly as the middleware's would (fix round 4).
func recordOneCall(
	ctx context.Context, reg *tools.Registry, rec *Recorder, t Task, i int, call RecordCall,
	resolve Resolver, runDir string, out io.Writer,
) error {
	args, err := resolvePlaceholders(ctx, call.Args, resolve)
	if err != nil {
		return fmt.Errorf("%s: call %d: %w", t.ID, i, err)
	}
	sig := Signature(call.Tool, args)
	if call.DryRun {
		sig = DryRunSignature(call.Tool, args)
	}
	var text string
	if call.DryRun {
		text, err = reg.DryRun(ctx, call.Tool, args, runDir)
	} else {
		text, err = reg.Execute(ctx, call.Tool, args, runDir)
	}
	// A dry run's own tool can decide it has none for this call (kubectl/helm/terraform, for a verb
	// other than apply/upgrade/install): that runs through the middleware like any other dry run and
	// is already saved by it. A tool with no DryRun at all never reaches the middleware (Registry.DryRun
	// returns ErrNoDryRun before the wrap), so it is saved here instead. Either way this is exactly what
	// the live model sees as the call's result, so a fixture must exist for it (ruling P3-R41 item 7),
	// logged as recorded below, never skipped, so the count and the log agree. The placeholder saved
	// here never replaces a fixture already recorded under the same signature (keepExisting, ruling
	// P3-R55 item 3), and is logged as kept instead. guardAndSave's error is dropped on purpose: every
	// one it returns is latched on rec, and the checks below report it.
	kept := false
	if call.DryRun && errors.Is(err, tools.ErrNoDryRun) {
		if tool, ok := reg.Get(call.Tool); ok && tool.DryRun == nil {
			kept, _ = rec.guardAndSave(ctx, sig, err.Error(), true, keepExisting)
		}
	}
	if refused := rec.Refused(); refused != nil {
		_, _ = fmt.Fprintf(out, "   %-14s %s\n", "refused", sig)
		return fmt.Errorf("%s: %w", t.ID, refused)
	}
	if saveErr := rec.SaveErr(); saveErr != nil {
		_, _ = fmt.Fprintf(out, "   %-14s %s\n", "save failed", sig)
		return fmt.Errorf("%s: %w", t.ID, saveErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		_, _ = fmt.Fprintf(out, "   %-14s %s\n", "cancelled", sig)
		return fmt.Errorf("%s: %w", t.ID, ctxErr)
	}
	if kept {
		_, _ = fmt.Fprintf(out, "   %-14s %s (a dry-run placeholder does not replace a recorded fixture)\n", "kept", sig)
		return nil
	}
	status := "ok"
	if err != nil {
		status, text = "error recorded", err.Error()
	}
	_, _ = fmt.Fprintf(out, "   %-14s %s (%d bytes)\n", status, sig, len(text))
	return nil
}

// swapFixtures replaces finalDir with newDir: the previous fixtures move aside, the new ones move
// in, and only then does the aside copy get removed, so a failure partway through leaves either the
// old fixtures or the new ones intact, never a mix (ruling P3-R36). If the second rename fails, the
// aside copy moves straight back to finalDir before the error returns (ruling P3-R41 item 2): finalDir
// must never be left missing just because moving the new set in did not work.
func swapFixtures(finalDir, newDir string) error {
	asideDir := finalDir + ".replaced"
	_ = os.RemoveAll(asideDir)
	movedAside := false
	if _, err := os.Lstat(finalDir); err == nil {
		if err := os.Rename(finalDir, asideDir); err != nil {
			return fmt.Errorf("set aside the previous fixtures: %w", err)
		}
		movedAside = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(newDir, finalDir); err != nil {
		if movedAside {
			if rollbackErr := os.Rename(asideDir, finalDir); rollbackErr != nil {
				return fmt.Errorf("move the recorded fixtures into place: %w (restoring the previous fixtures also failed: %v)",
					err, rollbackErr)
			}
		}
		return fmt.Errorf("move the recorded fixtures into place: %w", err)
	}
	return os.RemoveAll(asideDir)
}

// runScript runs <dir>/<name> with sh in dir, streaming its output to out, and returns the process
// group id it ran in: runScript itself only kills that group if the context ends (see Cancel below),
// so a caller whose script exits normally but leaves a background job running (RecordTask, for
// setup.sh) is responsible for reaping the group once it is done with the scenario. A missing script
// is not an error. The script runs in its own process group so it is killable as a whole; canceling
// the context (the five-minute limit here, or the caller's own) kills that whole group via Cancel, not
// just the sh process. When out is not an *os.File, Go relays the child's output through a pipe on a
// goroutine, and a job the script backgrounds inherits that pipe: it can keep the pipe's write end
// open long after sh itself exits, so Wait's WaitDelay (scriptWaitDelay after sh exits) fires and Run
// returns exec.ErrWaitDelay even though sh's own exit status was fine (ruling P3-R41 item 3). That
// specific case -- ErrWaitDelay with a zero exit status -- is not a failure: sh did what it was asked,
// so it becomes a warning line on out instead of an error. The script's path is made absolute
// before sh sees it (ruling P3-R55 item 1): sh runs with dir as its working directory, so a relative
// path would be resolved from inside dir and not be found.
func runScript(ctx context.Context, dir, name string, env []string, out io.Writer) (pgid int, err error) {
	path, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		return 0, err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return 0, nil
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
	runErr := cmd.Run()
	if cmd.Process != nil {
		pgid = cmd.Process.Pid
	}
	if errors.Is(runErr, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 0 {
		_, _ = fmt.Fprintln(out, "setup left a background process running; its output is dropped")
		return pgid, nil
	}
	return pgid, runErr
}

// killProcessGroup sends SIGKILL to the process group pgid leads (runScript's Setpgid makes a
// script's own pid its process group id too), best effort (ruling P3-R41 item 3): a pgid of zero (no
// script ran) or any error from the kill, ESRCH (already gone) included, is silently ignored.
func killProcessGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
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
