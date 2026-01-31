package ui

import (
	"testing"
	"time"
)

func TestFormatMessageWithElapsed(t *testing.T) {
	tests := []struct {
		message  string
		elapsed  time.Duration
		expected string
	}{
		{"Running...", 5 * time.Second, "Running... 5s"},
		{"Running...", 10 * time.Second, "Running... 10s"},
		{"Running...", 45 * time.Second, "Running... 45s"},
		{"Running...", 60 * time.Second, "Running... 1m0s"},
		{"Running...", 90 * time.Second, "Running... 1m30s"},
		{"Running...", 125 * time.Second, "Running... 2m5s"},
		{"Thinking", 15 * time.Second, "Thinking... 15s"},
		{"Processing", 30 * time.Second, "Processing... 30s"},
	}

	for _, tc := range tests {
		result := formatMessageWithElapsed(tc.message, tc.elapsed)
		if result != tc.expected {
			t.Errorf("formatMessageWithElapsed(%q, %v) = %q, want %q",
				tc.message, tc.elapsed, result, tc.expected)
		}
	}
}

func TestNewSpinnerDefaults(t *testing.T) {
	s := NewSpinner()

	if !s.showElapsed {
		t.Error("Expected showElapsed to be true by default")
	}

	if s.elapsedThreshold != 5*time.Second {
		t.Errorf("Expected elapsedThreshold to be 5s, got %v", s.elapsedThreshold)
	}

	if s.rotateMessages {
		t.Error("Expected rotateMessages to be false for NewSpinner")
	}
}

func TestNewThinkingSpinnerDefaults(t *testing.T) {
	s := NewThinkingSpinner()

	if !s.showElapsed {
		t.Error("Expected showElapsed to be true by default")
	}

	if s.elapsedThreshold != 5*time.Second {
		t.Errorf("Expected elapsedThreshold to be 5s, got %v", s.elapsedThreshold)
	}

	if !s.rotateMessages {
		t.Error("Expected rotateMessages to be true for NewThinkingSpinner")
	}
}

func TestSpinnerSetters(t *testing.T) {
	s := NewSpinner()

	s.SetShowElapsed(false)
	if s.showElapsed {
		t.Error("Expected showElapsed to be false after SetShowElapsed(false)")
	}

	s.SetElapsedThreshold(10 * time.Second)
	if s.elapsedThreshold != 10*time.Second {
		t.Errorf("Expected elapsedThreshold to be 10s, got %v", s.elapsedThreshold)
	}

	s.SetRotateMessages(true)
	if !s.rotateMessages {
		t.Error("Expected rotateMessages to be true after SetRotateMessages(true)")
	}
}

func TestSpinnerNotRunningByDefault(t *testing.T) {
	s := NewSpinner()

	if s.IsRunning() {
		t.Error("Expected spinner to not be running by default")
	}
}

func TestSpinnerGetElapsedBeforeStart(t *testing.T) {
	s := NewSpinner()

	elapsed := s.GetElapsed()
	if elapsed != 0 {
		t.Errorf("Expected elapsed to be 0 before start, got %v", elapsed)
	}
}
