package cmd

import (
	"strings"
	"testing"
)

func TestDetectSuggestion(t *testing.T) {
	tests := []struct {
		response, want string
	}{
		{"Delete the namespace? [y/N]", "n"},
		{"Apply the plan? [Y/n]", "y"},
		{"Continue? [y/n]", "y"},
		{"Continue (y/n)", "y"},
		{"  Would you like me to check the logs?  ", "yes"},
		{"Should I restart the pod?", "yes"},
		{"Do you want the full diff?", "yes"},
		{"Shall I apply it?", "yes"},
		{"Would you like me to check the logs. I can.", ""},
		{"The pod is healthy.", ""},
	}
	for _, tt := range tests {
		if got := DetectSuggestion(tt.response); got != tt.want {
			t.Errorf("DetectSuggestion(%q) = %q, want %q", tt.response, got, tt.want)
		}
	}
}

func completions(matches [][]rune) string {
	parts := make([]string, len(matches))
	for i, m := range matches {
		parts[i] = string(m)
	}
	return strings.Join(parts, ",")
}

func TestSlashCompleterDo(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, "main.go")
	sc := NewSlashCompleter(dir)

	sc.SetSuggestion("yes")
	if matches, length := sc.Do(nil, 0); completions(matches) != "yes" || length != 0 || sc.GetSuggestion() != "yes" {
		t.Fatalf("an empty line offers the suggestion: %v %d", matches, length)
	}
	sc.ClearSuggestion()
	if matches, length := sc.Do(nil, 0); matches != nil || length != 0 {
		t.Fatalf("no suggestion, nothing offered: %v %d", matches, length)
	}

	tests := []struct {
		line, want string
		length     int
	}{
		{"/mo", "del,de,de investigate,de operate", 3},
		{"/zzz", "", 0},
		{"look at @mai", "main.go", 3},
		{"plain text", "", 0},
	}
	for _, tt := range tests {
		line := []rune(tt.line)
		matches, length := sc.Do(line, len(line))
		if completions(matches) != tt.want || length != tt.length {
			t.Errorf("Do(%q) = %q, %d; want %q, %d", tt.line, completions(matches), length, tt.want, tt.length)
		}
	}
}

func TestGetPromptCompleterCompletesSlashCommands(t *testing.T) {
	completer := GetPromptCompleter(t.TempDir())
	line := []rune("/he")
	if matches, length := completer.Do(line, len(line)); completions(matches) != "lp" || length != 3 {
		t.Fatalf("Do(/he) = %v, %d", matches, length)
	}
}
