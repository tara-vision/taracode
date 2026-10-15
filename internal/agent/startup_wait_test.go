package agent

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm"
)

// TestNewWaitsForAStartingEngine: a session started while the engine fetches its runtime prints each step the engine
// reports, once, before it reads and loads the model; the engine is ready by the time the model is read.
func TestNewWaitsForAStartingEngine(t *testing.T) {
	opts, srv, out := engineOptions(t)
	srv.Starting = []string{"installing runtime b11514 (65%)", "installing runtime b11514 (65%)", "starting llama-server"}
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	first, second := "taraengine is starting: installing runtime b11514 (65%)\n", "taraengine is starting: starting llama-server\n"
	if strings.Count(text, first) != 1 || strings.Count(text, second) != 1 ||
		strings.Index(text, second) > strings.Index(text, "Loading gemma4:12b") {
		t.Fatalf("the start's steps, once each, before the load: %q", text)
	}
	if len(srv.Loads) != 1 || srv.Loads[0].Model != "gemma4:12b" {
		t.Fatalf("loads %+v", srv.Loads)
	}
}

// TestNewOnTheGenericPathAsksNoHealth: a server that is not the engine is not waited for.
func TestNewOnTheGenericPathAsksNoHealth(t *testing.T) {
	opts, srv, out := engineOptions(t)
	opts.Vendor = "vllm"
	if _, err := New(opts); err != nil {
		t.Fatal(err)
	}
	if countPath(srv, "/api/health") != 0 || strings.Contains(out.String(), "taraengine is starting") {
		t.Fatalf("health calls %d, output %q", countPath(srv, "/api/health"), out.String())
	}
}

// TestSwitchModelDoesNotWaitForTheEngine: the engine runs once a session is up, so /model takes the engine's answer as
// it comes; a 503 there is the refusal, after the client's one retry.
func TestSwitchModelDoesNotWaitForTheEngine(t *testing.T) {
	opts, srv, out := engineOptions(t)
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Starting = []string{"starting llama-server"}
	err = a.SwitchModel("glm-4.7-flash")
	var refused *llm.ServerError
	if !errors.As(err, &refused) || refused.Status != http.StatusServiceUnavailable ||
		strings.Contains(out.String(), "taraengine is starting") {
		t.Fatalf("err %#v, output %q", err, out.String())
	}
}
