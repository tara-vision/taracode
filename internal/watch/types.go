package watch

import (
	"time"
)

// WatchState represents the current state of the monitor
type WatchState int

const (
	StateIdle WatchState = iota
	StateMonitoring
	StateAnalyzing
)

// String returns the string representation of WatchState
func (s WatchState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateMonitoring:
		return "monitoring"
	case StateAnalyzing:
		return "analyzing"
	default:
		return "unknown"
	}
}

// FindingType represents the type of finding from analysis
type FindingType string

const (
	FindingError       FindingType = "ERROR"
	FindingWarning     FindingType = "WARNING"
	FindingImprovement FindingType = "IMPROVEMENT"
)

// Finding represents a single detected issue from screen analysis
type Finding struct {
	Type        FindingType `json:"type"`
	Description string      `json:"description"`
	Suggestion  string      `json:"suggestion,omitempty"`
}

// AnalysisResult contains the LLM's findings from analyzing a screenshot
type AnalysisResult struct {
	Timestamp    time.Time `json:"timestamp"`
	ScreenCount  int       `json:"screen_count"`
	Findings     []Finding `json:"findings"`
	RawResponse  string    `json:"raw_response,omitempty"`
	AnalysisTime time.Duration `json:"analysis_time"`
}

// HasFindings returns true if the analysis found any issues
func (r *AnalysisResult) HasFindings() bool {
	return len(r.Findings) > 0
}

// ErrorCount returns the number of ERROR findings
func (r *AnalysisResult) ErrorCount() int {
	count := 0
	for _, f := range r.Findings {
		if f.Type == FindingError {
			count++
		}
	}
	return count
}

// WarningCount returns the number of WARNING findings
func (r *AnalysisResult) WarningCount() int {
	count := 0
	for _, f := range r.Findings {
		if f.Type == FindingWarning {
			count++
		}
	}
	return count
}

// ScreenCapture represents a captured screenshot
type ScreenCapture struct {
	Path       string    `json:"path"`
	DisplayID  int       `json:"display_id"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Timestamp  time.Time `json:"timestamp"`
	Hash       uint64    `json:"hash"`
}

// WatchConfig holds configuration for the watch monitor
type WatchConfig struct {
	// Enabled determines if the watch feature is active
	Enabled bool `mapstructure:"enabled"`

	// ChangeThreshold is the percentage difference (0.0-1.0) that triggers analysis
	// Default: 0.15 (15%)
	ChangeThreshold float64 `mapstructure:"change_threshold"`

	// CaptureInterval is how often to check for screen changes
	// Default: 2s
	CaptureInterval time.Duration `mapstructure:"capture_interval"`

	// DebounceInterval is the minimum time between analyses
	// Default: 5s
	DebounceInterval time.Duration `mapstructure:"debounce_interval"`

	// MaxAnalysisPerMin is the rate limit for analyses per minute
	// Default: 6
	MaxAnalysisPerMin int `mapstructure:"max_analysis_per_min"`

	// Notify enables macOS system notifications
	// Default: true
	Notify bool `mapstructure:"notify"`
}

// DefaultConfig returns the default watch configuration
func DefaultConfig() WatchConfig {
	return WatchConfig{
		Enabled:           true,
		ChangeThreshold:   0.15,
		CaptureInterval:   2 * time.Second,
		DebounceInterval:  5 * time.Second,
		MaxAnalysisPerMin: 6,
		Notify:            true,
	}
}

// MonitorStatus provides information about the current monitoring state
type MonitorStatus struct {
	State           WatchState    `json:"state"`
	DisplayCount    int           `json:"display_count"`
	TotalCaptures   int           `json:"total_captures"`
	TotalAnalyses   int           `json:"total_analyses"`
	LastCapture     time.Time     `json:"last_capture,omitempty"`
	LastAnalysis    time.Time     `json:"last_analysis,omitempty"`
	AnalysesThisMin int           `json:"analyses_this_min"`
	Uptime          time.Duration `json:"uptime,omitempty"`
	StartTime       time.Time     `json:"start_time,omitempty"`
}

// AnalysisPrompt is the prompt template for screen analysis
const AnalysisPrompt = `Analyze this screenshot for DevOps/development issues:
1. Error messages (red text, error dialogs, stack traces, failed commands)
2. Warnings (yellow/orange alerts, deprecation notices)
3. Improvement opportunities (inefficient patterns, security issues)

Context: The user is a DevOps engineer working with Kubernetes, Docker, Terraform, and cloud services.

Be concise. Format findings as:
- [ERROR] Brief description
- [WARNING] Brief description
- [IMPROVEMENT] Brief description

If nothing notable found, say "No issues detected."`
