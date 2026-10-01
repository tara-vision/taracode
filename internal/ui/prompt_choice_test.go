package ui

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/chzyer/readline"

	"github.com/tara-vision/taracode/internal/policy"
)

type discardCloser struct{ io.Writer }

func (discardCloser) Close() error { return nil }

// keystrokes points readline's default input, which promptui reads, at a pipe holding input, and its
// output nowhere, for the rest of the test. Ctrl-N (\x0e) moves a selection down, \r picks it, and
// input that ends without \r leaves the prompt with no answer.
func keystrokes(t *testing.T, input string) {
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
	readline.Stdin, readline.Stdout = reader, discardCloser{io.Discard}
	t.Cleanup(func() {
		readline.Stdin, readline.Stdout = stdin, stdout
		_ = reader.Close()
	})
}

func TestPromptPermissionMapsEveryChoice(t *testing.T) {
	inv := policy.Invocation{Tool: "shell", Command: "rm -rf build", Reason: "rm removes files",
		Targets: policy.Targets{KubeContext: "prod", KubeNamespace: "shop"}}
	tests := []struct {
		keys string
		want PermissionChoice
	}{
		{"\r", PermissionChoice{Allowed: true}},
		{"\x0e\r", PermissionChoice{}},
		{"\x0e\x0e\r", PermissionChoice{Allowed: true, Remember: true}},
		{"\x0e\x0e\x0e\r", PermissionChoice{Remember: true}},
		{"", PermissionChoice{}}, // no answer: denied, nothing remembered
	}
	for _, tt := range tests {
		keystrokes(t, tt.keys)
		var got PermissionChoice
		out := captureStdout(t, func() { got = PromptPermission(inv, nil) })
		if got != tt.want {
			t.Errorf("keys %q: %+v, want %+v", tt.keys, got, tt.want)
		}
		for _, want := range []string{"shell wants to run a mutation", "Command: rm -rf build", "Why: rm removes files",
			"Target: context=prod namespace=shop account="} {
			if !strings.Contains(out, want) {
				t.Errorf("keys %q: output lacks %q:\n%s", tt.keys, want, out)
			}
		}
	}
}

func TestPromptPermissionShowsTheParamsOfACallWithoutACommand(t *testing.T) {
	keystrokes(t, "\x0e\r")
	out := captureStdout(t, func() {
		if got := PromptPermission(policy.Invocation{Tool: "write_file"}, map[string]any{"path": "notes.txt"}); got.Allowed {
			t.Error("the second choice denies")
		}
	})
	if !strings.Contains(out, "Params: path=notes.txt") || strings.Contains(out, "Why:") || strings.Contains(out, "Target:") {
		t.Fatalf("%s", out)
	}
}

func TestDisplayEditPreviewMapsEveryChoice(t *testing.T) {
	preview := &EditPreview{FilePath: "app.yaml", OldContent: "a: 1\nb: 2\n", NewContent: "a: 1\nb: 3\n", OldString: "b: 2",
		NewString: "b: 3"}
	tests := []struct {
		keys string
		want EditPreviewChoice
	}{
		{"\r", EditPreviewApply},
		{"\x0e\r", EditPreviewCancel},
		{"\x0e\x0e\r", EditPreviewBackupThenApply},
		{"", EditPreviewCancel}, // no answer: cancelled
	}
	for _, tt := range tests {
		keystrokes(t, tt.keys)
		var got EditPreviewChoice
		out := captureStdout(t, func() { got = DisplayEditPreview(preview) })
		if got != tt.want {
			t.Errorf("keys %q: %v, want %v", tt.keys, got, tt.want)
		}
		for _, want := range []string{"Edit Preview: app.yaml", "-b: 2", "+b: 3", "Changes: modified"} {
			if !strings.Contains(out, want) {
				t.Errorf("keys %q: output lacks %q:\n%s", tt.keys, want, out)
			}
		}
	}
}

// TestDisplayEditPreviewCountsTheLines: the change line says how many lines the edit adds or removes.
func TestDisplayEditPreviewCountsTheLines(t *testing.T) {
	tests := []struct {
		oldString, newString, want string
	}{
		{"b", "b\nc\nd", "Changes: +2 lines"},
		{"b\nc", "b", "Changes: -1 lines"},
	}
	for _, tt := range tests {
		keystrokes(t, "\x0e\r")
		out := captureStdout(t, func() {
			DisplayEditPreview(&EditPreview{FilePath: "f", OldContent: "a\n" + tt.oldString + "\n",
				NewContent: "a\n" + tt.newString + "\n", OldString: tt.oldString, NewString: tt.newString})
		})
		if !strings.Contains(out, tt.want) {
			t.Errorf("%q -> %q: output lacks %q:\n%s", tt.oldString, tt.newString, tt.want, out)
		}
	}
}

func TestConfirmActionWithoutAnAnswerIsNo(t *testing.T) {
	keystrokes(t, "")
	if ConfirmAction("Delete the session?") {
		t.Fatal("no answer is no")
	}
}
