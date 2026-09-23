package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/ui"
)

func testREPL(t *testing.T, dir string, ephemeral bool) *repl {
	t.Helper()
	srv := ollamatest.New(t)
	srv.Models = []ollamatest.ModelSpec{{Name: "gemma4:12b", Capabilities: []string{"completion", "tools"}, ContextLength: 32768}}
	opts := agent.DefaultOptions()
	opts.Host, opts.Model, opts.WorkingDir, opts.Ephemeral, opts.Spinner = srv.URL, "gemma4:12b", dir, ephemeral, false
	var asst *agent.Assistant
	var err error
	_ = captureStdoutForTest(t, func() { asst, err = agent.New(opts) })
	if err != nil {
		t.Fatal(err)
	}
	return &repl{asst: asst, opts: opts, renderer: ui.NewRenderer(), projectRoot: dir, absDir: dir, initialised: !ephemeral}
}

func TestOperateModeNeedsAnInitialisedProject(t *testing.T) {
	r := testREPL(t, t.TempDir(), true)
	out := captureStdoutForTest(t, func() { r.dispatch("/mode operate") })
	if !strings.Contains(out, "/init") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/policy show") })
	if !strings.Contains(out, "built-in") || !strings.Contains(out, "kube-system") {
		t.Fatalf("%q", out)
	}
}

func TestInitEnablesOperateModeAndWritesTheStarterPolicy(t *testing.T) {
	dir := t.TempDir()
	r := testREPL(t, dir, true)
	_ = captureStdoutForTest(t, func() { r.dispatch("/init") })
	if _, err := os.Stat(filepath.Join(dir, ".taracode", "policy.yaml")); err != nil {
		t.Fatal("policy.yaml missing")
	}
	if !r.initialised || r.asst.GetStorage() == nil {
		t.Fatal("init must enable storage")
	}
	out := captureStdoutForTest(t, func() { r.dispatch("/mode operate") })
	if !strings.Contains(out, "Mode: operate") {
		t.Fatalf("%q", out)
	}
	out = captureStdoutForTest(t, func() { r.dispatch("/policy show") })
	if !strings.Contains(out, filepath.Join(dir, ".taracode", "policy.yaml")) {
		t.Fatalf("%q", out)
	}
}

func TestABrokenPolicyLocksOperateMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".taracode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".taracode", "policy.yaml"), []byte("version: 1\nprotected:\n  pths: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := testREPL(t, dir, false)
	out := captureStdoutForTest(t, func() { r.dispatch("/mode operate") })
	if !strings.Contains(out, "locked") || !strings.Contains(out, "pths") {
		t.Fatalf("%q", out)
	}
}

// TestPromptShowsTheOperateMarkerFromTheSharedHelper is the ruling P2-R23 regression: openReadline's
// first prompt and changeDir's prompt after cd must show the [operate] marker exactly like
// refreshPrompt does, because all three now build the prompt through the one shared formatPrompt
// helper. It calls that helper directly - the same string openReadline and changeDir would set as
// the prompt - rather than starting readline, which a test must not do.
func TestPromptShowsTheOperateMarkerFromTheSharedHelper(t *testing.T) {
	r := testREPL(t, t.TempDir(), false)
	if strings.Contains(r.formatPrompt(), "[operate]") {
		t.Fatalf("prompt = %q, should not show the marker in investigate mode", r.formatPrompt())
	}
	if err := r.asst.SetMode(policy.ModeOperate); err != nil {
		t.Fatal(err)
	}
	if got := r.formatPrompt(); !strings.Contains(got, "[operate]") {
		t.Fatalf("prompt = %q, want the operate marker (the string openReadline and changeDir build the prompt with)", got)
	}
}
