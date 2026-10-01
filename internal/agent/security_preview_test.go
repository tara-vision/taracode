package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chzyer/readline"

	"github.com/tara-vision/taracode/internal/ui"
)

// previewAssistant is a test assistant with previews on, every decision answered with choice.
func previewAssistant(t *testing.T, choice ui.EditPreviewChoice) (*Assistant, *[]*ui.EditPreview) {
	t.Helper()
	a, _ := newTestAssistant(t, false)
	var shown []*ui.EditPreview
	a.previewEdits, a.previewThreshold = true, 0
	a.confirmEditPreview = func(p *ui.EditPreview) ui.EditPreviewChoice {
		shown = append(shown, p)
		return choice
	}
	return a, &shown
}

func TestEditPreviewLeavesCallsItCannotPreviewToTheTool(t *testing.T) {
	a, shown := previewAssistant(t, ui.EditPreviewCancel)
	for _, params := range []map[string]any{
		{"old": "hello", "new": "x"},
		{"path": "hello.txt", "new": "x"},
		{"path": "hello.txt", "old": "", "new": "x"},
		{"path": "absent.txt", "old": "hello", "new": "x"},
		{"path": "hello.txt", "old": "not in the file", "new": "x"},
	} {
		if proceed, msg, err := a.handleEditPreview(params); !proceed || msg != "" || err != nil {
			t.Errorf("%v: proceed %v, %q, %v", params, proceed, msg, err)
		}
	}
	a.previewEdits = false
	if proceed, _, _ := a.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "x"}); !proceed {
		t.Error("previews off")
	}
	if len(*shown) != 0 {
		t.Fatalf("nothing was previewable: %d previews", len(*shown))
	}
}

func TestEditPreviewThresholdSkipsSmallEdits(t *testing.T) {
	a, shown := previewAssistant(t, ui.EditPreviewCancel)
	a.previewThreshold = 3
	if proceed, _, _ := a.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "a\nb"}); !proceed {
		t.Fatal("two lines is below the threshold of three")
	}
	if proceed, _, _ := a.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "a\nb\nc"}); proceed {
		t.Fatal("three lines reaches the threshold and is previewed (and cancelled)")
	}
	p := (*shown)[0]
	if len(*shown) != 1 || p.FilePath != "hello.txt" || p.OldContent != "hello from disk" || p.NewContent != "a\nb\nc from disk" {
		t.Fatalf("previews %+v", *shown)
	}
}

func TestEditPreviewApplyAndBackup(t *testing.T) {
	a, shown := previewAssistant(t, ui.EditPreviewApply)
	abs := filepath.Join(a.workingDir, "hello.txt")
	if proceed, msg, err := a.handleEditPreview(map[string]any{"path": abs, "old": "hello", "new": "x"}); !proceed ||
		msg != "" || err != nil {
		t.Fatalf("apply: %v %q %v", proceed, msg, err)
	}
	if len(*shown) != 1 || (*shown)[0].FilePath != abs || (*shown)[0].OldContent != "hello from disk" ||
		(*shown)[0].NewContent != "x from disk" {
		t.Fatalf("an absolute path is read as it is: previews %+v", *shown)
	}

	b, _ := previewAssistant(t, ui.EditPreviewBackupThenApply)
	if proceed, _, err := b.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "x"}); !proceed || err != nil {
		t.Fatalf("backup without storage applies: %v %v", proceed, err)
	}
	enterOperate(t, b)
	out := captureStdout(t, func() {
		if proceed, _, err := b.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "x"}); !proceed ||
			err != nil {
			t.Errorf("backup then apply: %v %v", proceed, err)
		}
	})
	backups, err := os.ReadDir(b.storage.GetBackupDir())
	if err != nil || len(backups) != 1 || !strings.Contains(out, "Backup saved: ") {
		t.Fatalf("backups %v err=%v output %q", backups, err, out)
	}
}

// TestEditPreviewNeverShowsMoreThanOneReplacement: edit_file replaces one occurrence (and refuses
// an old string it finds twice), so the preview replaces only the first occurrence.
func TestEditPreviewNeverShowsMoreThanOneReplacement(t *testing.T) {
	a, shown := previewAssistant(t, ui.EditPreviewApply)
	if err := os.WriteFile(filepath.Join(a.workingDir, "twice.txt"), []byte("ab ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _ = a.handleEditPreview(map[string]any{"path": "twice.txt", "old": "ab", "new": "cd"})
	for _, p := range *shown {
		if p.NewContent != "cd ab" {
			t.Fatalf("the preview replaced more than one occurrence: %q", p.NewContent)
		}
	}
}

func TestEditPreviewStopsWhenTheBackupFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes whatever the mode says")
	}
	a, _ := previewAssistant(t, ui.EditPreviewBackupThenApply)
	enterOperate(t, a)
	var out bytes.Buffer
	a.out = &out
	backups := a.storage.GetBackupDir()
	if err := os.Chmod(backups, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(backups, 0o755) })
	proceed, msg, err := a.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "x"})
	if proceed || err == nil || !strings.HasPrefix(msg, "Failed to create backup: ") || !strings.Contains(out.String(), msg) {
		t.Fatalf("proceed %v, %q, %v, output %q", proceed, msg, err, out.String())
	}
}

// TestEditPreviewAsksTheTerminalByDefault: with nothing replacing it, the preview is the terminal
// prompt, read through readline's input (Ctrl-N then Enter picks "No, cancel this edit").
func TestEditPreviewAsksTheTerminalByDefault(t *testing.T) {
	a, _ := previewAssistant(t, ui.EditPreviewApply)
	a.confirmEditPreview = nil
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("\x0e\r"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stdin, stdout := readline.Stdin, readline.Stdout
	readline.Stdin, readline.Stdout = reader, discardWriteCloser{}
	t.Cleanup(func() { readline.Stdin, readline.Stdout = stdin, stdout; _ = reader.Close() })
	_ = captureStdout(t, func() {
		proceed, msg, err := a.handleEditPreview(map[string]any{"path": "hello.txt", "old": "hello", "new": "x"})
		if proceed || err != nil || !strings.HasPrefix(msg, "Edit cancelled by user: ") {
			t.Errorf("proceed %v, %q, %v", proceed, msg, err)
		}
	})
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }
