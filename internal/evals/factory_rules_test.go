package evals

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// factoryResult is a results value a factory run writes: live, or sampled above temperature 0.
func factoryResult(live bool, temperature float64) Results {
	return Results{Taracode: "3.4.0", Model: "qwen3.8:27b", Tier: "32", Think: "auto", Date: "2026-10-22", Runs: 1,
		Host: "lab", Live: live, Temperature: temperature, Engine: catalogEngine(),
		Summary: Summary{PassRate: 0.9, MeanScore: 0.9, ByArea: map[string]AreaSummary{}}}
}

// TestBuildScoreboardSkipsSampledResults: a run sampled above temperature 0 is a factory result, never a row.
func TestBuildScoreboardSkipsSampledResults(t *testing.T) {
	sb := BuildScoreboard([]Results{factoryResult(false, 0.7)}, 33, "3.4.0")
	if len(sb.Skipped) != 1 || !strings.Contains(sb.Skipped[0], "qwen3.8:27b 2026-10-22: sampled at temperature 0.7, not a board result") {
		t.Fatalf("skipped = %v", sb.Skipped)
	}
	if len(sb.Tiers) != 0 {
		t.Fatalf("a sampled row was listed: %+v", sb.Tiers)
	}
}

// TestLintResultsRefusesASampledResult: the results lint names a sampled run as it names a live one.
func TestLintResultsRefusesASampledResult(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteResults(dir, factoryResult(false, 0.7)); err != nil {
		t.Fatal(err)
	}
	problems := LintResults(dir)
	if len(problems) != 1 || !strings.Contains(problems[0], "sampled at temperature 0.7") {
		t.Fatalf("problems = %v", problems)
	}
}

