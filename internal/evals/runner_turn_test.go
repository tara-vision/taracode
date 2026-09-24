package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// engineProxy stands between the runner and the ollamatest fake. It forwards every request, records
// the user prompt of each /api/chat in arrival order, and lets hook take a /api/chat request over:
// hook returns true when it answered or dropped the request itself.
type engineProxy struct {
	URL   string
	mu    sync.Mutex
	chats []string
}

type proxyHook func(w http.ResponseWriter, r *http.Request, prompt string, n int) bool

func newEngineProxy(t *testing.T, target string, hook proxyHook) *engineProxy {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	forward := httputil.NewSingleHostReverseProxy(u)
	p := &engineProxy{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			prompt := userPrompt(body)
			p.mu.Lock()
			p.chats = append(p.chats, prompt)
			n := len(p.chats)
			p.mu.Unlock()
			if hook != nil && hook(w, r, prompt, n) {
				return
			}
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	p.URL = srv.URL
	return p
}

// userPrompt is the first user message of a /api/chat body: the task's prompt.
func userPrompt(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	for _, m := range req.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

// servedPrompts are the user prompts of the /api/chat requests the fake itself received.
func servedPrompts(srv *ollamatest.Server) []string {
	var out []string
	for _, r := range srv.Requests {
		if r.Path != "/api/chat" {
			continue
		}
		messages, _ := r.Body["messages"].([]any)
		for _, m := range messages {
			if msg, _ := m.(map[string]any); msg["role"] == "user" {
				content, _ := msg["content"].(string)
				out = append(out, content)
				break
			}
		}
	}
	return out
}

// promptedTasks writes one triage task per id, each prompt starting with "Task <id>." so a proxy can
// tell their requests apart.
func promptedTasks(t *testing.T, ids ...string) []Task {
	t.Helper()
	root := t.TempDir()
	for _, id := range ids {
		writeTask(t, root, id, strings.NewReplacer("id: crashloop-oomkilled", "id: "+id,
			"prompt: Pods", "prompt: Task "+id+". Pods").Replace(goodTask))
	}
	tasks, err := LoadCorpus(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

// TestRunnerCancelsAndJoinsATimedOutTurn is the review's probe of ruling P3-R47, under -race: task
// A's model request is held until task B asks for its own. The runner must cancel A's turn at the
// limit and wait for it, so A's request never reaches the engine (an abandoned turn would get it
// released, and answered, next to B's), nothing of A arrives after Run, B runs to its end, and A's
// row is timed out with a wall time of at least the limit.
func TestRunnerCancelsAndJoinsATimedOutTurn(t *testing.T) {
	tasks := promptedTasks(t, "a-held", "b-next")
	srv := fakeOllama(t, ollamatest.Turn{Content: "OOMKilled memory limit"})
	var once sync.Once
	bAsked := make(chan struct{})
	var mu sync.Mutex
	aCancelled := false
	proxy := newEngineProxy(t, srv.URL, func(_ http.ResponseWriter, r *http.Request, prompt string, _ int) bool {
		switch {
		case strings.HasPrefix(prompt, "Task b-next."):
			once.Do(func() { close(bAsked) })
		case strings.HasPrefix(prompt, "Task a-held."):
			select {
			case <-r.Context().Done():
				mu.Lock()
				aCancelled = true
				mu.Unlock()
				return true
			case <-bAsked:
			}
		}
		return false
	})
	opts := runOptions(srv, "")
	opts.Host = proxy.URL
	opts.Timeout = 300 * time.Millisecond
	res, err := Run(context.Background(), tasks, opts)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // a turn left running would reach the engine here
	for _, p := range servedPrompts(srv) {
		if strings.HasPrefix(p, "Task a-held.") {
			t.Fatalf("task A's request reached the engine: %q", servedPrompts(srv))
		}
	}
	mu.Lock()
	cancelled := aCancelled
	mu.Unlock()
	if !cancelled {
		t.Fatal("task A's held request was never cancelled")
	}
	if a := res.Tasks[0]; a.ID != "a-held" || !a.TimedOut || a.Score != 0 || a.WallMs < 300 {
		t.Fatalf("A %+v", a)
	}
	if b := res.Tasks[1]; b.ID != "b-next" || b.TimedOut || b.Error != "" {
		t.Fatalf("B %+v", b)
	}
}

// TestRunnerStopsWhenTheParentIsCancelled cancels Run's context while task B's request is in flight:
// Run returns the task finished before it, not B, with the context's error, and never starts C.
func TestRunnerStopsWhenTheParentIsCancelled(t *testing.T) {
	tasks := promptedTasks(t, "a-first", "b-cancelled", "c-never")
	srv := fakeOllama(t, ollamatest.Turn{Content: "OOMKilled memory limit"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := newEngineProxy(t, srv.URL, func(_ http.ResponseWriter, r *http.Request, prompt string, _ int) bool {
		if !strings.HasPrefix(prompt, "Task b-cancelled.") {
			return false
		}
		cancel()
		<-r.Context().Done()
		return true
	})
	opts := runOptions(srv, "")
	opts.Host = proxy.URL
	res, err := Run(ctx, tasks, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(res.Tasks) != 1 || res.Tasks[0].ID != "a-first" || res.Summary.ByArea["kubernetes"].Tasks != 1 {
		t.Fatalf("results %+v", res)
	}
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	for _, p := range proxy.chats {
		if strings.HasPrefix(p, "Task c-never.") {
			t.Fatalf("the run went on after the cancel: %q", proxy.chats)
		}
	}
}

// TestJoinTurnGivesUpOnATurnThatDoesNotReturn covers the bounded grace of ruling P3-R47: a turn that
// ignores its cancellation is an error once the grace has passed, which stops the run.
func TestJoinTurnGivesUpOnATurnThatDoesNotReturn(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	turn, err := joinTurn(context.Background(), 20*time.Millisecond, 50*time.Millisecond, func(context.Context) error {
		<-release
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "did not return within") {
		t.Fatalf("err=%v", err)
	}
	if since := time.Since(start); since < 70*time.Millisecond || turn.wall < 70*time.Millisecond {
		t.Fatalf("gave up after %v (wall %v)", since, turn.wall)
	}
}

// TestJoinTurnTellsTheLimitFromTheParent: the limit ending a turn is a timeout; a cancelled parent
// is not; a turn that finishes in time is neither.
func TestJoinTurnTellsTheLimitFromTheParent(t *testing.T) {
	waitCtx := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	turn, err := joinTurn(context.Background(), 20*time.Millisecond, time.Second, waitCtx)
	if err != nil || !turn.timedOut || !errors.Is(turn.err, context.DeadlineExceeded) || turn.wall < 20*time.Millisecond {
		t.Fatalf("the limit: %+v %v", turn, err)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	turn, err = joinTurn(parent, time.Minute, time.Second, waitCtx)
	if err != nil || turn.timedOut || !errors.Is(turn.err, context.Canceled) {
		t.Fatalf("the parent: %+v %v", turn, err)
	}
	turn, err = joinTurn(context.Background(), time.Minute, time.Second, func(context.Context) error { return nil })
	if err != nil || turn.timedOut || turn.err != nil {
		t.Fatalf("in time: %+v %v", turn, err)
	}
}
