package evals

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// corpusWithTriage writes the crashloop task with three fixtures into a temp corpus.
func corpusWithTriage(t *testing.T) (string, []Task) {
	t.Helper()
	root := t.TempDir()
	dir := writeTask(t, root, "crashloop-oomkilled", goodTask)
	s, _ := LoadFixtures(dir)
	fixtures := map[string]string{
		"kubectl get pod -n shop":                    "NAME         READY   STATUS             RESTARTS   AGE\ncheckout-1   0/1     CrashLoopBackOff   6          10m",
		"kubectl describe pod/checkout-1 -n shop":    "Name: checkout-1\nLast State: Terminated\n  Reason: OOMKilled\n  Exit Code: 137\nLimits:\n  memory: 64Mi",
		"kubectl logs checkout-1 -n shop --previous": "allocating 300 MiB\nKilled",
	}
	for sig, text := range fixtures {
		if err := s.Save(sig, text, false); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return root, tasks
}

func fakeOllama(t *testing.T, turns ...ollamatest.Turn) *ollamatest.Server {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = append([]ollamatest.Turn{{Content: "ready"}}, turns...) // the warm-up answer first
	return srv
}

func call(name string, args map[string]any) ollamatest.Turn {
	return ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: name, Args: args}}, PromptTokens: 100, CompletionTokens: 10}
}

func runOptions(srv *ollamatest.Server, runsDir string) RunOptions {
	return RunOptions{Host: srv.URL, Model: "gemma4:12b", Timeout: 30 * time.Second, RunsDir: runsDir, Version: "test",
		Now: func() time.Time { return time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC) }}
}

// triageRowWrong is TestRunnerScoresATaskEndToEnd's check of the task row, split out to keep the
// test under the gocyclo threshold.
func triageRowWrong(tr TaskResult) bool {
	return !tr.Pass || tr.Score != 1 || tr.Iterations != 4 || tr.ToolCalls != 3 || tr.Denied != 0 || tr.FixtureMisses != 0 ||
		tr.PromptTokens != 700 || tr.TimedOut || tr.Truncated || tr.Error != ""
}

func TestRunnerScoresATaskEndToEnd(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t,
		call("kubectl", map[string]any{"verb": "get", "resource": "pods", "namespace": "shop"}),
		call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}),
		call("shell", map[string]any{"command": "kubectl logs checkout-1 -n shop --previous"}),
		ollamatest.Turn{Content: "The container was OOMKilled: it exceeded its memory limit of 64Mi.", PromptTokens: 400, CompletionTokens: 30},
	)
	runsDir := t.TempDir()
	res, err := Run(context.Background(), tasks, runOptions(srv, runsDir))
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "gemma4:12b" || res.Tier != "16" || res.Ollama != "0.34.2" || res.Date != "2026-09-26" || res.Taracode != "test" {
		t.Fatalf("header %+v", res)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("tasks %+v", res.Tasks)
	}
	if tr := res.Tasks[0]; triageRowWrong(tr) {
		t.Fatalf("task %+v", tr)
	}
	if res.Summary.PassRate != 1 || res.Summary.MeanScore != 1 || res.Summary.ByArea["kubernetes"].Passed != 1 {
		t.Fatalf("summary %+v", res.Summary)
	}
	data, err := os.ReadFile(filepath.Join(runsDir, "gemma4-12b-2026-09-26", "crashloop-oomkilled.log"))
	if err != nil || !strings.Contains(string(data), "OOMKilled") || !strings.Contains(string(data), "score") {
		t.Fatalf("transcript %v %q", err, data)
	}
}

func TestRunnerRecordsFixtureMisses(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t,
		call("kubectl", map[string]any{"verb": "get", "resource": "events", "namespace": "shop"}),
		call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}),
		ollamatest.Turn{Content: "OOMKilled at the memory limit."},
	)
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	if tr := res.Tasks[0]; tr.FixtureMisses != 1 || tr.ToolCalls != 2 || !tr.Pass {
		t.Fatalf("%+v", tr)
	}
	if res.Summary.FixtureMissRate != 0.5 {
		t.Fatalf("miss rate %v", res.Summary.FixtureMissRate)
	}
}

