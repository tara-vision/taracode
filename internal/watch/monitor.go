package watch

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/assistant"
)

// AnalyzeFunc is the function signature for analyzing screenshots
type AnalyzeFunc func(prompt string, images []*assistant.ImageData) (string, error)

// WatchMonitor coordinates screen monitoring and analysis
type WatchMonitor struct {
	config     WatchConfig
	analyzeFunc AnalyzeFunc
	tempDir    string

	// State management
	mu             sync.RWMutex
	state          WatchState
	startTime      time.Time
	lastCapture    time.Time
	lastAnalysis   time.Time
	totalCaptures  int
	totalAnalyses  int
	displayCount   int

	// Previous capture hash for change detection
	lastHash uint64

	// Rate limiting
	analysisTimestamps []time.Time

	// Control channels
	stopCh   chan struct{}
	resultCh chan *AnalysisResult
	doneCh   chan struct{}
}

// NewWatchMonitor creates a new monitor with the given configuration
func NewWatchMonitor(config WatchConfig, analyzeFunc AnalyzeFunc, tempDir string) *WatchMonitor {
	return &WatchMonitor{
		config:      config,
		analyzeFunc: analyzeFunc,
		tempDir:     tempDir,
		state:       StateIdle,
		resultCh:    make(chan *AnalysisResult, 10),
	}
}

// Start begins continuous screen monitoring
func (m *WatchMonitor) Start() error {
	m.mu.Lock()
	if m.state != StateIdle {
		m.mu.Unlock()
		return fmt.Errorf("monitor already running")
	}

	m.state = StateMonitoring
	m.startTime = time.Now()
	m.stopCh = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.displayCount = GetDisplayCount()
	m.mu.Unlock()

	// Start background monitoring goroutine
	go m.monitorLoop()

	return nil
}

// Stop halts the monitoring loop
func (m *WatchMonitor) Stop() {
	m.mu.Lock()
	if m.state == StateIdle {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	// Signal stop
	close(m.stopCh)

	// Wait for monitor loop to finish
	<-m.doneCh

	m.mu.Lock()
	m.state = StateIdle
	m.mu.Unlock()
}

// Status returns the current monitor status
func (m *WatchMonitor) Status() MonitorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := MonitorStatus{
		State:           m.state,
		DisplayCount:    m.displayCount,
		TotalCaptures:   m.totalCaptures,
		TotalAnalyses:   m.totalAnalyses,
		LastCapture:     m.lastCapture,
		LastAnalysis:    m.lastAnalysis,
		AnalysesThisMin: m.countRecentAnalyses(),
	}

	if m.state != StateIdle {
		status.StartTime = m.startTime
		status.Uptime = time.Since(m.startTime)
	}

	return status
}

// Results returns the channel for receiving analysis results
func (m *WatchMonitor) Results() <-chan *AnalysisResult {
	return m.resultCh
}

// IsRunning returns true if the monitor is actively running
func (m *WatchMonitor) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state != StateIdle
}

// monitorLoop is the main background loop for continuous monitoring
func (m *WatchMonitor) monitorLoop() {
	defer close(m.doneCh)

	ticker := time.NewTicker(m.config.CaptureInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.captureAndAnalyze()
		}
	}
}

// captureAndAnalyze performs one capture cycle
func (m *WatchMonitor) captureAndAnalyze() {
	// Capture screenshot
	capture, err := CaptureOne(m.tempDir)
	if err != nil {
		// Silently skip failed captures
		return
	}
	defer CleanupPath(capture.Path)

	m.mu.Lock()
	m.totalCaptures++
	m.lastCapture = time.Now()
	m.mu.Unlock()

	// Compute hash for change detection
	hash, err := ComputeHash(capture.Path)
	if err != nil {
		return
	}
	capture.Hash = hash

	// Check if change is significant
	m.mu.RLock()
	lastHash := m.lastHash
	m.mu.RUnlock()

	if lastHash != 0 && !HasSignificantChange(lastHash, hash, m.config.ChangeThreshold) {
		// No significant change, skip analysis
		return
	}

	// Check debounce
	m.mu.RLock()
	timeSinceLastAnalysis := time.Since(m.lastAnalysis)
	m.mu.RUnlock()

	if timeSinceLastAnalysis < m.config.DebounceInterval {
		return
	}

	// Check rate limit
	if !m.canAnalyze() {
		return
	}

	// Perform analysis
	m.mu.Lock()
	m.state = StateAnalyzing
	m.mu.Unlock()

	result, err := m.analyze(capture)

	m.mu.Lock()
	m.state = StateMonitoring
	m.lastHash = hash
	if err == nil && result != nil {
		m.totalAnalyses++
		m.lastAnalysis = time.Now()
		m.recordAnalysis()
	}
	m.mu.Unlock()

	// Send result if we have findings
	if result != nil && result.HasFindings() {
		select {
		case m.resultCh <- result:
		default:
			// Channel full, drop oldest result
			select {
			case <-m.resultCh:
			default:
			}
			m.resultCh <- result
		}

		// Send notification if enabled
		if m.config.Notify {
			SendAnalysisNotification(result)
		}
	}
}

