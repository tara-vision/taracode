package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

// chatRequests counts the /api/chat requests the fake served.
func chatRequests(srv *ollamatest.Server) int {
	n := 0
	for _, r := range srv.Requests {
		if r.Path == "/api/chat" {
			n++
		}
	}
	return n
}

func TestProcessMessageContextStopsBeforeTheRequest(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = io.Discard
	srv.Turns = []ollamatest.Turn{{Content: "never asked"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.ProcessMessageContext(ctx, "hello"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if n := chatRequests(srv); n != 0 {
		t.Fatalf("%d chat requests after the cancel", n)
	}
}

// TestProcessMessageContextSkipsTheRemainingToolCalls cancels the turn while the first of two calls
// runs: the second never reaches the gate, gets a tool message of its own, and no further request
// goes to the model.
func TestProcessMessageContextSkipsTheRemainingToolCalls(t *testing.T) {
	a, srv := newTestAssistant(t, false)
	a.out = io.Discard
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []ToolEvent
	a.observer = func(ev ToolEvent) {
		events = append(events, ev)
		cancel()
	}
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{
			{Name: "read_file", Args: map[string]any{"path": "hello.txt"}},
			{Name: "list_files", Args: map[string]any{"path": "."}},
		}},
		{Content: "never asked"},
	}
	if err := a.ProcessMessageContext(ctx, "read, then list"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(events) != 1 || events[0].Tool != "read_file" || a.LastTurn().ToolCalls != 1 {
		t.Fatalf("events %+v, turn %+v", events, a.LastTurn())
	}
	var results []string
	for _, m := range a.conversation {
		if m.Role == openai.ChatMessageRoleTool {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 || !strings.Contains(results[1], "Not run: the turn was cancelled") {
		t.Fatalf("tool messages %q", results)
	}
	if n := chatRequests(srv); n != 1 {
		t.Fatalf("%d chat requests, want the first one only", n)
	}
}

type ctxKey struct{}

// TestProcessMessageContextReachesTheDryRunAndTheTool checks that the caller's context, not a
// background one, reaches both the mandatory dry run of a kubectl apply and the apply itself.
func TestProcessMessageContextReachesTheDryRunAndTheTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.taracode/policy.yaml from the machine running the test
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	srv.Turns = []ollamatest.Turn{
		{ToolCalls: []ollamatest.ToolCall{{Name: "kubectl", Args: map[string]any{
			"verb": "apply", "args": "-f app.yaml", "context": "kind-dev", "namespace": "shop"}}}},
		{Content: "applied"},
	}
	var mu sync.Mutex
	seen := map[bool]any{} // dry run or not -> the context value the executor saw
	opts := DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Mode = srv.URL, "gemma4:12b", t.TempDir(), policy.ModeOperate
	opts.Spinner, opts.Streaming, opts.Output = false, false, io.Discard
	opts.ToolMiddleware = func(call tools.Call, _ tools.Executor) tools.Executor {
		return func(ctx context.Context, _ map[string]any, _ string) (string, error) {
			mu.Lock()
			seen[call.DryRun] = ctx.Value(ctxKey{})
			mu.Unlock()
			return "ok", nil
		}
	}
	opts.PermissionDecider = func(policy.Invocation, map[string]any) ui.PermissionChoice {
		return ui.PermissionChoice{Allowed: true}
	}
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "the caller's")
	if err := a.ProcessMessageContext(ctx, "apply it"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen[true] != "the caller's" || seen[false] != "the caller's" {
		t.Fatalf("the executors saw %v", seen)
	}
}

func TestTurnContextKeepsTheCallersDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	parent, cancelParent := context.WithDeadline(context.Background(), deadline)
	defer cancelParent()
	ctx, cancel := turnContext(parent)
	defer cancel()
	if got, ok := ctx.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("deadline %v %v, want the caller's %v", got, ok, deadline)
	}
	ctx2, cancel2 := turnContext(context.Background())
	defer cancel2()
	got, ok := ctx2.Deadline()
	if !ok || time.Until(got) > apiResponseTimeout || time.Until(got) < apiResponseTimeout-time.Minute {
		t.Fatalf("default deadline %v %v", got, ok)
	}
}
