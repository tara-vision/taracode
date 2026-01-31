package ui

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ThinkingMessages are professional messages shown while the AI is processing
var ThinkingMessages = []string{
	"Thinking...",
	"Processing...",
	"Analyzing...",
	"Working...",
}

// Spinner provides an animated loading indicator
type Spinner struct {
	frames           []string
	interval         time.Duration
	message          string
	baseMessage      string        // Original message without elapsed time
	stop             chan struct{}
	done             chan struct{}
	mu               sync.Mutex
	running          bool
	rotateMessages   bool          // Whether to rotate through fun messages
	messageInterval  time.Duration // How often to change the message
	showElapsed      bool          // Whether to show elapsed time
	elapsedThreshold time.Duration // Time before showing elapsed (default 5s)
	startTime        time.Time     // When the spinner started
	tokens           int           // Current token count for status line
	state            string        // Current state (thinking, executing, etc.)
	showStatusLine   bool          // Whether to show Claude Code style status line
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
		frames:           SpinnerFrames.Dots,
		interval:         80 * time.Millisecond,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		rotateMessages:   false,
		messageInterval:  3 * time.Second,
		showElapsed:      true, // Enable elapsed time by default
		elapsedThreshold: 5 * time.Second,
	}
}

// NewThinkingSpinner creates a spinner with rotating fun thinking messages
func NewThinkingSpinner() *Spinner {
	return &Spinner{
		frames:           SpinnerFrames.Dots,
		interval:         80 * time.Millisecond,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		rotateMessages:   true,
		messageInterval:  3 * time.Second,
		showElapsed:      true, // Enable elapsed time by default
		elapsedThreshold: 5 * time.Second,
	}
}

// NewStatusLineSpinner creates a Claude Code style status line spinner
func NewStatusLineSpinner() *Spinner {
	return &Spinner{
		frames:           SpinnerFrames.Dots,
		interval:         80 * time.Millisecond,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		rotateMessages:   false,
		showElapsed:      true,
		elapsedThreshold: 0, // Show immediately
		showStatusLine:   true,
		state:            "thinking",
	}
}

// SetRotateMessages enables or disables rotating through fun messages
func (s *Spinner) SetRotateMessages(rotate bool) {
	s.mu.Lock()
	s.rotateMessages = rotate
	s.mu.Unlock()
}

// SetShowElapsed enables or disables elapsed time display
func (s *Spinner) SetShowElapsed(show bool) {
	s.mu.Lock()
	s.showElapsed = show
	s.mu.Unlock()
}

// SetElapsedThreshold sets the time before elapsed time is shown
func (s *Spinner) SetElapsedThreshold(threshold time.Duration) {
	s.mu.Lock()
	s.elapsedThreshold = threshold
	s.mu.Unlock()
}

