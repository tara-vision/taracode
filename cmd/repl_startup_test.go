package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chzyer/readline"
	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/upgrade"
)

// readlineInput points readline's default input at a pipe that holds input, and its output nowhere,
// for the rest of the test: openReadline builds its instance on readline's defaults.
func readlineInput(t *testing.T, input string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stdin, stdout := readline.Stdin, readline.Stdout
	readline.Stdin, readline.Stdout = reader, discardCloser{}
	t.Cleanup(func() {
		readline.Stdin, readline.Stdout = stdin, stdout
		_ = reader.Close()
	})
}

// startupConfig is the configuration newREPL reads: the fake's host and model, no spinner, HOME
// isolated, and dir as the working directory.
func startupConfig(t *testing.T, srv *ollamatest.Server, dir string) {
	t.Helper()
	resetConfig(t)
	isolateHome(t)
	t.Chdir(dir)
	viper.Set("host", srv.URL)
	viper.Set("model", "gemma4:12b")
	viper.Set("no_spinner", true)
}

// answerUpdateCheck replaces the startup update check with one that has already answered.
func answerUpdateCheck(t *testing.T, result *upgrade.CheckResult) {
	t.Helper()
	original := checkForUpdate
	checkForUpdate = func(_ string, results chan<- *upgrade.CheckResult) { results <- result }
	t.Cleanup(func() { checkForUpdate = original })
}

// forbidUpdateCheck makes the startup update check a test failure: the configuration says not to run it.
func forbidUpdateCheck(t *testing.T, why string) {
	t.Helper()
	original := checkForUpdate
	checkForUpdate = func(string, chan<- *upgrade.CheckResult) { t.Errorf("the update check ran although %s", why) }
	t.Cleanup(func() { checkForUpdate = original })
}

// runAndClose reads the rest of the scripted input through the repl's own readline instance, with
// raw mode switched off so the terminal of whoever runs the tests is never touched.
func runAndClose(t *testing.T, r *repl) string {
	t.Helper()
	r.rl.Config.FuncMakeRaw, r.rl.Config.FuncExitRaw = noRawMode, noRawMode
	out := captureStdoutForTest(t, r.run)
	r.close()
	return out
}

func TestNewREPLNeedsAHost(t *testing.T) {
	resetConfig(t)
	isolateHome(t)
	viper.Set("watch", map[string]any{"interval": 5}) // a 2.x section: its warning comes before the error
	var err error
	out := captureStdoutForTest(t, func() { _, err = newREPL() })
	if err == nil || !strings.Contains(err.Error(), "LLM server host not found") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "the watch: section is ignored") {
		t.Fatalf("the warnings print before the error: %q", out)
	}
}

func TestNewREPLReportsAConnectionFailure(t *testing.T) {
	resetConfig(t)
	isolateHome(t)
	t.Chdir(t.TempDir())
	viper.Set("host", "http://127.0.0.1:1")
	var err error
	_ = captureStdoutForTest(t, func() { _, err = newREPL() })
	if err == nil || !strings.Contains(err.Error(), "Cannot connect to LLM server") ||
		!strings.Contains(err.Error(), "Host: http://127.0.0.1:1") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewREPLStartsAnUninitialisedSession(t *testing.T) {
	srv := fakeOllama(t)
	startupConfig(t, srv, t.TempDir())
	answerUpdateCheck(t, &upgrade.CheckResult{CurrentVersion: "dev", LatestVersion: "v9.9.9", UpdateAvailable: true})
	readlineInput(t, "pwd\nexit\nleft over\n")
	var r *repl
	var err error
	out := captureStdoutForTest(t, func() { r, err = newREPL() })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Connected to Ollama", "Tara Code", "Run '/init' to initialize project context",
		fmt.Sprintf("Mode: investigate (%d tools)", r.asst.ToolRegistry().Available(r.asst.Mode())),
		"Not initialised: nothing is saved", "Update available:", "v9.9.9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("startup lacks %q:\n%s", want, out)
		}
	}
	if r.initialised || r.history != nil || r.memory != nil || r.mcp != nil || r.asst.GetStorage() != nil {
		t.Fatalf("an uninitialised start: %+v", r)
	}
	out = runAndClose(t, r)
	if !strings.Contains(out, "(project root: ") || !strings.Contains(out, "Goodbye!") {
		t.Fatalf("the loop over readline's input: %q", out)
	}
	if line, err := r.rl.Readline(); line != "" || !errors.Is(err, io.EOF) {
		t.Fatalf("a closed repl reads no more input: %q %v", line, err)
	}
	(&repl{}).close() // a repl that never opened readline closes too
}

