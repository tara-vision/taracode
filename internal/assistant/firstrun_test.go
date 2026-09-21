package assistant

import (
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// TestNewPrintsFirstRunAdviceWhenNoModelIsAvailable covers the first-run hint (native core, Task
// 11, spec 5.2 "First run with no model configured calls the same recommendation"): when nothing
// is persisted or configured and the server lists no model at all, New must print the registry's
// recommendation before it returns its "no models available" error.
func TestNewPrintsFirstRunAdviceWhenNoModelIsAvailable(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t) // no Models scripted: the server has nothing installed

	var err error
	out := captureStdout(t, func() {
		_, err = New(srv.URL, "", "", "ollama", false, false)
	})

	if err == nil || !strings.Contains(err.Error(), "no models available") {
		t.Fatalf("New() = %v, want the no-models-available error", err)
	}
	if !strings.Contains(out, "Advice") || !strings.Contains(out, "ollama pull") {
		t.Fatalf("first-run advice not printed: %q", out)
	}
}

// TestNewPrintsNoAdviceWhenAModelIsConfigured guards the other side: a configured model still
// lets New proceed (no advice to print, nothing to fail on this path).
func TestNewPrintsNoAdviceWhenAModelIsConfigured(t *testing.T) {
	viper.Reset()
	t.Chdir(t.TempDir())

	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{
		{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768, Family: "gemma4"},
	}

	out := captureStdout(t, func() {
		if _, err := New(srv.URL, "", "gemma4:12b", "ollama", false, false); err != nil {
			t.Fatalf("New() = %v, want success with a configured model", err)
		}
	})

	if strings.Contains(out, "Advice") {
		t.Fatalf("advice printed although a model was configured: %q", out)
	}
}
