package evals

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
)

func TestWithDefaults(t *testing.T) {
	o := withDefaults(RunOptions{})
	if o.Runs != 1 || o.Timeout != 10*time.Minute || o.HostLabel != "lab" || o.Out != io.Discard || o.Now == nil ||
		o.Think != "auto" || o.NumPredict != evalNumPredict {
		t.Fatalf("%+v", o)
	}
	set := RunOptions{Runs: 3, Timeout: time.Second, HostLabel: "gpu", Think: "off", NumPredict: -1}
	if k := withDefaults(set); k.Runs != 3 || k.Timeout != time.Second || k.HostLabel != "gpu" || k.Think != "off" ||
		k.NumPredict != -1 {
		t.Fatalf("set values are kept: %+v", k)
	}
}

// brokenTempDir points TMPDIR at a file, so creating a temporary directory fails. Call it after the
// test's own t.TempDir, whose root is then already made.
func brokenTempDir(t *testing.T) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", file)
}

func TestRunReportsAnEnvironmentItCannotIsolate(t *testing.T) {
	srv := fakeOllama(t)
	brokenTempDir(t)
	if restore, err := isolateEnv(); err == nil || restore != nil {
		t.Fatalf("isolateEnv: %v", err)
	}
	if _, err := Run(context.Background(), nil, runOptions(srv, "")); err == nil ||
		!strings.HasPrefix(err.Error(), "isolating the environment: ") {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunNeedsAnEngine(t *testing.T) {
	if _, err := Run(context.Background(), nil, RunOptions{Model: "gemma4:12b"}); err == nil || err.Error() != "host is required" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunStopsWhenTheWarmUpFails(t *testing.T) {
	srv := fakeOllama(t)
	srv.Turns = []ollamatest.Turn{{Status: 500, Error: "model crashed"}}
	_, tasks := corpusWithTriage(t)
	if _, err := Run(context.Background(), tasks, runOptions(srv, "")); err == nil || !strings.HasPrefix(err.Error(), "warm-up: ") {
		t.Fatalf("err = %v", err)
	}
	if prompts := servedPrompts(srv); len(prompts) != 1 {
		t.Fatalf("no task runs after a failed warm-up: %q", prompts)
	}
}

// TestRunStopsWhenTheWarmUpRunsAnotherModel: an engine that answers for the model but lists only
// another makes the warm-up's assistant fall back to that one, and the run stops before any model
// request (ruling P3-R49).
func TestRunStopsWhenTheWarmUpRunsAnotherModel(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(target)
	alias := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"other:1b","model":"other:1b"}]}`))
			return
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(alias.Close)
	opts := runOptions(srv, "")
	opts.Host = alias.URL
	if _, err := Run(context.Background(), tasks, opts); err == nil ||
		!strings.HasPrefix(err.Error(), "warm-up: the engine serves other:1b instead of gemma4:12b") {
		t.Fatalf("err = %v", err)
	}
	if prompts := servedPrompts(srv); len(prompts) != 0 {
		t.Fatalf("no model request goes out: %q", prompts)
	}
}

// cancelAfterRow cancels the run once the row of the task it names is printed.
type cancelAfterRow struct {
	bytes.Buffer
	id     string
	cancel context.CancelFunc
}

func (w *cancelAfterRow) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), w.id+" ") {
		w.cancel()
	}
	return w.Buffer.Write(p)
}

// TestRunStopsBetweenTasksWhenCancelled: a run cancelled after a task's row is printed starts no
// further task and returns the rows so far with the cancellation.
func TestRunStopsBetweenTasksWhenCancelled(t *testing.T) {
	tasks := promptedTasks(t, "a-first", "b-never")
	srv := fakeOllama(t, ollamatest.Turn{Content: "OOMKilled memory limit"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := runOptions(srv, "")
	opts.Out = &cancelAfterRow{id: "a-first", cancel: cancel}
	res, err := Run(ctx, tasks, opts)
	if !errors.Is(err, context.Canceled) || len(res.Tasks) != 1 || res.Tasks[0].ID != "a-first" {
		t.Fatalf("err %v, results %+v", err, res.Tasks)
	}
	for _, p := range servedPrompts(srv) {
		if strings.HasPrefix(p, "Task b-never.") {
			t.Fatal("the next task started after the cancellation")
		}
	}
}

func TestTranscriptWriterWarnsWithoutARunScope(t *testing.T) {
	file := filepath.Join(t.TempDir(), "runs")
	if err := os.WriteFile(file, []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts := withDefaults(RunOptions{RunsDir: file, Model: "gemma4:12b", Out: &out})
	w, closeTranscript := transcriptWriter(opts, "a-task", 1)
	defer closeTranscript()
	if w != io.Discard || !strings.HasPrefix(out.String(), "warning: transcripts are not written: ") {
		t.Fatalf("writer %T, output %q", w, out.String())
	}
}

// newTaskAssistant builds the assistant the runner would build for task on srv.
func newTaskAssistant(t *testing.T, task Task, opts RunOptions) *agent.Assistant {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	a, err := agent.New(assistantOptions(task, opts, t.TempDir(), NewReplay(emptyStore(), t.TempDir()), nil, io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckAssistantRefusesAnotherModeOrModel(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "other:1b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	opts := withDefaults(runOptions(srv, ""))
	opts.Model = "other:1b"
	a := newTaskAssistant(t, tasks[0], opts)
	operate := tasks[0]
	operate.Mode = "operate"
	if err := checkAssistant(a, operate, opts); err == nil || err.Error() != "the assistant runs in investigate mode, the task wants operate" {
		t.Fatalf("mode: %v", err)
	}
	opts.Model = "gemma4:12b" // configured, but the engine lists only other:1b, which New falls back to
	b := newTaskAssistant(t, tasks[0], opts)
	if err := checkAssistant(b, tasks[0], opts); err == nil || !strings.HasPrefix(err.Error(), "the engine serves other:1b instead of gemma4:12b") {
		t.Fatalf("model: %v", err)
	}
}

func TestCorpusDefectNamesEachSignatureOnce(t *testing.T) {
	s, err := LoadFixtures(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kubectl get pods", "x", false); err != nil {
		t.Fatal(err)
	}
	got := corpusDefect(s, []string{"kubectl get pods", "kubectl get pods"})
	want := `corpus defect: fixtures/` + fixtureFileName("kubectl get pods") + ` for "kubectl get pods" (unreadable)`
	if got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestRunTaskStopsOnASetupItCannotMake(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t)
	noHost := withDefaults(runOptions(srv, ""))
	noHost.Host = ""
	run := runTask(context.Background(), tasks[0], noHost, 1)
	if run.stop == nil || !strings.HasPrefix(run.row.Error, "setup defect: the assistant: ") {
		t.Fatalf("no engine: %+v", run.row)
	}
	brokenTempDir(t)
	if _, _, err := makeRunDir("taracode-eval-*"); err == nil {
		t.Fatal("makeRunDir")
	}
	run = runTask(context.Background(), tasks[0], withDefaults(runOptions(srv, "")), 1)
	if run.stop == nil || !strings.HasPrefix(run.row.Error, "setup defect: the run directory: ") {
		t.Fatalf("no run directory: %+v", run.row)
	}
}

func TestPrepareRunDirReportsWhatItCannotPrepare(t *testing.T) {
	file := filepath.Join(t.TempDir(), "task")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareRunDir(Task{Dir: file}, t.TempDir()); err == nil {
		t.Fatal("a task directory that is a file")
	}
	runFile := filepath.Join(t.TempDir(), "run")
	if err := os.WriteFile(runFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := prepareRunDir(Task{Dir: t.TempDir(), Mode: "operate"}, runFile); err == nil {
		t.Fatal("project storage in a file")
	}
	err := prepareRunDir(Task{Dir: t.TempDir(), Mode: "operate", Policy: "missing.yaml"}, t.TempDir())
	if !os.IsNotExist(err) {
		t.Fatalf("a missing policy file: %v", err)
	}
}

// TestPrepareRunDirForAnOperateTaskWithoutAPolicy: the project storage is made, and no policy file
// is written.
func TestPrepareRunDirForAnOperateTaskWithoutAPolicy(t *testing.T) {
	runDir := t.TempDir()
	if err := prepareRunDir(Task{Dir: t.TempDir(), Mode: "operate"}, runDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, ".taracode", "history")); err != nil {
		t.Fatalf("project storage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, ".taracode", "policy.yaml")); !os.IsNotExist(err) {
		t.Fatalf("no policy file: %v", err)
	}
}

func TestMatchesChecksTheSignature(t *testing.T) {
	e := ev("kubectl", "get", policy.Read, true, map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"})
	if matches(Matcher{SignatureMatches: "^kubectl delete"}, e) || !matches(Matcher{SignatureMatches: "^kubectl get pod"}, e) {
		t.Fatal("the signature decides")
	}
}
