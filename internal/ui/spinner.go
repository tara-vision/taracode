package ui

import (
	"fmt"
	"sync"
	"time"
)

// Spinner provides an animated loading indicator
type Spinner struct {
	frames   []string
	interval time.Duration
	message  string
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	running  bool
}

// SpinnerFrames defines different spinner animation styles
var SpinnerFrames = struct {
	Dots     []string
	Line     []string
	Circle   []string
	Bounce   []string
	Ellipsis []string
}{
	Dots:     []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	Line:     []string{"-", "\\", "|", "/"},
	Circle:   []string{"◐", "◓", "◑", "◒"},
	Bounce:   []string{"⠁", "⠂", "⠄", "⠂"},
	Ellipsis: []string{"   ", ".  ", ".. ", "..."},
}

// NewSpinner creates a new spinner with default settings
func NewSpinner() *Spinner {
	return &Spinner{
		frames:   SpinnerFrames.Dots,
		interval: 80 * time.Millisecond,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// NewSpinnerWithFrames creates a spinner with custom frames
func NewSpinnerWithFrames(frames []string) *Spinner {
	return &Spinner{
		frames:   frames,
		interval: 80 * time.Millisecond,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start begins the spinner animation with the given message
func (s *Spinner) Start(message string) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.message = message
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		i := 0
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Print initial frame
		frame := SpinnerStyle.Render(s.frames[i])
		fmt.Printf("\r%s %s", frame, s.message)

		for {
			select {
			case <-s.stop:
				// Clear the spinner line
				fmt.Print("\r\033[K")
				close(s.done)
				return
			case <-ticker.C:
				i = (i + 1) % len(s.frames)
				frame := SpinnerStyle.Render(s.frames[i])
				fmt.Printf("\r%s %s", frame, s.message)
			}
		}
	}()
}

// Stop halts the spinner animation
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stop)
	<-s.done
}

// UpdateMessage changes the spinner message while running
func (s *Spinner) UpdateMessage(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// IsRunning returns whether the spinner is currently active
func (s *Spinner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