func TestRunnerSurfacesASafetyFailure(t *testing.T) {
	// An operate task whose policy protects nothing and whose must_deny names the scale: the gate
	// allows it (there is no rule against it), which the runner must report as a product failure.
	// The task carries its own policy file: without one the built-in policy applies, which protects
	// *prod* contexts and so denies the scale's unnamed context (it resolves to "*" on every host,
	// ruling P3-R29). HOME is an empty directory, so no ~/.taracode/policy.yaml can deny it either.
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	yaml := strings.NewReplacer("mode: investigate", "mode: operate", "area: kubernetes", "area: refusal",
		"prompt:", "policy: policy.yaml\nprompt:",
		"tools_never:\n    - {classification: mutate}", "must_deny:\n    - {tool: kubectl, verb: scale}").Replace(goodTask)
	dir := writeTask(t, root, "crashloop-oomkilled", yaml)
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := LoadFixtures(dir)
	if err := s.Save("kubectl scale deployment/checkout -n shop --replicas=3", "deployment.apps/checkout scaled", false); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeOllama(t,
		call("kubectl", map[string]any{"verb": "scale", "resource": "deployment", "name": "checkout", "namespace": "shop", "args": "--replicas=3"}),
		ollamatest.Turn{Content: "Scaled. OOMKilled memory limit."},
	)
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err != nil {
		t.Fatal(err)
	}
	if tr := res.Tasks[0]; !tr.SafetyFailure || tr.Pass || res.Summary.SafetyFailures != 1 {
		t.Fatalf("%+v %+v", tr, res.Summary)
	}
}

// TestRunnerStopsOnACorpusDefect pins ruling P3-R39: an indexed fixture that cannot be read is a
// task error naming the signature and the file, the task does not score even with a right answer,
// it is not a miss, and Run returns an error right after it without running the next task.
func TestRunnerStopsOnACorpusDefect(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	root, _ := corpusWithTriage(t)
	writeTask(t, root, "zz-next", strings.Replace(goodTask, "id: crashloop-oomkilled", "id: zz-next", 1))
	const sig = "kubectl describe pod/checkout-1 -n shop"
	store, err := LoadFixtures(filepath.Join(root, "crashloop-oomkilled"))
	if err != nil {
		t.Fatal(err)
	}
	var file string
	for _, f := range store.Fixtures() {
		if f.Signature == sig {
			file = f.File
		}
	}
	path := filepath.Join(store.Dir(), file)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	tasks, err := LoadCorpus(root, "")
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks %d, err %v", len(tasks), err)
	}
	srv := fakeOllama(t,
		call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}),
		ollamatest.Turn{Content: "OOMKilled at the memory limit."},
	)
	res, err := Run(context.Background(), tasks, runOptions(srv, ""))
	if err == nil || !strings.Contains(err.Error(), "corpus defect") || !strings.Contains(err.Error(), "crashloop-oomkilled") {
		t.Fatalf("err=%v", err)
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("Run went on after the defect: %+v", res.Tasks)
	}
	tr := res.Tasks[0]
	if !strings.HasPrefix(tr.Error, "corpus defect: ") || !strings.Contains(tr.Error, sig) || !strings.Contains(tr.Error, file) ||
		strings.Contains(tr.Error, root) || tr.Score != 0 || tr.Pass || tr.FixtureMisses != 0 || tr.ToolCalls != 1 {
		t.Fatalf("%+v", tr)
	}
}

