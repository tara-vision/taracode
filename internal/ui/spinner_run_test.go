package ui

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveStdout captures os.Stdout while spinners run, so a test can wait for what they print.
type liveStdout struct {
	mu       sync.Mutex
	text     strings.Builder
	changed  chan struct{} // receives after each chunk
	writer   *os.File
	original *os.File
	finished chan struct{}
}

func watchStdout(t *testing.T) *liveStdout {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	l := &liveStdout{changed: make(chan struct{}, 1), writer: writer, original: os.Stdout, finished: make(chan struct{})}
	os.Stdout = writer
	t.Cleanup(func() { // a test that fails before stop still gets its stdout back
		os.Stdout = l.original
		_ = writer.Close()
	})
	go func() {
		defer close(l.finished)
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				l.mu.Lock()
				l.text.Write(buf[:n])
				l.mu.Unlock()
				select {
				case l.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				_ = reader.Close()
				return
			}
		}
	}()
	return l
}

// waitFor blocks until the output holds every one of want (up to 10 seconds).
func (l *liveStdout) waitFor(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		l.mu.Lock()
		text := l.text.String()
		l.mu.Unlock()
		all := true
		for _, w := range want {
			all = all && strings.Contains(text, w)
		}
		if all {
			return
		}
		select {
		case <-l.changed:
		case <-deadline:
			t.Fatalf("the output never showed %q:\n%q", want, text)
		}
	}
}

// stop restores os.Stdout and returns everything printed.
func (l *liveStdout) stop(t *testing.T) string {
	t.Helper()
	os.Stdout = l.original
	if err := l.writer.Close(); err != nil {
		t.Fatal(err)
	}
	<-l.finished
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

func TestSpinnerShowsItsFramesAndMessages(t *testing.T) {
	out := watchStdout(t)
	s := NewSpinnerWithFrames([]string{"<a>", "<b>"})
	s.interval = time.Millisecond
	s.Start("Working")
	s.Start("ignored: already running")
	out.waitFor(t, "<a> Working", "<b> Working")
	if !s.IsRunning() || s.GetElapsed() <= 0 {
		t.Fatalf("running %v, elapsed %v", s.IsRunning(), s.GetElapsed())
	}
	s.UpdateMessage("Still working")
	out.waitFor(t, "Still working")
	s.Stop()
	s.Stop() // a second stop is a no-op
	text := out.stop(t)
	if s.IsRunning() || !strings.HasSuffix(text, "\r\033[K") || strings.Contains(text, "ignored") {
		t.Fatalf("running %v, output %q", s.IsRunning(), text)
	}
}

func TestThinkingSpinnerRotatesItsMessages(t *testing.T) {
	out := watchStdout(t)
	s := NewThinkingSpinner()
	s.interval, s.messageInterval = time.Millisecond, time.Millisecond
	s.Start("Booting")
	deadline := time.After(10 * time.Second)
	for rotated := false; !rotated; {
		select {
		case <-out.changed:
		case <-deadline:
			t.Fatal("the message never rotated")
		}
		out.mu.Lock()
		text := out.text.String()
		out.mu.Unlock()
		for _, m := range ThinkingMessages {
			rotated = rotated || strings.Contains(text, m)
		}
	}
	s.Stop()
	out.stop(t)
}

// TestSpinnersShowTheElapsedTime runs a plain spinner and a status-line spinner side by side past
// their one-second elapsed tick: the plain one appends the seconds to its message, the status line
// shows the time, the tokens and the state.
func TestSpinnersShowTheElapsedTime(t *testing.T) {
	out := watchStdout(t)
	plain := NewSpinner()
	plain.interval = time.Millisecond
	plain.SetElapsedThreshold(0)
	status := NewStatusLineSpinner()
	status.interval = time.Millisecond
	status.UpdateTokens(8000)
	status.UpdateState("executing")
	plain.Start("Working...")
	status.Start("")
	out.waitFor(t, "Working... 1s", "Esc to cancel \u00b7 2s \u00b7 \u2193 8.0k tokens \u00b7 executing")
	plain.Stop()
	status.Stop()
	out.stop(t)
	status.mu.Lock()
	defer status.mu.Unlock()
	if !strings.Contains(status.message, "Esc to cancel") {
		t.Fatalf("the elapsed tick refreshes the status line message: %q", status.message)
	}
}

func TestFormatStatusLine(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		tokens  int
		want    string
	}{
		{5 * time.Second, 999, "(Esc to cancel \u00b7 5s \u00b7 \u2193 999 tokens \u00b7 thinking)"},
		{201 * time.Second, 12345, "(Esc to cancel \u00b7 3m 21s \u00b7 \u2193 12.3k tokens \u00b7 thinking)"},
	}
	for _, tt := range tests {
		if got := formatStatusLine("thinking", tt.elapsed, tt.tokens); got != "\033[90m"+tt.want+"\033[0m" {
			t.Errorf("formatStatusLine(%s, %d) = %q", tt.elapsed, tt.tokens, got)
		}
	}
}
