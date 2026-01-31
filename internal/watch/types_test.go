package watch

import (
	"testing"
	"time"
)

func TestWatchStateString(t *testing.T) {
	tests := []struct {
		state    WatchState
		expected string
	}{
		{StateIdle, "idle"},
		{StateMonitoring, "monitoring"},
		{StateAnalyzing, "analyzing"},
		{WatchState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			if result != tt.expected {
				t.Errorf("WatchState(%d).String() = %s, want %s", tt.state, result, tt.expected)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Enabled != true {
		t.Error("DefaultConfig().Enabled should be true")
	}
	if config.ChangeThreshold != 0.15 {
		t.Errorf("DefaultConfig().ChangeThreshold = %f, want 0.15", config.ChangeThreshold)
	}
	if config.CaptureInterval != 2*time.Second {
		t.Errorf("DefaultConfig().CaptureInterval = %v, want 2s", config.CaptureInterval)
	}
	if config.DebounceInterval != 5*time.Second {
		t.Errorf("DefaultConfig().DebounceInterval = %v, want 5s", config.DebounceInterval)
	}
	if config.MaxAnalysisPerMin != 6 {
		t.Errorf("DefaultConfig().MaxAnalysisPerMin = %d, want 6", config.MaxAnalysisPerMin)
	}
	if config.Notify != true {
		t.Error("DefaultConfig().Notify should be true")
	}
}

func TestAnalysisResult(t *testing.T) {
	result := &AnalysisResult{
		Timestamp:   time.Now(),
		ScreenCount: 2,
		Findings: []Finding{
			{Type: FindingError, Description: "Error 1"},
			{Type: FindingError, Description: "Error 2"},
			{Type: FindingWarning, Description: "Warning 1"},
			{Type: FindingImprovement, Description: "Improvement 1"},
		},
	}

	if !result.HasFindings() {
		t.Error("HasFindings() should return true")
	}

	if result.ErrorCount() != 2 {
		t.Errorf("ErrorCount() = %d, want 2", result.ErrorCount())
	}

	if result.WarningCount() != 1 {
		t.Errorf("WarningCount() = %d, want 1", result.WarningCount())
	}
}

func TestAnalysisResultEmpty(t *testing.T) {
	result := &AnalysisResult{
		Timestamp:   time.Now(),
		ScreenCount: 1,
		Findings:    []Finding{},
	}

	if result.HasFindings() {
		t.Error("HasFindings() should return false for empty findings")
	}

	if result.ErrorCount() != 0 {
		t.Errorf("ErrorCount() = %d, want 0", result.ErrorCount())
	}
}