// NewSpinnerWithFrames creates a spinner with custom frames
func NewSpinnerWithFrames(frames []string) *Spinner {
	return &Spinner{
		frames:           frames,
		interval:         80 * time.Millisecond,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		showElapsed:      true,
		elapsedThreshold: 5 * time.Second,
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
	s.baseMessage = message
	s.startTime = time.Now()
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	rotateMessages := s.rotateMessages
	messageInterval := s.messageInterval
	showElapsed := s.showElapsed
	elapsedThreshold := s.elapsedThreshold
	showStatusLine := s.showStatusLine
	s.mu.Unlock()

	go func() {
		i := 0
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// Message rotation ticker (only if enabled)
		var messageTicker *time.Ticker
		var messageTickerChan <-chan time.Time
		if rotateMessages {
			messageTicker = time.NewTicker(messageInterval)
			messageTickerChan = messageTicker.C
			defer messageTicker.Stop()
		}

		// Elapsed time update ticker (every second)
		elapsedTicker := time.NewTicker(1 * time.Second)
		defer elapsedTicker.Stop()

		// Print initial frame (clear to end of line to handle message length changes)
		s.mu.Lock()
		msg := s.message
		if showStatusLine {
			msg = formatStatusLine(s.state, time.Since(s.startTime), s.tokens)
		}
		s.mu.Unlock()
		frame := SpinnerStyle.Render(s.frames[i])
		fmt.Printf("\r%s %s\033[K", frame, msg)

		for {
			select {
			case <-s.stop:
				// Clear the spinner line
				fmt.Print("\r\033[K")
				close(s.done)
				return
			case <-messageTickerChan:
				// Rotate to a new random thinking message
				s.mu.Lock()
				s.baseMessage = ThinkingMessages[rand.Intn(len(ThinkingMessages))]
				s.message = s.baseMessage
				s.mu.Unlock()
			case <-elapsedTicker.C:
				// Update elapsed time if enabled and past threshold
				s.mu.Lock()
				elapsed := time.Since(s.startTime)
				if showStatusLine {
					s.message = formatStatusLine(s.state, elapsed, s.tokens)
				} else if showElapsed && elapsed >= elapsedThreshold {
					s.message = formatMessageWithElapsed(s.baseMessage, elapsed)
				}
				s.mu.Unlock()
			case <-ticker.C:
				i = (i + 1) % len(s.frames)
				s.mu.Lock()
				msg := s.message
				if showStatusLine {
					// Always update status line on each frame for smooth token updates
					msg = formatStatusLine(s.state, time.Since(s.startTime), s.tokens)
				}
				s.mu.Unlock()
				frame := SpinnerStyle.Render(s.frames[i])
				fmt.Printf("\r%s %s\033[K", frame, msg)
			}
		}
	}()
}

// formatMessageWithElapsed adds elapsed time to a message
func formatMessageWithElapsed(baseMessage string, elapsed time.Duration) string {
	// Remove trailing "..." if present for cleaner formatting
	cleanMessage := baseMessage
	if len(cleanMessage) > 3 && cleanMessage[len(cleanMessage)-3:] == "..." {
		cleanMessage = cleanMessage[:len(cleanMessage)-3]
	}

	// Format elapsed time
	secs := int(elapsed.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%s... %ds", cleanMessage, secs)
	}
	mins := secs / 60
	secs = secs % 60
	return fmt.Sprintf("%s... %dm%ds", cleanMessage, mins, secs)
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
	s.baseMessage = message
	s.mu.Unlock()
}

// IsRunning returns whether the spinner is currently active
func (s *Spinner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetElapsed returns the elapsed time since the spinner started
func (s *Spinner) GetElapsed() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.startTime.IsZero() {
		return 0
	}
	return time.Since(s.startTime)
}

// UpdateTokens updates the token count displayed in the status line
func (s *Spinner) UpdateTokens(tokens int) {
	s.mu.Lock()
	s.tokens = tokens
	s.mu.Unlock()
}

// UpdateState updates the current state displayed in the status line
func (s *Spinner) UpdateState(state string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

// formatStatusLine creates a Claude Code style status line
// Format: (Esc to cancel · 3m 21s · ↓ 8.0k tokens · thinking)
func formatStatusLine(state string, elapsed time.Duration, tokens int) string {
	// Muted color (ANSI 90 = bright black/gray)
	muted := "\033[90m"
	reset := "\033[0m"

	// Format elapsed time
	var elapsedStr string
	secs := int(elapsed.Seconds())
	if secs < 60 {
		elapsedStr = fmt.Sprintf("%ds", secs)
	} else {
		mins := secs / 60
		secs = secs % 60
		elapsedStr = fmt.Sprintf("%dm %ds", mins, secs)
	}

	// Format tokens
	var tokensStr string
	if tokens >= 1000 {
		tokensStr = fmt.Sprintf("%.1fk", float64(tokens)/1000)
	} else {
		tokensStr = fmt.Sprintf("%d", tokens)
	}

	return fmt.Sprintf("%s(Esc to cancel · %s · ↓ %s tokens · %s)%s",
		muted, elapsedStr, tokensStr, state, reset)
}
