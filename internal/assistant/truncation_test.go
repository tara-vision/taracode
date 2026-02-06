package assistant

import (
	"strings"
	"testing"
)

func TestTruncateToolOutput_EmptyInput(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 10, MaxChars: 100}
	result := TruncateToolOutput("", "read_file", cfg)
	if result.WasTruncated {
		t.Error("empty input should not be truncated")
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestTruncateToolOutput_DisabledLimits(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 0, MaxChars: 0}
	input := strings.Repeat("line\n", 1000)
	result := TruncateToolOutput(input, "read_file", cfg)
	if result.WasTruncated {
		t.Error("should not truncate when limits are disabled")
	}
	if result.Output != input {
		t.Error("output should be unchanged")
	}
}

func TestTruncateToolOutput_UnderLimits(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 100, MaxChars: 10000}
	input := "line1\nline2\nline3\n"
	result := TruncateToolOutput(input, "execute_command", cfg)
	if result.WasTruncated {
		t.Error("should not truncate when under limits")
	}
	if result.Output != input {
		t.Error("output should be unchanged")
	}
}

func TestTruncateToolOutput_LineLimitExceeded(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 5, MaxChars: 0}
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "content line"
	}
	input := strings.Join(lines, "\n")

	result := TruncateToolOutput(input, "read_file", cfg)
	if !result.WasTruncated {
		t.Error("should be truncated when over line limit")
	}
	if result.OrigLines != 20 {
		t.Errorf("expected 20 original lines, got %d", result.OrigLines)
	}
	if result.KeptLines > 5 {
		t.Errorf("expected at most 5 kept lines, got %d", result.KeptLines)
	}
	if !strings.Contains(result.Output, "[Output truncated:") {
		t.Error("should contain truncation notice")
	}
}

func TestTruncateToolOutput_CharLimitExceeded(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 0, MaxChars: 50}
	input := strings.Repeat("abcdefghij\n", 20) // 220 chars
	result := TruncateToolOutput(input, "search_files", cfg)
	if !result.WasTruncated {
		t.Error("should be truncated when over char limit")
	}
	if result.KeptChars > 50 {
		t.Errorf("expected at most 50 kept chars, got %d", result.KeptChars)
	}
	if !strings.Contains(result.Output, "[Output truncated:") {
		t.Error("should contain truncation notice")
	}
}

func TestTruncateToolOutput_BothLimits(t *testing.T) {
	cfg := TruncationConfig{MaxLines: 5, MaxChars: 100}
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = strings.Repeat("x", 30)
	}
	input := strings.Join(lines, "\n")

	result := TruncateToolOutput(input, "execute_command", cfg)
	if !result.WasTruncated {
		t.Error("should be truncated")
	}
	if result.KeptLines > 5 {
		t.Errorf("expected at most 5 kept lines, got %d", result.KeptLines)
	}
}

func TestTruncateToolOutput_ToolSpecificHints(t *testing.T) {
	tests := []struct {
		tool string
		hint string
	}{
		{"read_file", "start_line/end_line"},
		{"search_files", "Narrow your search"},
		{"execute_command", "head/tail or grep"},
		{"kubectl_logs", "--tail or --since"},
		{"git_log", "max_count"},
		{"git_diff", "file paths"},
		{"unknown_tool", ""},
	}

	cfg := TruncationConfig{MaxLines: 2, MaxChars: 0}
	input := "line1\nline2\nline3\nline4\nline5\n"

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result := TruncateToolOutput(input, tt.tool, cfg)
			if tt.hint != "" && !strings.Contains(result.Output, tt.hint) {
				t.Errorf("expected hint %q in output for tool %s", tt.hint, tt.tool)
			}
		})
	}
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected bool
	}{
		{"object", []string{`{"key": "value"}`}, true},
		{"array", []string{`[1, 2, 3]`}, true},
		{"indented object", []string{"  {"}, true},
		{"plain text", []string{"hello world"}, false},
		{"empty", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeJSON(tt.lines)
			if result != tt.expected {
				t.Errorf("looksLikeJSON(%v) = %v, expected %v", tt.lines, result, tt.expected)
			}
		})
	}
}

func TestTruncateJSONBoundary(t *testing.T) {
	// JSON with clear object boundaries
	lines := []string{
		"[",
		"  {",
		`    "name": "first"`,
		"  },",
		"  {",
		`    "name": "second"`,
		"  },",
		"  {",
		`    "name": "third"`,
		"  }",
		"]",
	}

	result := truncateJSONBoundary(lines, 7)
	lastLine := strings.TrimSpace(result[len(result)-1])
	if lastLine != "}," {
		t.Errorf("expected truncation at JSON boundary, last line is %q", lastLine)
	}
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"text", "hello world", false},
		{"binary", "hello\x00world", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBinaryContent(tt.input)
			if result != tt.expected {
				t.Errorf("isBinaryContent(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncateBinaryOutput(t *testing.T) {
	// Non-binary content should pass through
	result := TruncateBinaryOutput("hello world", "read_file", 256)
	if result.WasTruncated {
		t.Error("non-binary content should not be truncated")
	}

	// Binary content should be truncated
	binary := "hello\x00world" + strings.Repeat("\x00", 500)
	result = TruncateBinaryOutput(binary, "read_file", 10)
	if !result.WasTruncated {
		t.Error("binary content should be truncated")
	}
	if !strings.Contains(result.Output, "Binary content detected") {
		t.Error("should contain binary notice")
	}
}

func TestBuildTruncationNotice(t *testing.T) {
	notice := buildTruncationNotice("read_file", 100, 5000, 50, 2500)
	if !strings.Contains(notice, "50 of 100 lines") {
		t.Error("should contain line count")
	}
	if !strings.Contains(notice, "2500 of 5000 chars") {
		t.Error("should contain char count")
	}
	if !strings.Contains(notice, "start_line") {
		t.Error("should contain read_file hint")
	}
}