// analyze sends a screenshot to the LLM for analysis
func (m *WatchMonitor) analyze(capture *ScreenCapture) (*AnalysisResult, error) {
	startTime := time.Now()

	// Load image for analysis
	img, err := assistant.LoadImage(capture.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to load screenshot: %w", err)
	}

	// Call the analysis function
	response, err := m.analyzeFunc(AnalysisPrompt, []*assistant.ImageData{img})
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	// Parse response into findings
	findings := parseFindings(response)

	return &AnalysisResult{
		Timestamp:    time.Now(),
		ScreenCount:  1,
		Findings:     findings,
		RawResponse:  response,
		AnalysisTime: time.Since(startTime),
	}, nil
}

// canAnalyze checks if we're within the rate limit
func (m *WatchMonitor) canAnalyze() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := m.countRecentAnalyses()
	return count < m.config.MaxAnalysisPerMin
}

// countRecentAnalyses returns the number of analyses in the last minute
func (m *WatchMonitor) countRecentAnalyses() int {
	cutoff := time.Now().Add(-time.Minute)
	count := 0
	for _, t := range m.analysisTimestamps {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// recordAnalysis adds a timestamp for rate limiting
func (m *WatchMonitor) recordAnalysis() {
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Clean old timestamps
	newTimestamps := make([]time.Time, 0, len(m.analysisTimestamps)+1)
	for _, t := range m.analysisTimestamps {
		if t.After(cutoff) {
			newTimestamps = append(newTimestamps, t)
		}
	}
	newTimestamps = append(newTimestamps, now)
	m.analysisTimestamps = newTimestamps
}

// parseFindings extracts findings from the LLM response
func parseFindings(response string) []Finding {
	findings := []Finding{}

	// Look for patterns like [ERROR], [WARNING], [IMPROVEMENT]
	lines := strings.Split(response, "\n")

	// Patterns for each finding type
	errorPattern := regexp.MustCompile(`(?i)^\s*-?\s*\[ERROR\]\s*(.+)`)
	warningPattern := regexp.MustCompile(`(?i)^\s*-?\s*\[WARNING\]\s*(.+)`)
	improvementPattern := regexp.MustCompile(`(?i)^\s*-?\s*\[IMPROVEMENT\]\s*(.+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if match := errorPattern.FindStringSubmatch(line); len(match) > 1 {
			findings = append(findings, Finding{
				Type:        FindingError,
				Description: strings.TrimSpace(match[1]),
			})
		} else if match := warningPattern.FindStringSubmatch(line); len(match) > 1 {
			findings = append(findings, Finding{
				Type:        FindingWarning,
				Description: strings.TrimSpace(match[1]),
			})
		} else if match := improvementPattern.FindStringSubmatch(line); len(match) > 1 {
			findings = append(findings, Finding{
				Type:        FindingImprovement,
				Description: strings.TrimSpace(match[1]),
			})
		}
	}

	return findings
}

// AnalyzeOnce performs a one-time screen capture and analysis
func AnalyzeOnce(ctx context.Context, analyzeFunc AnalyzeFunc, tempDir string) (*AnalysisResult, error) {
	startTime := time.Now()

	// Check context before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Capture screenshot
	capture, err := CaptureOne(tempDir)
	if err != nil {
		return nil, fmt.Errorf("capture failed: %w", err)
	}
	defer CleanupPath(capture.Path)

	// Check context after capture
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Load image
	img, err := assistant.LoadImage(capture.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to load screenshot: %w", err)
	}

	// Run analysis in a goroutine so we can respect context cancellation
	type analyzeResult struct {
		response string
		err      error
	}
	resultCh := make(chan analyzeResult, 1)

	go func() {
		response, err := analyzeFunc(AnalysisPrompt, []*assistant.ImageData{img})
		resultCh <- analyzeResult{response, err}
	}()

	// Wait for either result or context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, fmt.Errorf("analysis failed: %w", result.err)
		}

		findings := parseFindings(result.response)

		return &AnalysisResult{
			Timestamp:    time.Now(),
			ScreenCount:  GetDisplayCount(),
			Findings:     findings,
			RawResponse:  result.response,
			AnalysisTime: time.Since(startTime),
		}, nil
	}
}
