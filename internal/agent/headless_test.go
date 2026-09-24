package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

// TestNoBarePrintsOutsideProjectInit pins the output writer: the loop, the gate, the constructor
// and the model switch write to a.out (or the out writer New passes) so a headless caller can
// silence or capture them. InitProject is a command with its own progress lines and stays.
func TestNoBarePrintsOutsideProjectInit(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allow := map[string]bool{"project_init.go": true}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || allow[name] {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "fmt.Print") {
				t.Errorf("%s:%d: bare print; write to a.out or the out writer instead", name, i+1)
			}
		}
	}
}

func TestOutputWriterCarriesTheAnswerAndToolLines(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	var buf bytes.Buffer
	a.out = &buf
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}},
		{Content: "It says hello from disk."},
	}
	if err := a.ProcessMessage("What does hello.txt say?"); err != nil {
		t.Fatal(err)
	}
	// read_file's status line is "Read <file> (<n> lines)"; it names the file, not the tool.
	if out := buf.String(); !strings.Contains(out, "hello from disk") || !strings.Contains(out, "Read hello.txt") {
		t.Fatalf("output %q", out)
	}
}

func TestObserverSeesAllowedAndDeniedCallsAndLastTurnCounts(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = io.Discard
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{
			{Name: "read_file", Args: map[string]any{"path": "hello.txt"}},
			{Name: "kubectl", Args: map[string]any{"verb": "delete", "resource": "pod", "name": "x"}},
		}, PromptTokens: 100, CompletionTokens: 20},
		{Content: "done", PromptTokens: 150, CompletionTokens: 5},
	}
	if err := a.ProcessMessage("read then delete"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events %+v", events)
	}
	if !events[0].Allowed || events[0].Rule != "read" || events[0].Tool != "read_file" || events[0].Duration < 0 {
		t.Errorf("read event %+v", events[0])
	}
	if events[1].Allowed || events[1].Rule != "mode" || events[1].Verb != "delete" ||
		events[1].Classification != policy.Mutate || events[1].Reason == "" {
		t.Errorf("denied event %+v", events[1])
	}
	st := a.LastTurn()
	if st.Completions != 2 || st.ToolCalls != 2 || st.Denied != 1 || st.Truncated || st.Wall <= 0 ||
		st.PromptTokens != 250 || st.CompletionTokens != 25 {
		t.Errorf("turn %+v", st)
	}
}

func TestLastTurnMarksTheIterationCap(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = io.Discard
	a.maxIterations = 2
	call := ollamatest.Turn{ToolCalls: []ollamatest.ToolCall{{Name: "read_file", Args: map[string]any{"path": "hello.txt"}}}}
	srv.Turns = []ollamatest.Turn{call, call, {Content: "never reached"}}
	if err := a.ProcessMessage("loop"); err != nil {
		t.Fatal(err)
	}
	if st := a.LastTurn(); !st.Truncated || st.Completions != 2 || st.ToolCalls != 2 {
		t.Errorf("turn %+v", st)
	}
}

func TestPermissionDeciderReplacesThePrompt(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = io.Discard
	enterOperate(t, a)
	a.permissions, _, _ = policy.LoadPermissions("") // every tool asks
	var asked []string
	a.confirmPermission = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		asked = append(asked, inv.Tool)
		return ui.PermissionChoice{Allowed: false}
	}
	var events []ToolEvent
	a.observer = func(ev ToolEvent) { events = append(events, ev) }
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "write_file", Args: map[string]any{"path": "new.txt", "content": "x"}}}},
		{Content: "denied"},
	}
	if err := a.ProcessMessage("write"); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || len(events) != 1 || events[0].Allowed || events[0].Rule != "user" {
		t.Fatalf("asked=%v events=%+v", asked, events)
	}
	if _, err := os.Stat(filepath.Join(a.workingDir, "new.txt")); err == nil {
		t.Fatal("the file was written although the decider refused")
	}
}

func TestHeadlessOptionsReachTheAssistantThroughNew(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "shell", Args: map[string]any{"command": "echo hi"}}}},
		{Content: "replayed"},
	}
	var buf bytes.Buffer
	var events []ToolEvent
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir = srv.URL, "gemma4:12b", t.TempDir()
	opts.Ephemeral, opts.Spinner, opts.Streaming = true, false, false
	opts.Output = &buf
	opts.ToolObserver = func(ev ToolEvent) { events = append(events, ev) }
	opts.ToolMiddleware = func(_ tools.Call, _ tools.Executor) tools.Executor {
		return func(context.Context, map[string]any, string) (string, error) { return "from the middleware", nil }
	}
	opts.PermissionDecider = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		return ui.PermissionChoice{Allowed: true}
	}
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ProcessMessage("run echo"); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !events[0].Allowed || events[0].Tool != "shell" {
		t.Fatalf("events %+v", events)
	}
	if !strings.Contains(buf.String(), "replayed") {
		t.Fatalf("the answer did not reach the writer: %q", buf.String())
	}
	found := false
	for _, m := range a.conversation {
		if m.Role == openai.ChatMessageRoleTool && strings.Contains(m.Content, "from the middleware") {
			found = true
		}
	}
	if !found {
		t.Fatal("the model did not get the middleware's output as the tool result")
	}
}

// TestNilOutputFollowsStdoutAfterNew pins the nil Output: the assistant writes to os.Stdout as it is
// at each write, as the bare prints did, so an assistant built while os.Stdout was swapped (the cmd
// tests build theirs inside a capture) still prints to the stdout of the moment.
func TestNilOutputFollowsStdoutAfterNew(t *testing.T) {
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = []ollamatest.Turn{{Content: "printed to the current stdout"}}
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir = srv.URL, "gemma4:12b", t.TempDir()
	opts.Ephemeral, opts.Spinner, opts.Streaming = true, false, false
	var a *Assistant
	var err error
	_ = captureStdout(t, func() { a, err = New(opts) })
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := a.ProcessMessage("say it"); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "printed to the current stdout") {
		t.Fatalf("output %q", out)
	}
}

// TestPermissionDeciderReachesTheGateThroughNew covers the hook that
// TestHeadlessOptionsReachTheAssistantThroughNew sets but never consults (a read needs no
// permission): a write in operate mode asks New's decider, and its refusal stands.
func TestPermissionDeciderReachesTheGateThroughNew(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.taracode/policy.yaml from the machine running the test
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "write_file", Args: map[string]any{"path": "new.txt", "content": "x"}}}},
		{Content: "refused"},
	}
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Mode = srv.URL, "gemma4:12b", t.TempDir(), policy.ModeOperate
	opts.Spinner, opts.Streaming, opts.Output = false, false, io.Discard
	var asked []string
	opts.PermissionDecider = func(inv policy.Invocation, _ map[string]any) ui.PermissionChoice {
		asked = append(asked, inv.Tool)
		return ui.PermissionChoice{Allowed: false}
	}
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ProcessMessage("write"); err != nil {
		t.Fatal(err)
	}
	if a.Mode() != policy.ModeOperate || len(asked) != 1 || asked[0] != "write_file" {
		t.Fatalf("mode=%s asked=%v", a.Mode(), asked)
	}
	if _, err := os.Stat(filepath.Join(opts.WorkingDir, "new.txt")); err == nil {
		t.Fatal("the file was written although the decider refused")
	}
}