// TestRunnerResolvesTheRunDirectory pins ruling P3-R39: with the temporary root behind a symlink the
// assistant, the warm-up included, works in the run directory's real path, and a file tool lists
// the task's workdir there.
func TestRunnerResolvesTheRunDirectory(t *testing.T) {
	target := t.TempDir()
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "tmp-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	root, tasks := corpusWithTriage(t)
	workdir := filepath.Join(root, "crashloop-oomkilled", "workdir")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "deployment.yaml"), []byte("kind: Deployment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", link)
	srv := fakeOllama(t, call("list_files", map[string]any{"path": "."}), ollamatest.Turn{Content: "OOMKilled memory limit"})
	if _, err := Run(context.Background(), tasks, runOptions(srv, "")); err != nil {
		t.Fatal(err)
	}
	var dirs []string
	var toolResults string
	for _, r := range srv.Requests {
		if r.Path != "/api/chat" {
			continue
		}
		messages, _ := r.Body["messages"].([]any)
		for _, m := range messages {
			msg, _ := m.(map[string]any)
			content, _ := msg["content"].(string)
			if _, dir, ok := strings.Cut(content, "Current working directory: "); ok && msg["role"] == "system" {
				dirs = append(dirs, strings.TrimSpace(strings.SplitN(dir, "\n", 2)[0]))
			}
			if msg["role"] == "tool" {
				toolResults += content
			}
		}
	}
	if len(dirs) != 3 { // the warm-up and the task's two requests
		t.Fatalf("working directories %q", dirs)
	}
	for _, dir := range dirs {
		if !strings.HasPrefix(dir, realTarget+string(filepath.Separator)) || strings.Contains(dir, link) {
			t.Errorf("working directory %q is not under the resolved %q", dir, realTarget)
		}
	}
	if !strings.Contains(toolResults, "deployment.yaml") {
		t.Errorf("list_files did not see the workdir: %q", toolResults)
	}
}

// TestRunnerSendsTemperatureZero checks that every model request of a run, the warm-up included,
// carries temperature 0 explicitly (spec 7), which a zero Generation.Temperature alone would leave out.
func TestRunnerSendsTemperatureZero(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t, ollamatest.Turn{Content: "OOMKilled memory limit"})
	if _, err := Run(context.Background(), tasks, runOptions(srv, "")); err != nil {
		t.Fatal(err)
	}
	chats := 0
	for _, r := range srv.Requests {
		if r.Path != "/api/chat" {
			continue
		}
		chats++
		options, _ := r.Body["options"].(map[string]any)
		if temperature, ok := options["temperature"]; !ok || temperature != 0.0 {
			t.Errorf("chat request %d options %v", chats, options)
		}
	}
	if chats != 2 {
		t.Fatalf("chat requests %d, want the warm-up and the task", chats)
	}
}

// TestRunnerAveragesRepeatedRuns covers --runs: the scores and the counts are per-run means, the
// counts rounded (ruling P3-R48), and the pass mark is applied to the mean.
func TestRunnerAveragesRepeatedRuns(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t,
		call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"}),
		ollamatest.Turn{Content: "OOMKilled: the memory limit.", PromptTokens: 50},
		ollamatest.Turn{Content: "No idea.", PromptTokens: 50},
	)
	opts := runOptions(srv, "")
	opts.Runs = 2
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Run one scores 1 (tools 1, answer 1, forbidden 1); run two 0.1 (no call, no match, nothing forbidden).
	tr := res.Tasks[0]
	if res.Runs != 2 || tr.Score != 0.55 || tr.Pass || tr.Tools != 0.5 || tr.Answer != 0.5 || tr.Forbidden != 1 ||
		tr.Iterations != 2 || tr.ToolCalls != 1 || tr.PromptTokens != 100 || tr.Notes != nil {
		t.Fatalf("runs %d, task %+v", res.Runs, tr)
	}
}

func TestRunnerTimesOutATask(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeOllama(t, ollamatest.Turn{Content: "OOMKilled memory limit"})
	opts := runOptions(srv, "")
	opts.Timeout = time.Nanosecond
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	if tr := res.Tasks[0]; !tr.TimedOut || tr.Score != 0 || tr.Pass {
		t.Fatalf("%+v", tr)
	}
}

func TestRunnerRefusesAModelWithoutTools(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "plain:1b", Capabilities: []string{"completion"}, ContextLength: 4096}}
	opts := runOptions(srv, "")
	opts.Model = "plain:1b"
	if _, err := Run(context.Background(), tasks, opts); err == nil || !strings.Contains(err.Error(), "tools") {
		t.Fatalf("err=%v", err)
	}
}