// TestFactoryResultsNamesLiveAndSampledFiles: FactoryResults lists the files eval report must refuse.
func TestFactoryResultsNamesLiveAndSampledFiles(t *testing.T) {
	dir := t.TempDir()
	live := factoryResult(true, 0)
	sampled := factoryResult(false, 0.7)
	sampled.Model = "gemma4:12b"
	board := factoryResult(false, 0)
	board.Model = "glm-4.7-flash"
	for _, r := range []Results{live, sampled, board} {
		if _, err := WriteResults(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	names, err := FactoryResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || !strings.Contains(names[0], "gemma4-12b") || !strings.Contains(names[1], "qwen3.8-27b") {
		t.Fatalf("factory results = %v, want the sampled and the live file", names)
	}
}

// TestIsolateEnvKeepsTheEnvironmentOfALiveRun: a live run needs the host's kubeconfig and home.
func TestIsolateEnvKeepsTheEnvironmentOfALiveRun(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/the-lab-kubeconfig")
	t.Setenv("HOME", "/tmp/the-lab-home")
	restore, err := isolateEnv(true)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if os.Getenv("KUBECONFIG") != "/tmp/the-lab-kubeconfig" || os.Getenv("HOME") != "/tmp/the-lab-home" {
		t.Fatalf("a live run lost its environment: KUBECONFIG %q, HOME %q", os.Getenv("KUBECONFIG"), os.Getenv("HOME"))
	}
	restoreReplay, err := isolateEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	defer restoreReplay()
	if os.Getenv("KUBECONFIG") == "/tmp/the-lab-kubeconfig" || os.Getenv("HOME") == "/tmp/the-lab-home" {
		t.Fatal("a replay run kept the host's environment")
	}
}

// fakeKubectlContext puts a kubectl on PATH whose current-context is the given name.
func fakeKubectlContext(t *testing.T, currentContext string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = config ] && [ \"$2\" = current-context ]; then echo " + currentContext + "; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func liveCorpus(t *testing.T) []Task {
	t.Helper()
	root := t.TempDir()
	writeTask(t, root, "live-echo", liveTask)
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

// TestLiveRunRefusesAKubeContextOtherThanTheLabel: the label must be the kubeconfig's current context.
func TestLiveRunRefusesAKubeContextOtherThanTheLabel(t *testing.T) {
	fakeKubectlContext(t, "kind-other")
	srv := fakeEngine(t, call("shell", map[string]any{"command": "echo factory-live"}))
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, "kind-taracode-evals"

	_, err := Run(context.Background(), liveCorpus(t), opts)

	if err == nil || !strings.Contains(err.Error(), "kind-other") || !strings.Contains(err.Error(), "kind-taracode-evals") {
		t.Fatalf("err = %v, want a refusal naming both contexts", err)
	}
	for _, r := range srv.Requests {
		if r.Path == "/v1/chat/completions" {
			t.Fatal("a chat request was sent: the refusal came too late")
		}
	}
}

// TestLiveRunAcceptsTheLabelledContext: the matching context lets the run go ahead.
func TestLiveRunAcceptsTheLabelledContext(t *testing.T) {
	fakeKubectlContext(t, "kind-taracode-evals")
	srv := fakeEngine(t, call("shell", map[string]any{"command": "echo factory-live"}),
		taraenginetest.Turn{Content: "The sandbox printed factory-live."})
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, " kind-taracode-evals "

	res, err := Run(context.Background(), liveCorpus(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Tasks[0].Pass || res.Sandbox != "kind-taracode-evals" {
		t.Fatalf("row %+v, sandbox %q", res.Tasks[0], res.Sandbox)
	}
}

// TestLiveRunRefusesATaskThatIsNotLive: --live runs live tasks only, never the public corpus for real.
func TestLiveRunRefusesATaskThatIsNotLive(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t)
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, "kind-taracode-evals"

	_, err := Run(context.Background(), tasks, opts)

	if err == nil || !strings.Contains(err.Error(), "crashloop-oomkilled") || !strings.Contains(err.Error(), "recorded") {
		t.Fatalf("err = %v, want a refusal naming the task and its provenance", err)
	}
}

// TestLiveRunRefusesAWhitespaceSandboxLabel: spaces are not a label.
func TestLiveRunRefusesAWhitespaceSandboxLabel(t *testing.T) {
	srv := fakeEngine(t)
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, "   "
	if _, err := Run(context.Background(), liveCorpus(t), opts); err == nil || !strings.Contains(err.Error(), "TARACODE_EVALS_LIVE_SANDBOX") {
		t.Fatalf("err = %v", err)
	}
}

// TestFactoryResultsReportsAnUnreadableDirectory: a results directory that cannot be read is an error.
func TestFactoryResultsReportsAnUnreadableDirectory(t *testing.T) {
	if _, err := FactoryResults(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing directory did not fail")
	}
}

// TestLiveRunWithoutKubectlStandsOnTheLabel: a Docker-only sandbox has no kubectl; the label alone suffices.
func TestLiveRunWithoutKubectlStandsOnTheLabel(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	srv := fakeEngine(t, call("shell", map[string]any{"command": "echo factory-live"}),
		taraenginetest.Turn{Content: "The sandbox printed factory-live."})
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, "compose-only"
	res, err := Run(context.Background(), liveCorpus(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sandbox != "compose-only" {
		t.Fatalf("sandbox %q", res.Sandbox)
	}
}

// TestLiveRunRefusesWhenKubectlReportsNoContext: a kubeconfig without a current context is not the sandbox.
func TestLiveRunRefusesWhenKubectlReportsNoContext(t *testing.T) {
	fakeKubectlContext(t, "")
	srv := fakeEngine(t)
	opts := runOptions(srv, "")
	opts.Live, opts.Sandbox = true, "kind-taracode-evals"
	if _, err := Run(context.Background(), liveCorpus(t), opts); err == nil || !strings.Contains(err.Error(), "reports none") {
		t.Fatalf("err = %v", err)
	}
}

// TestPrivateNamesInHandlesNoPatternsAndAnUnencodableTranscript: both answer with no names.
func TestPrivateNamesInHandlesNoPatternsAndAnUnencodableTranscript(t *testing.T) {
	if names := privateNamesIn(Transcript{Task: "t"}, nil); names != nil {
		t.Fatalf("no patterns found %v", names)
	}
	patterns := privateNamePatterns([]string{"lab-host-seven"})
	bad := Transcript{Task: "lab-host-seven", Score: TranscriptScore{Total: math.NaN()}}
	if names := privateNamesIn(bad, patterns); names != nil {
		t.Fatalf("an unencodable transcript found %v", names)
	}
}
