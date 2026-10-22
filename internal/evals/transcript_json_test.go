package evals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// triageTurns script a triage: the model thinks, calls kubectl, then answers.
func triageTurns() []taraenginetest.Turn {
	return []taraenginetest.Turn{
		{Thinking: "check the pod", ToolCalls: []taraenginetest.ToolCall{{Name: "kubectl",
			Args: map[string]any{"verb": "get", "resource": "pod", "namespace": "shop"}}}, PromptTokens: 100, CompletionTokens: 10},
		{Content: "OOMKilled memory limit", PromptTokens: 120, CompletionTokens: 5},
	}
}

// jsonTranscripts returns the JSON transcripts under runsDir, by file name.
func jsonTranscripts(t *testing.T, runsDir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	_ = filepath.WalkDir(runsDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") {
			data, readErr := os.ReadFile(path) //nolint:gosec // a transcript under the test's temp dir
			if readErr != nil {
				t.Fatal(readErr)
			}
			out[filepath.Base(path)] = data
		}
		return nil
	})
	return out
}

// TestJSONTranscriptCarriesTheWholeConversation covers --transcripts-json: every message with its
// reasoning, the tool calls and their results, the calls block, the score and the tools offered.
func TestJSONTranscriptCarriesTheWholeConversation(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, triageTurns()...)
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts = true

	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}

	files := jsonTranscripts(t, runsDir)
	data, ok := files["crashloop-oomkilled.json"]
	if !ok {
		t.Fatalf("transcripts written: %v, want crashloop-oomkilled.json", files)
	}
	var tr Transcript
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, m := range tr.Messages {
		roles = append(roles, m.Role)
	}
	if want := "system user assistant tool assistant"; strings.Join(roles, " ") != want {
		t.Fatalf("roles = %q, want %q", strings.Join(roles, " "), want)
	}
	if tr.Messages[2].Reasoning != "check the pod" {
		t.Fatalf("assistant reasoning = %q", tr.Messages[2].Reasoning)
	}
	if len(tr.Messages[2].ToolCalls) != 1 || tr.Messages[2].ToolCalls[0].ID == "" ||
		tr.Messages[3].ToolCallID != tr.Messages[2].ToolCalls[0].ID {
		t.Fatalf("tool call %+v does not pair with tool result %+v", tr.Messages[2].ToolCalls, tr.Messages[3])
	}
	if len(tr.Calls) != 1 || !strings.HasPrefix(tr.Calls[0].Signature, "kubectl ") || tr.Calls[0].Decision != "allowed" {
		t.Fatalf("calls = %+v", tr.Calls)
	}
	if tr.Score.Pass != res.Tasks[0].Pass || tr.Score.Total != res.Tasks[0].Score {
		t.Fatalf("score %+v, row %+v", tr.Score, res.Tasks[0])
	}
	// an eval run is offline, so an investigate task offers 11 tools: the 13 with a read form less the two web tools
	if len(tr.Tools) != 11 || tr.Task != "crashloop-oomkilled" || tr.Mode != "investigate" || tr.Model != "gemma4:12b" {
		t.Fatalf("header: tools %d, task %q, mode %q, model %q", len(tr.Tools), tr.Task, tr.Mode, tr.Model)
	}
	if tr.Usage.Completions != 2 || tr.Usage.PromptTokens != 220 || tr.Usage.CompletionTokens != 15 {
		t.Fatalf("usage = %+v", tr.Usage)
	}
	if !json.Valid(data) || !strings.Contains(string(data), `"tool_call_id"`) || !strings.Contains(string(data), `"safety_failure"`) {
		t.Fatalf("keys are not snake_case: %s", data)
	}
}

// TestJSONTranscriptNeverCarriesTheHomePath: a tool result holding the operator's home path is
// scrubbed the way the results' notes are.
func TestJSONTranscriptNeverCarriesTheHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, _ := corpusWithTriage(t)
	store, _ := LoadFixtures(filepath.Join(root, "crashloop-oomkilled"))
	if err := store.Save("kubectl get pod -n shop", "see "+home+"/notes/pods.txt for the list", false); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t, triageTurns()...)
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts = true

	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}

	for name, data := range jsonTranscripts(t, runsDir) {
		if strings.Contains(string(data), home) {
			t.Fatalf("%s carries the home path %q", name, home)
		}
		if !strings.Contains(string(data), "pods.txt") {
			t.Fatalf("%s lost the tool result itself", name)
		}
	}
}

// TestJSONTranscriptNamesRunsWhenRepeated: with --runs above 1 each repetition gets its own file.
func TestJSONTranscriptNamesRunsWhenRepeated(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	turns := append(triageTurns(), triageTurns()...)
	srv := fakeEngine(t, turns...)
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts = true
	opts.Runs = 2

	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}

	files := jsonTranscripts(t, runsDir)
	if _, ok := files["crashloop-oomkilled.run1.json"]; !ok {
		t.Fatalf("files %v lack run1", keysOf(files))
	}
	if _, ok := files["crashloop-oomkilled.run2.json"]; !ok {
		t.Fatalf("files %v lack run2", keysOf(files))
	}
}

func keysOf(m map[string][]byte) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
