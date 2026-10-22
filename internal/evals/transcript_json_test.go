package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/llm"
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
		t.Fatalf("transcripts written: %v, want crashloop-oomkilled.json", keysOf(files))
	}
	var tr Transcript
	if err := json.Unmarshal(data, &tr); err != nil {
		t.Fatal(err)
	}
	checkTranscriptMessages(t, tr)
	checkTranscriptHeader(t, tr, res.Tasks[0])
	if !strings.Contains(string(data), `"tool_call_id"`) || !strings.Contains(string(data), `"safety_failure"`) {
		t.Fatalf("keys are not snake_case: %s", data)
	}
}

// checkTranscriptMessages asserts the message list of a triage transcript.
func checkTranscriptMessages(t *testing.T, tr Transcript) {
	t.Helper()
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
}

// checkTranscriptHeader asserts the header, the score and the usage of a triage transcript against its row.
func checkTranscriptHeader(t *testing.T, tr Transcript, row TaskResult) {
	t.Helper()
	if tr.Score.Pass != row.Pass || tr.Score.Total != row.Score {
		t.Fatalf("score %+v, row %+v", tr.Score, row)
	}
	// an eval run is offline, so an investigate task offers 11 tools: the 13 with a read form less the two web tools
	if len(tr.Tools) != 11 || tr.Task != "crashloop-oomkilled" || tr.Mode != "investigate" || tr.Model != "gemma4:12b" {
		t.Fatalf("header: tools %d, task %q, mode %q, model %q", len(tr.Tools), tr.Task, tr.Mode, tr.Model)
	}
	if tr.Usage.Completions != 2 || tr.Usage.PromptTokens != 220 || tr.Usage.CompletionTokens != 15 {
		t.Fatalf("usage = %+v", tr.Usage)
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

// TestRunWarnsWhenTranscriptsCannotBeWritten: a runs directory that is a file warns once, for the
// text transcript and the JSON one together, and the run goes on.
func TestRunWarnsWhenTranscriptsCannotBeWritten(t *testing.T) {
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, triageTurns()...)
	runsDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(runsDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts, opts.Out = true, &out

	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Tasks) != 1 || strings.Count(out.String(), "warning: transcripts are not written") != 1 {
		t.Fatalf("rows %d, output:\n%s", len(res.Tasks), out.String())
	}
}

// TestRunStopsOnUnreadableFixtures: a fixtures index that does not parse is a setup defect that stops the run.
func TestRunStopsOnUnreadableFixtures(t *testing.T) {
	root, tasks := corpusWithTriage(t)
	index := filepath.Join(root, "crashloop-oomkilled", "fixtures", "index.yaml")
	if err := os.WriteFile(index, []byte("fixtures: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t, triageTurns()...)

	_, err := Run(context.Background(), tasks, runOptions(srv, ""))

	if err == nil || !strings.Contains(err.Error(), "setup defect: the fixtures") {
		t.Fatalf("err = %v, want the fixtures setup defect", err)
	}
}

// fakeSource is a transcriptSource for the unit tests of buildTranscript.
type fakeSource struct {
	messages []openai.ChatCompletionMessage
	tools    []openai.Tool
}

func (f fakeSource) Conversation() []openai.ChatCompletionMessage { return f.messages }
func (f fakeSource) ToolDefinitions() []openai.Tool               { return f.tools }

// TestBuildTranscriptRecordsDeniedAndFailedCalls: a denied call keeps its rule and reason, a failed
// one its error, a missed one the signature the replay lacked.
func TestBuildTranscriptRecordsDeniedAndFailedCalls(t *testing.T) {
	task := Task{ID: "t", Area: "kubernetes", Mode: "operate"}
	src := fakeSource{messages: []openai.ChatCompletionMessage{{Role: "system", Content: "s"}}}
	calls := []loggedCall{
		{event: agent.ToolEvent{Tool: "kubectl", Args: map[string]any{"verb": "delete", "resource": "pod", "name": "a",
			"namespace": "kube-system"}, Allowed: false, Rule: "protected", Reason: "kube-system is protected"}},
		{event: agent.ToolEvent{Tool: "shell", Args: map[string]any{"command": "cat notes"}, Allowed: true,
			Err: errors.New("exit status 1")}, replayed: []ReplayCall{{Signature: "cat notes", Miss: true, Err: fmt.Errorf("%w: cat notes", ErrNoFixture)}}},
	}

	tr := buildTranscript(task, RunOptions{Model: "m"}, 1, src, nil, calls, TaskResult{})

	if tr.Calls[0].Decision != "denied" || tr.Calls[0].Rule != "protected" || tr.Calls[0].Reason != "kube-system is protected" {
		t.Fatalf("denied call = %+v", tr.Calls[0])
	}
	if tr.Calls[1].Decision != "allowed" || tr.Calls[1].Error != "exit status 1" || strings.Join(tr.Calls[1].Missed, ",") != "cat notes" {
		t.Fatalf("failed call = %+v", tr.Calls[1])
	}
}

// TestBuildTranscriptWithoutCallsWritesAnEmptyList: no calls is an empty list, never null.
func TestBuildTranscriptWithoutCallsWritesAnEmptyList(t *testing.T) {
	tr := buildTranscript(Task{ID: "t"}, RunOptions{}, 1, fakeSource{}, nil, nil, TaskResult{})
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"calls":[]`) {
		t.Fatalf("calls are not an empty list: %s", data)
	}
}

// TestWriteJSONTranscriptReportsWhatItCannotWrite: an unwritable directory and an unencodable score
// are errors, not panics or silence.
func TestWriteJSONTranscriptReportsWhatItCannotWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := runOptions(nil, file)
	if err := writeJSONTranscript(opts, "t", 1, Transcript{}); err == nil {
		t.Fatal("a runs directory that is a file did not fail")
	}
	opts = runOptions(nil, t.TempDir())
	bad := Transcript{Score: TranscriptScore{Total: math.NaN()}}
	if err := writeJSONTranscript(opts, "t", 1, bad); err == nil {
		t.Fatal("an unencodable score did not fail")
	}
}

func keysOf(m map[string][]byte) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestJSONTranscriptScrubsSignaturesAndMisses: the calls block is scrubbed like the messages.
func TestJSONTranscriptScrubsSignaturesAndMisses(t *testing.T) {
	home, _ := os.UserHomeDir()
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t,
		call("shell", map[string]any{"command": "cat " + home + "/notes/pods.txt"}),
		taraenginetest.Turn{Content: "OOMKilled memory limit"})
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts = true
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	for name, data := range jsonTranscripts(t, runsDir) {
		var tr Transcript
		if err := json.Unmarshal(data, &tr); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), home) {
			t.Fatalf("%s carries the home path", name)
		}
		if len(tr.Calls) != 1 || !strings.Contains(tr.Calls[0].Signature, "~/notes/pods.txt") ||
			len(tr.Calls[0].Missed) != 1 || !strings.Contains(tr.Calls[0].Missed[0], "~/notes/pods.txt") {
			t.Fatalf("calls = %+v, want the scrubbed signature and miss", tr.Calls)
		}
	}
}

// TestJSONTranscriptKeepsAddressesInToolCalls: a cluster address in a call is training data, not a
// secret; the engine's own address is still scrubbed.
func TestJSONTranscriptKeepsAddressesInToolCalls(t *testing.T) {
	root, _ := corpusWithTriage(t)
	store, _ := LoadFixtures(filepath.Join(root, "crashloop-oomkilled"))
	if err := store.Save("kubectl get pod -n shop", "see the engine at ENGINE for the list", false); err != nil {
		t.Fatal(err)
	}
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeEngine(t,
		call("shell", map[string]any{"command": "curl http://10.96.0.1/healthz"}),
		triageTurns()[0],
		taraenginetest.Turn{Content: "OOMKilled memory limit"})
	data, _ := os.ReadFile(filepath.Join(root, "crashloop-oomkilled", "fixtures", "index.yaml"))
	_ = data
	runsDir := t.TempDir()
	opts := runOptions(srv, runsDir)
	opts.JSONTranscripts = true
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	for _, raw := range jsonTranscripts(t, runsDir) {
		text := string(raw)
		if !strings.Contains(text, "curl http://10.96.0.1/healthz") {
			t.Fatalf("the cluster address was rewritten:\n%s", text)
		}
		if strings.Contains(text, srv.URL) {
			t.Fatalf("the engine address survived:\n%s", text)
		}
	}
}

// TestJSONTranscriptMarksPrivateNames: a transcript carrying a private name of the lab says so.
func TestJSONTranscriptMarksPrivateNames(t *testing.T) {
	t.Setenv(privateNamesEnvVar, "aramis,tara.lab")
	root, _ := corpusWithTriage(t)
	store, _ := LoadFixtures(filepath.Join(root, "crashloop-oomkilled"))
	if err := store.Save("kubectl get pod -n shop", "scheduled on node aramis", false); err != nil {
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
	for _, raw := range jsonTranscripts(t, runsDir) {
		var tr Transcript
		if err := json.Unmarshal(raw, &tr); err != nil {
			t.Fatal(err)
		}
		if strings.Join(tr.PrivateNames, ",") != "aramis" {
			t.Fatalf("private names = %v, want aramis", tr.PrivateNames)
		}
	}
}

// TestJSONTranscriptWritesNothingWithoutARunsDirectory: no runs directory, no transcript anywhere.
func TestJSONTranscriptWritesNothingWithoutARunsDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	_, tasks := corpusWithTriage(t)
	srv := fakeEngine(t, triageTurns()...)
	opts := runOptions(srv, "")
	opts.JSONTranscripts = true
	if _, err := Run(context.Background(), tasks, opts); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(cwd)
	if len(entries) != 0 {
		t.Fatalf("the working directory got %v", entries)
	}
}

// TestBuildTranscriptCarriesTheRowFlagsAndAlignment: truncated, timed out and error travel with the
// row; reasoning is attached only when every assistant message has its reply.
func TestBuildTranscriptCarriesTheRowFlagsAndAlignment(t *testing.T) {
	task := Task{ID: "t", Area: "kubernetes", Mode: "investigate"}
	src := fakeSource{messages: []openai.ChatCompletionMessage{
		{Role: "system", Content: "s"}, {Role: "user", Content: "u"}, {Role: "assistant", Content: "a"},
	}}
	row := TaskResult{Truncated: true, TimedOut: true, Error: "the turn timed out"}
	aligned := buildTranscript(task, RunOptions{}, 1, src, []llm.Result{{Content: "a", Thinking: "think"}}, nil, row)
	if !aligned.ReasoningAligned || aligned.Messages[2].Reasoning != "think" || !aligned.Truncated || !aligned.TimedOut ||
		aligned.Error != "the turn timed out" {
		t.Fatalf("aligned transcript = %+v", aligned)
	}
	shifted := buildTranscript(task, RunOptions{}, 1, src, []llm.Result{{Thinking: "one"}, {Thinking: "two"}}, nil, row)
	if shifted.ReasoningAligned || shifted.Messages[2].Reasoning != "" {
		t.Fatalf("shifted transcript attached reasoning: %+v", shifted)
	}
}
