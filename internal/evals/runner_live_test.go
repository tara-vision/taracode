package evals

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// liveTask has no fixtures: a live run executes its shell call for real in the run directory.
const liveTask = `id: live-echo
area: docker
mode: investigate
provenance: files
prompt: Print the sandbox marker with echo and report it.
expect:
  tools_called_any:
    - {tool: shell}
  answer_matches: ["factory-live"]
  max_iterations: 4
`

// TestLiveRunExecutesToolsForReal covers --live: the shell call runs for real and its output
// reaches the model, no fixture is consulted, the results say live and the transcript header too.
func TestLiveRunExecutesToolsForReal(t *testing.T) {
	root := t.TempDir()
	writeTask(t, root, "live-echo", liveTask)
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t, call("shell", map[string]any{"command": "echo factory-live"}),
		taraenginetest.Turn{Content: "The sandbox printed factory-live."})
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.Live, opts.Sandbox = true, "kind-taracode-evals"

	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}

	if !res.Live || res.Sandbox != "kind-taracode-evals" {
		t.Fatalf("the results do not say live in the sandbox: live %v, sandbox %q", res.Live, res.Sandbox)
	}
	row := res.Tasks[0]
	if !row.Pass || row.FixtureMisses != 0 || row.RealCalls != 0 || row.Error != "" {
		t.Fatalf("row = %+v, want a pass with no fixture accounting", row)
	}
	if !toolResultReachedTheModel(srv, "factory-live") {
		t.Fatal("the echo output never reached the model: the tool did not run for real")
	}
	if first := firstTranscriptLine(t, runsDir); !strings.HasSuffix(first, "(live)") {
		t.Fatalf("transcript header %q does not end with (live)", first)
	}
}

// TestLintResultsRefusesALiveResult: a live result in a results directory is a problem the lint names.
func TestLintResultsRefusesALiveResult(t *testing.T) {
	dir := t.TempDir()
	r := Results{Taracode: "3.4.0", Model: "qwen3.8:27b", Tier: "32", Think: "auto", Date: "2026-10-22", Runs: 1,
		Host: "lab", Live: true}
	if _, err := WriteResults(dir, r); err != nil {
		t.Fatal(err)
	}
	problems := LintResults(dir)
	if len(problems) != 1 || !strings.Contains(problems[0], "live run") || !strings.Contains(problems[0], "qwen3.8:27b") {
		t.Fatalf("problems = %v, want one naming the live run", problems)
	}
}

// toolResultReachedTheModel reports whether a chat request carried a tool message holding marker.
func toolResultReachedTheModel(srv *taraenginetest.Server, marker string) bool {
	for _, r := range srv.Requests {
		if r.Path != "/v1/chat/completions" {
			continue
		}
		messages, _ := r.Body["messages"].([]any)
		for _, m := range messages {
			msg, _ := m.(map[string]any)
			content, _ := msg["content"].(string)
			if msg["role"] == "tool" && strings.Contains(content, marker) {
				return true
			}
		}
	}
	return false
}

// firstTranscriptLine returns the first line of the only .log transcript under runsDir.
func firstTranscriptLine(t *testing.T, runsDir string) string {
	t.Helper()
	var logs []string
	_ = filepath.WalkDir(runsDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".log") {
			logs = append(logs, path)
		}
		return nil
	})
	if len(logs) != 1 {
		t.Fatalf("transcripts under %s: %v, want one", runsDir, logs)
	}
	data, err := os.ReadFile(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	return strings.SplitN(string(data), "\n", 2)[0]
}

// TestLiveRunRefusesWithoutASandbox: a live run executes real mutations wherever the environment
// points, so it fails closed unless the operator names the sandbox the machine is; nothing runs.
func TestLiveRunRefusesWithoutASandbox(t *testing.T) {
	root := t.TempDir()
	writeTask(t, root, "live-echo", liveTask)
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t, call("shell", map[string]any{"command": "echo factory-live"}))
	opts := runOptions(srv, "")
	opts.Live = true

	_, err = Run(context.Background(), tasks, opts)

	if err == nil || !strings.Contains(err.Error(), "TARACODE_EVALS_LIVE_SANDBOX") {
		t.Fatalf("err = %v, want a refusal naming the sandbox variable", err)
	}
	for _, r := range srv.Requests {
		if r.Path == "/v1/chat/completions" {
			t.Fatal("a chat request was sent: the refusal came too late")
		}
	}
}
