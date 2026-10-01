package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chzyer/readline"

	"github.com/tara-vision/taracode/internal/llm/ollamatest"
)

// discardCloser is a readline output that keeps nothing.
type discardCloser struct{}

func (discardCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardCloser) Close() error                { return nil }

func noRawMode() error { return nil }

// scriptedReadline is a readline instance that reads input from a pipe, writes nowhere and never
// touches the terminal. Close it only after at least one Readline call: readline's Close races with
// its own reader goroutine otherwise (the race detector reports it).
func scriptedReadline(t *testing.T, input string) *readline.Instance {
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
	rl, err := readline.NewEx(&readline.Config{
		Stdin: reader, Stdout: discardCloser{}, Stderr: discardCloser{},
		FuncMakeRaw: noRawMode, FuncExitRaw: noRawMode, FuncIsTerminal: func() bool { return false },
		FuncGetWidth: func() int { return 80 }, FuncOnWidthChanged: func(func()) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rl
}

// runLines feeds lines to the repl's read loop until it ends, then releases the readline instance.
func runLines(t *testing.T, r *repl, lines ...string) (stdout, stderr string) {
	t.Helper()
	r.completer = NewSlashCompleter(r.absDir)
	r.rl = scriptedReadline(t, strings.Join(lines, "\n")+"\n")
	stdout, stderr = captureOutput(t, r.run)
	r.close()
	return stdout, stderr
}

func TestRunRoutesEveryKindOfLine(t *testing.T) {
	resetConfig(t)
	r, _ := projectREPL(t, ollamatest.Turn{Content: "The pod restarts. Would you like me to check its logs?"})
	if err := os.Mkdir(filepath.Join(r.projectRoot, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := runLines(t, r, "", "cd sub", "pwd", "cd ../..", "cd", "/think", "@missing.txt explain",
		"is the pod healthy")
	for _, want := range []string{
		"Changed to: sub", "/sub (absolute: " + filepath.Join(r.projectRoot, "sub") + ")",
		"Error: cannot navigate above project root", "Changed to project root", "Reasoning mode: auto",
		"The pod restarts.", "Tab: yes", "Goodbye!",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "Error: failed to access missing.txt") {
		t.Errorf("stderr %q", stderr)
	}
	if r.relDir != "" || r.absDir != r.projectRoot || r.completer.GetSuggestion() != "yes" {
		t.Fatalf("relDir %q absDir %q suggestion %q", r.relDir, r.absDir, r.completer.GetSuggestion())
	}
}

func TestRunStopsAtExitOrQuit(t *testing.T) {
	for _, word := range []string{"exit", "quit"} {
		r := replOn(t, fakeOllama(t), t.TempDir(), true)
		stdout, _ := runLines(t, r, word, "/help")
		if !strings.Contains(stdout, "Goodbye!") || strings.Contains(stdout, "Commands:") {
			t.Errorf("%s: the loop must end before the next line:\n%s", word, stdout)
		}
	}
}

func TestAskOffersToRememberAConvention(t *testing.T) {
	resetConfig(t)
	r, _ := projectREPL(t, ollamatest.Turn{Content: "Understood."})
	var stdout string
	withStdin(t, "y\n", func() { stdout, _ = runLines(t, r, "We always use helm for every deploy here") })
	list := r.memory.List()
	if !strings.Contains(stdout, `Remember: "We always use helm for every deploy here"?`) || len(list) != 1 {
		t.Fatalf("memories %+v, output %q", list, stdout)
	}
}

func TestAskReportsAModelError(t *testing.T) {
	r := replOn(t, fakeOllama(t, ollamatest.Turn{Status: 500, Error: "model crashed"}), t.TempDir(), true)
	_, stderr := runLines(t, r, "hello")
	if !strings.Contains(stderr, "model crashed") {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestAskSendsTheReferencedImage(t *testing.T) {
	srv := fakeOllama(t, ollamatest.Turn{Content: "A diagram."})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pic.png"), "png-bytes")
	r := replOn(t, srv, dir, true)
	stdout, _ := runLines(t, r, "what is @pic.png")
	if !strings.Contains(stdout, "Loaded image: pic.png") || !strings.Contains(stdout, "A diagram.") {
		t.Fatalf("%q", stdout)
	}
	var images []any
	for _, req := range srv.Requests {
		if req.Path != "/api/chat" {
			continue
		}
		messages, _ := req.Body["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		images, _ = last["images"].([]any)
	}
	if len(images) != 1 {
		t.Fatalf("the chat request carries images %v", images)
	}
}
