package agent

import (
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/llm/taraenginetest"
)

// TestNewPrintsFirstRunAdviceWhenNoModelIsAvailable covers the first-run hint (native core, Task
// 11, spec 5.2 "First run with no model configured calls the same recommendation"): when nothing
// is persisted or configured and the server lists no model at all, New must print the engine's
// recommendation before it returns its "no models available" error.
func TestNewPrintsFirstRunAdviceWhenNoModelIsAvailable(t *testing.T) {
	t.Chdir(t.TempDir())

	srv := taraenginetest.New(t) // no Models scripted: the server has nothing installed

	var err error
	out := captureStdout(t, func() {
		_, err = New(Options{Host: srv.URL, Vendor: "taraengine"})
	})

	if err == nil || !strings.Contains(err.Error(), "no models available") {
		t.Fatalf("New() = %v, want the no-models-available error", err)
	}
	if !strings.Contains(out, "Advice") || !strings.Contains(out, "taraengine pull") {
		t.Fatalf("first-run advice not printed: %q", out)
	}
}

// TestNewPrintsNoAdviceWhenAModelIsConfigured guards the other side: a configured model still
// lets New proceed (no advice to print, nothing to fail on this path).
func TestNewPrintsNoAdviceWhenAModelIsConfigured(t *testing.T) {
	t.Chdir(t.TempDir())

	srv := taraenginetest.New(t)
	srv.Models = []taraenginetest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	out := captureStdout(t, func() {
		if _, err := New(Options{Host: srv.URL, Model: "gemma4:12b", Vendor: "taraengine"}); err != nil {
			t.Fatalf("New() = %v, want success with a configured model", err)
		}
	})

	if strings.Contains(out, "Advice") {
		t.Fatalf("advice printed although a model was configured: %q", out)
	}
}
