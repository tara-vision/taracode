package evals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/tools/redact"
)

// ErrFixtureHostname says a recorded output carried the host name the recorder runs on.
var ErrFixtureHostname = errors.New("the fixture would carry the recorder's host name; fix the scenario")

// Recorder captures real tool results into a Store, redacted, for a task's fixtures (spec 6.1).
type Recorder struct {
	store    *Store
	red      *redact.Redactor
	hostname string

	mu    sync.Mutex
	saved []string
}

// NewRecorder returns a recorder that redacts with red (nil = no redaction, tests only) and refuses
// any output containing hostname ("" = no check).
func NewRecorder(store *Store, red *redact.Redactor, hostname string) *Recorder {
	return &Recorder{store: store, red: red, hostname: hostname}
}

// Middleware runs the tool for real and saves what came back under the call's signature: the
// output, or the error text with Error set, redacted before it touches the disk.
func (rec *Recorder) Middleware(call tools.Call, next tools.Executor) tools.Executor {
	return func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
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
		if rec.hostname != "" && strings.Contains(text, rec.hostname) {
			return "", fmt.Errorf("%w: %s", ErrFixtureHostname, sig)
		}
		if saveErr := rec.store.Save(sig, text, isErr); saveErr != nil {
			return "", saveErr
		}
		rec.mu.Lock()
		rec.saved = append(rec.saved, sig)
		rec.mu.Unlock()
		return out, err
	}
}

// Saved lists the signatures recorded, in order.
func (rec *Recorder) Saved() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.saved...)
}

// Resolver answers a placeholder: kind is "pod" or "node", spec the text after it.
type Resolver func(ctx context.Context, kind, spec string) (string, error)

var placeholder = regexp.MustCompile(`\{\{\s*(pod|node)([^}]*)\}\}`)

// resolvePlaceholders replaces every placeholder in the string arguments of a call.
func resolvePlaceholders(ctx context.Context, args map[string]any, resolve Resolver) (map[string]any, error) {
	out := make(map[string]any, len(args))
	for k, v := range args {
		s, ok := v.(string)
		if !ok || !placeholder.MatchString(s) {
			out[k] = v
			continue
		}
		if resolve == nil {
			return nil, fmt.Errorf("argument %s has a placeholder but no resolver", k)
		}
		var firstErr error
		s = placeholder.ReplaceAllStringFunc(s, func(m string) string {
			sub := placeholder.FindStringSubmatch(m)
			val, err := resolve(ctx, sub[1], strings.TrimSpace(sub[2]))
			if err != nil && firstErr == nil {
				firstErr = err
			}
			return val
		})
		if firstErr != nil {
			return nil, firstErr
		}
		out[k] = s
	}
	return out, nil
}

// KubectlResolver resolves placeholders against the live cluster: {{pod app=x ns=y}} is the first
// pod matching the label selector in the namespace, {{node}} the first node.
func KubectlResolver(ctx context.Context, kind, spec string) (string, error) {
	argv := []string{"get", kind, "-o", "jsonpath={.items[0].metadata.name}"}
	for _, f := range strings.Fields(spec) {
		if ns, ok := strings.CutPrefix(f, "ns="); ok {
			argv = append(argv, "-n", ns)
		} else {
			argv = append(argv, "-l", f)
		}
	}
	out, err := exec.CommandContext(ctx, "kubectl", argv...).Output()
	if err != nil {
		return "", fmt.Errorf("resolve {{%s %s}}: %w", kind, spec, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("resolve {{%s %s}}: nothing matched", kind, spec)
	}
	return name, nil
}

// RecordTask records one task: it copies the workdir to a fresh directory, runs the scenario's
// setup there, executes every recorded call through a registry with the record middleware, and
// runs the teardown whatever happened. scenariosRoot is evals/scenarios; out receives progress.
func RecordTask(ctx context.Context, t Task, scenariosRoot string, resolve Resolver, out io.Writer) error {
	if t.Record == nil {
		return fmt.Errorf("%s: no record block", t.ID)
	}
	scenario := filepath.Join(scenariosRoot, t.Record.Scenario)
	if _, err := os.Stat(filepath.Join(scenario, "setup.sh")); err != nil {
		return fmt.Errorf("%s: scenario %s has no setup.sh", t.ID, t.Record.Scenario)
	}
	runDir, err := os.MkdirTemp("", "taracode-record-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(runDir) }()
	if err := copyDir(filepath.Join(t.Dir, "workdir"), runDir); err != nil && !os.IsNotExist(err) {
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
	return recordCalls(ctx, t, runDir, resolve, out)
}

// recordCalls executes the task's calls with a recording registry in runDir.
func recordCalls(ctx context.Context, t Task, runDir string, resolve Resolver, out io.Writer) error {
	store, err := LoadFixtures(t.Dir)
	if err != nil {
		return err
	}
	red, err := redact.New(redact.Options{Environ: os.Environ()})
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	rec := NewRecorder(store, red, hostname)
	reg := tools.NewBuiltinRegistry(tools.Options{Redactor: red, Middleware: rec.Middleware}, tools.Config{})
	for i, call := range t.Record.Calls {
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
		if errors.Is(err, ErrFixtureHostname) {
			return fmt.Errorf("%s: %w", t.ID, err)
		}
		status := "ok"
		if err != nil {
			status, text = "error recorded", err.Error()
		}
		_, _ = fmt.Fprintf(out, "   %-14s %s (%d bytes)\n", status, Signature(call.Tool, args), len(text))
	}
	_, _ = fmt.Fprintf(out, "== %s: %d fixtures\n", t.ID, store.Len())
	return nil
}

// runScript runs <dir>/<name> with sh in dir, streaming its output to out, under a five-minute
// limit. A missing script is not an error.
func runScript(ctx context.Context, dir, name string, env []string, out io.Writer) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", path)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
	return cmd.Run()
}

// copyDir copies src into dst (files and directories, no symlinks followed).
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755) //nolint:gosec // dst is a fresh recorder run directory
		}
		data, err := os.ReadFile(p) //nolint:gosec // p walks the task's own workdir, a trusted corpus path
		if err != nil {
			return err
		}
		info, _ := d.Info()
		return os.WriteFile(target, data, info.Mode().Perm()) //nolint:gosec // dst is a fresh recorder run directory
	})
}