// TestNewREPLEnablesAnInitialisedProject starts in a project with a session, a memory and an MCP
// server that connects on its own: the session resumes, the managers exist and the server's tools
// are registered before the first prompt.
func TestNewREPLEnablesAnInitialisedProject(t *testing.T) {
	dir := t.TempDir()
	storedSession(t, dir, []storage.ConversationMessage{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}})
	if _, err := managerAt(t, filepath.Join(dir, ".taracode")).Create(storage.MemoryCategoryLearning, "Staging is blue", "",
		nil, storage.MemorySourceManual); err != nil {
		t.Fatal(err)
	}
	srv := fakeOllama(t)
	startupConfig(t, srv, dir)
	forbidUpdateCheck(t, "the configuration is offline")
	viper.SetConfigType("yaml")
	config := fmt.Sprintf("offline: true\nwatch:\n  interval: 5\nmcp:\n  servers:\n    - name: fake\n      command: %q\n"+
		"      args: [\"-test.run=^TestFakeMCPServerProcess$\"]\n      env:\n        %s: \"1\"\n      auto_connect: true\n"+
		"      timeout: 20s\n", os.Args[0], fakeMCPEnv)
	if err := viper.ReadConfig(strings.NewReader(config)); err != nil {
		t.Fatal(err)
	}
	readlineInput(t, "/mcp\nexit\n")
	var r *repl
	var err error
	out := captureStdoutForTest(t, func() { r, err = newREPL() })
	if err != nil {
		t.Fatal(err)
	}
	if r.mcp != nil {
		t.Cleanup(r.mcp.Close)
	}
	for _, want := range []string{
		"Resuming session with 2 previous messages", "config: the watch: section is ignored",
		"Project context loaded from TARACODE.md", "Loaded 1 project memories",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("startup lacks %q:\n%s", want, out)
		}
	}
	if !r.initialised || r.history == nil || r.memory == nil || r.mcp == nil || !r.mcp.IsConnected("fake") {
		t.Fatalf("an initialised start: %+v", r)
	}
	if _, ok := r.asst.ToolRegistry().Get("fake.list_issues"); !ok {
		t.Fatal("the auto-connected server's tools are not registered")
	}
	out = runAndClose(t, r)
	if !strings.Contains(out, "\033[32mconnected\033[0m (auto-connect)  [2 tools]") || !strings.Contains(out, "Goodbye!") {
		t.Fatalf("%q", out)
	}
}

// TestNewREPLLeavesTheUpdateCheckOffWhenItIsSwitchedOff: upgrade.auto_check false keeps the startup
// update check from running on an online session.
func TestNewREPLLeavesTheUpdateCheckOffWhenItIsSwitchedOff(t *testing.T) {
	srv := fakeOllama(t)
	startupConfig(t, srv, t.TempDir())
	viper.Set("upgrade.auto_check", false)
	forbidUpdateCheck(t, "upgrade.auto_check is false")
	readlineInput(t, "exit\n")
	var r *repl
	var err error
	out := captureStdoutForTest(t, func() { r, err = newREPL() })
	if err != nil {
		t.Fatal(err)
	}
	if out += runAndClose(t, r); strings.Contains(out, "Update available") || !strings.Contains(out, "Goodbye!") {
		t.Fatalf("%q", out)
	}
}