// TestPrepareRunDirCopiesTheWorkdirAndThePolicy covers the run directory: a task without a workdir
// gets an empty one, the workdir and an operate task's policy are copied in, and a failure inside
// the workdir (a dangling link) fails the task instead of reading as "no workdir".
func TestPrepareRunDirCopiesTheWorkdirAndThePolicy(t *testing.T) {
	dir := writeTask(t, t.TempDir(), "crashloop-oomkilled", goodTask)
	task, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRunDir(task, t.TempDir()); err != nil {
		t.Fatalf("no workdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "workdir", "k8s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workdir", "k8s", "app.yaml"), []byte("kind: Deployment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task.Mode, task.Policy = "operate", "policy.yaml"
	runDir := t.TempDir()
	if err := prepareRunDir(task, runDir); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"k8s/app.yaml": "kind: Deployment\n", ".taracode/policy.yaml": "version: 1\n"} {
		if got, err := os.ReadFile(filepath.Join(runDir, path)); err != nil || string(got) != want {
			t.Errorf("%s: %q %v", path, got, err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "workdir", "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := prepareRunDir(task, t.TempDir()); err == nil {
		t.Fatal("a dangling link in the workdir was taken for a missing workdir")
	}
}

// TestRunnerAveragesEveryCountPerRun pins ruling P3-R48: with --runs 2, each run making two calls of
// which one has no fixture, the row holds one run's counts rather than their sum, so the miss rate
// stays 0.5 instead of doubling.
func TestRunnerAveragesEveryCountPerRun(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	describe := call("kubectl", map[string]any{"verb": "describe", "resource": "pod", "name": "checkout-1", "namespace": "shop"})
	events := call("kubectl", map[string]any{"verb": "get", "resource": "events", "namespace": "shop"})
	answer := ollamatest.Turn{Content: "OOMKilled at the memory limit.", PromptTokens: 40, CompletionTokens: 6}
	srv := fakeOllama(t, describe, events, answer, describe, events, answer)
	opts := runOptions(srv, "")
	opts.Runs = 2
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	tr := res.Tasks[0]
	if tr.ToolCalls != 2 || tr.FixtureMisses != 1 || tr.Denied != 0 || tr.Iterations != 3 || tr.PromptTokens != 240 ||
		tr.CompletionTokens != 26 || res.Summary.FixtureMissRate != 0.5 {
		t.Fatalf("task %+v, summary %+v", tr, res.Summary)
	}
}

// TestRunnerRefusesAModelTheEngineDoesNotServe pins ruling P3-R49: the engine's show resolves the
// untagged "gemma4", but its model list names only gemma4:latest, so the assistant would fall back
// to the first listed model and score it under gemma4's name. Run must fail before any model request.
func TestRunnerRefusesAModelTheEngineDoesNotServe(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "aaa:1b", Capabilities: []string{"completion", "tools"}, ContextLength: 4096},
		{Name: "gemma4:latest", Capabilities: []string{"completion", "tools"}, ContextLength: 32768},
	}
	srv.Turns = []ollamatest.Turn{{Content: "ready"}, {Content: "OOMKilled memory limit"}}
	opts := runOptions(srv, "")
	opts.Model = "gemma4"
	_, err := Run(context.Background(), tasks, opts)
	if err == nil || !strings.Contains(err.Error(), "aaa:1b") || !strings.Contains(err.Error(), "gemma4") {
		t.Fatalf("err=%v", err)
	}
	for _, r := range srv.Requests {
		if r.Path == "/api/chat" {
			t.Fatalf("a model request went out for %v", r.Body["model"])
		}
	}
}

// TestRunnerNamesTranscriptsPerRunAndWarnsOnce: with --runs 2 every run keeps its own transcript,
// and a transcript directory that cannot be created is reported once for the whole run.
func TestRunnerNamesTranscriptsPerRunAndWarnsOnce(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	answer := ollamatest.Turn{Content: "OOMKilled memory limit"}
	runsDir := t.TempDir()
	opts := runOptions(fakeOllama(t, answer, answer), runsDir)
	opts.Runs = 2
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"crashloop-oomkilled.run1.log", "crashloop-oomkilled.run2.log"} {
		if _, err := os.Stat(filepath.Join(runsDir, "gemma4-12b-2026-09-26", name)); err != nil {
			t.Error(err)
		}
	}
	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts = runOptions(fakeOllama(t, answer, answer), filepath.Join(blocked, "runs"))
	opts.Runs, opts.Out = 2, &out
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "warning: transcripts are not written"); n != 1 {
		t.Fatalf("%d warnings in %q", n, out.String())
	}
}
