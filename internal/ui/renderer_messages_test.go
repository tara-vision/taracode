package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
)

// captureStdout runs fn with os.Stdout replaced by a pipe and returns everything it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()
	func() {
		defer func() { os.Stdout = original }() // also when fn fails the test
		fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

func TestRendererMessages(t *testing.T) {
	r := NewRenderer()
	tests := []struct {
		name, got string
		want      []string
	}{
		{"welcome", r.WelcomeMessage(), []string{IconCloud + " Tara Code", "DevOps & Cloud SI Assistant",
			"Type '/help' for commands, 'exit' to quit"}},
		{"context loaded", r.ProjectContextMessage(true), []string{IconFolder + " Project context loaded from TARACODE.md"}},
		{"context missing", r.ProjectContextMessage(false), []string{IconTip + " Run '/init' to initialize project context"}},
		{"resume", r.SessionResumeMessage(7), []string{IconSession + " Resuming session with 7 previous messages",
			"Type '/session new' to start fresh"}},
		{"prompt", r.PromptString(), []string{"\u276f "}},
		{"error", r.ErrorMessage(errors.New("disk full")), []string{IconError + " Error: disk full"}},
		{"provider", r.ProviderMessage(&provider.Info{Name: "Ollama"}), []string{IconSuccess + " Connected to Ollama"}},
		{"usage", r.FormatUsage(&storage.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}),
			[]string{IconInfo + " Token Usage", "Prompt tokens:     100", "Completion tokens: 20", "Total tokens:      120"}},
		{"no usage", r.FormatUsage(&storage.TokenUsage{}), []string{"No token usage recorded yet."}},
		{"nil usage", r.FormatUsage(nil), []string{"No token usage recorded yet."}},
		{"dim", r.Dim("thinking"), []string{"thinking"}},
		{"tool status", r.FormatToolStatus("write_file", map[string]interface{}{"path": "dir/app.yaml"}, "", false),
			[]string{IconSuccess + " Wrote app.yaml"}},
		{"web search", r.FormatToolStatus("web_search", map[string]interface{}{"query": "helm"}, "1. A\n2. B", false),
			[]string{`Searched "helm" (2 results)`}},
	}
	for _, tt := range tests {
		for _, want := range tt.want {
			if !strings.Contains(tt.got, want) {
				t.Errorf("%s: %q lacks %q", tt.name, tt.got, want)
			}
		}
	}
	if r.ProviderMessage(nil) != "" {
		t.Error("no provider, no message")
	}
	if r.SessionResumeMessage(7) == r.SessionResumeMessage(8) {
		t.Error("the resume message names the count")
	}
}

func TestDimWithoutColourIsPlain(t *testing.T) {
	plain := NewRendererWithConfig(&Config{EnableColor: false})
	if got := plain.Dim("reasoning"); got != "reasoning" {
		t.Fatalf("Dim() = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{999, ""},
		{1500, " [1.5s]"},
		{59000, " [59.0s]"},
		{125000, " [2m5s]"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.ms); got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestTruncateIDAndString(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{TruncateID("0123456789abcdef", 0), "01234567"},
		{TruncateID("0123456789abcdef", 4), "0123"},
		{TruncateID("short", 8), "short"},
		{TruncateString("abcdefghij", 20), "abcdefghij"},
		{TruncateString("abcdefghij", 8), "abcde..."},
		{TruncateString("abcdefghij", 3), "abc"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestFormatEnhancedErrorLinksTheDocs(t *testing.T) {
	got := FormatEnhancedError("boom", ErrorSuggestion{Title: "Broken", Suggestions: []string{"fix it"},
		DocURL: "https://code.tara.vision/docs"})
	for _, want := range []string{"Error: Broken", "  boom", IconTip + " fix it", "Learn more: https://code.tara.vision/docs"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

func TestSuggestSimilarToolsKeepsTheBestThree(t *testing.T) {
	if got := SuggestSimilarTools("read", nil); got != nil {
		t.Fatalf("no tools: %v", got)
	}
	got := SuggestSimilarTools("read_file", []string{"read_file", "read_dir", "readme", "reader", "write_file"})
	if len(got) != 3 || got[0] != "read_file" {
		t.Fatalf("SuggestSimilarTools() = %v", got)
	}
}

func TestDisplayMessages(t *testing.T) {
	tests := []struct {
		name string
		fn   func()
		want string
	}{
		{"denied", func() { DisplayPermissionDenied("kubectl") }, IconError + " Tool 'kubectl' blocked by permission settings"},
		{"saved", func() { DisplayPermissionSaved("kubectl", "allow") }, IconSuccess + " Saved: kubectl " + IconArrow + " allow"},
		{"applied both", func() { DisplayEditApplied("app.go", 3, 1) }, IconSuccess + " Applied edit to app.go (+3, -1 lines)"},
		{"applied added", func() { DisplayEditApplied("app.go", 2, 0) }, "(+2 lines)"},
		{"applied removed", func() { DisplayEditApplied("app.go", 0, 4) }, "(-4 lines)"},
		{"applied modified", func() { DisplayEditApplied("app.go", 0, 0) }, "(modified)"},
		{"cancelled", func() { DisplayEditCancelled("app.go") }, IconWarning + " Edit cancelled: app.go"},
		{"backup", func() { DisplayBackupCreated("/b/app.go.1") }, "  Backup saved: /b/app.go.1"},
	}
	for _, tt := range tests {
		if out := captureStdout(t, tt.fn); !strings.Contains(out, tt.want) {
			t.Errorf("%s: %q lacks %q", tt.name, out, tt.want)
		}
	}
}

func TestFormatParams(t *testing.T) {
	if got := formatParams(nil); got != "" {
		t.Fatalf("no params: %q", got)
	}
	if got := formatParams(map[string]interface{}{"path": "a.txt"}); got != "path=a.txt" {
		t.Fatalf("one param: %q", got)
	}
	long := strings.Repeat("v", 60)
	if got := formatParams(map[string]interface{}{"content": long}); got != "content="+long[:47]+"..." {
		t.Fatalf("a long value is cut at 50: %q", got)
	}
	many := map[string]interface{}{"a": strings.Repeat("1", 40), "b": strings.Repeat("2", 40), "c": strings.Repeat("3", 40)}
	if got := formatParams(many); len(got) != 80 || !strings.HasSuffix(got, "...") {
		t.Fatalf("the summary is cut at 80: %q (%d)", got, len(got))
	}
}

func TestSearchFallbackMessageKeepsAShortReason(t *testing.T) {
	got := NewRenderer().SearchFallbackMessage("Brave", "DuckDuckGo", errors.New("bad gateway"))
	if !strings.Contains(got, "Search: Brave "+IconArrow+" DuckDuckGo (bad gateway)") {
		t.Fatalf("%q", got)
	}
}
