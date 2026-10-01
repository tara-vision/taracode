package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/tara-vision/taracode/internal/agent"
	"github.com/tara-vision/taracode/internal/storage"
)

func TestFormatBoxLinePadsAndCuts(t *testing.T) {
	if got := formatBoxLine("abc"); got != "\u2502  abc"+strings.Repeat(" ", boxWidth-3)+"\u2502" {
		t.Fatalf("formatBoxLine(abc) = %q", got)
	}
	long := strings.Repeat("x", boxWidth+5)
	if got := formatBoxLine(long); got != "\u2502  "+strings.Repeat("x", boxWidth-3)+"...\u2502" {
		t.Fatalf("formatBoxLine(long) = %q", got)
	}
}

func TestPrintContextBudget(t *testing.T) {
	over := agent.ContextInfo{
		TotalTokens: 40000, MaxTokens: 32768, SystemPromptTokens: 1200, ToolDefsTokens: 3400,
		ServerContextTokens: 16384, ContextWindow: 32768, ConversationTokens: 5000, MessageCount: 7,
		CompactionEvents: make([]agent.CompactionEvent, 2),
	}
	out := captureStdoutForTest(t, func() { printContextBudget(over) })
	for _, want := range []string{
		"Context Budget: 40.0k / 32.8k tokens (122%)", "System prompt:    1.2k tokens", "Tool definitions: 3.4k tokens",
		"Server context:   16.4k tokens (Ollama num_ctx)", "Context window (requested): 32.8k tokens",
		"Conversation:     5.0k tokens (7 messages, 2 compactions)", "Available:        0.0k tokens",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("budget lacks %q:\n%s", want, out)
		}
	}

	out = captureStdoutForTest(t, func() { printContextBudget(agent.ContextInfo{TotalTokens: 500, MessageCount: 1}) })
	if !strings.Contains(out, "(0%)") || strings.Contains(out, "Server context") || strings.Contains(out, "requested") ||
		!strings.Contains(out, "(1 messages)") {
		t.Fatalf("an unknown window, server context and no compactions:\n%s", out)
	}
}

func TestPrintContextCompactionHistory(t *testing.T) {
	if out := captureStdoutForTest(t, func() { printContextCompactionHistory(agent.ContextInfo{}) }); out != "" {
		t.Fatalf("no compactions prints nothing: %q", out)
	}
	info := agent.ContextInfo{CompactionEvents: []agent.CompactionEvent{
		{TokensBefore: 30000, TokensAfter: 10000, MessagesBefore: 20, MessagesAfter: 12},
		{TokensBefore: 28000, TokensAfter: 9000, MessagesBefore: 18, MessagesAfter: 10},
	}}
	out := captureStdoutForTest(t, func() { printContextCompactionHistory(info) })
	for _, want := range []string{
		"Compaction History", "#1: 30.0k -> 10.0k (8 messages summarized)", "#2: 28.0k -> 9.0k (8 messages summarized)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("history lacks %q:\n%s", want, out)
		}
	}
}

func TestPrintContextTruncationEventsShowsTheFirstFive(t *testing.T) {
	if out := captureStdoutForTest(t, func() { printContextTruncationEvents(agent.ContextInfo{}) }); out != "" {
		t.Fatalf("no truncations prints nothing: %q", out)
	}
	var events []agent.TruncationResult
	for i := 1; i <= 7; i++ {
		events = append(events, agent.TruncationResult{ToolName: fmt.Sprintf("tool%d", i), OrigLines: 100 * i, KeptLines: 50})
	}
	out := captureStdoutForTest(t, func() { printContextTruncationEvents(agent.ContextInfo{TruncationEvents: events}) })
	for _, want := range []string{"Truncated Outputs: 7", "tool1: 100 -> 50 lines", "tool5: 500 -> 50 lines", "... and 2 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("truncations lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tool6") {
		t.Errorf("only the first five are listed:\n%s", out)
	}
}

func TestPrintContextSessionInfo(t *testing.T) {
	if out := captureStdoutForTest(t, func() { printContextSessionInfo(nil) }); out != "" {
		t.Fatalf("no session prints nothing: %q", out)
	}
	session := &storage.Session{ID: "0123456789abcdef", Messages: []storage.ConversationMessage{
		{Role: "user"}, {Role: "assistant", ToolCalls: make([]storage.ToolCallRecord, 2)}, {Role: "tool"},
		{Role: "user"}, {Role: "assistant"},
	}}
	out := captureStdoutForTest(t, func() { printContextSessionInfo(session) })
	for _, want := range []string{"Session: 01234567", "Messages: 2 user, 2 assistant", "Tool calls: 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("session info lacks %q:\n%s", want, out)
		}
	}
	quiet := &storage.Session{ID: "fedcba9876543210", Messages: []storage.ConversationMessage{{Role: "user"}}}
	if out := captureStdoutForTest(t, func() { printContextSessionInfo(quiet) }); strings.Contains(out, "Tool calls") {
		t.Fatalf("a session without tool calls has no tool-call line:\n%s", out)
	}
}

func TestExtractFilesReadKeepsTheFirstOfEachPath(t *testing.T) {
	messages := []storage.ConversationMessage{
		{Role: "assistant", ToolCalls: []storage.ToolCallRecord{
			{Tool: "read_file", Params: map[string]any{"path": "main.go"}},
			{Tool: "list_files", Params: map[string]any{"path": "src"}},
			{Tool: "read_file", Params: map[string]any{"file_path": "legacy.go"}},
		}},
		{Role: "assistant", ToolCalls: []storage.ToolCallRecord{
			{Tool: "read_file", Params: map[string]any{"path": "main.go"}},
			{Tool: "read_file", Params: map[string]any{}},
		}},
	}
	if got := strings.Join(extractFilesRead(messages), ","); got != "main.go,legacy.go" {
		t.Fatalf("extractFilesRead() = %s", got)
	}
}

func TestPrintContextFilesRead(t *testing.T) {
	for _, session := range []*storage.Session{nil, {}, {Messages: []storage.ConversationMessage{{Role: "user"}}}} {
		if out := captureStdoutForTest(t, func() { printContextFilesRead(session) }); out != "" {
			t.Fatalf("no files read prints nothing: %q", out)
		}
	}
	long := strings.Repeat("d/", 40) + "deep.go"
	calls := []storage.ToolCallRecord{{Tool: "read_file", Params: map[string]any{"path": long}}}
	for i := 1; i <= 11; i++ {
		calls = append(calls, storage.ToolCallRecord{Tool: "read_file", Params: map[string]any{"path": fmt.Sprintf("f%d.go", i)}})
	}
	session := &storage.Session{Messages: []storage.ConversationMessage{{Role: "assistant", ToolCalls: calls}}}
	out := captureStdoutForTest(t, func() { printContextFilesRead(session) })
	for _, want := range []string{"Files Read This Session", "  ..." + long[len(long)-57:], "f9.go", "... and 2 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("files read lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "f10.go") {
		t.Errorf("only the first ten are listed:\n%s", out)
	}
}

func TestPrintContextMemories(t *testing.T) {
	resetConfig(t)
	if out := captureStdoutForTest(t, func() { printContextMemories(nil) }); out != "" {
		t.Fatalf("no manager prints nothing: %q", out)
	}
	mm := newMemoryManager(t)
	if out := captureStdoutForTest(t, func() { printContextMemories(mm) }); out != "" {
		t.Fatalf("no memories prints nothing: %q", out)
	}
	long := strings.Repeat("y", 60)
	for _, content := range []string{long, "Second", "Third", "Fourth"} {
		mem, err := mm.Create(storage.MemoryCategoryLearning, content, "", nil, storage.MemorySourceManual)
		if err != nil {
			t.Fatal(err)
		}
		if content == long { // the most used memory ranks first, so it is one of the three shown
			if err := mm.IncrementUseCount(mem.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	out := captureStdoutForTest(t, func() { printContextMemories(mm) })
	if !strings.Contains(out, "Project Memories: 4 total, 4 in context") || !strings.Contains(out, "... and 1 more") ||
		strings.Count(out, "[learning]") != 3 {
		t.Fatalf("memories section:\n%s", out)
	}
	if !strings.Contains(out, "[learning] "+long[:47]+"...") || strings.Contains(out, long) {
		t.Errorf("a long memory is cut to 47 characters:\n%s", out)
	}

	viper.Set("memory.max_context_tokens", 1) // too small a budget for any memory
	out = captureStdoutForTest(t, func() { printContextMemories(mm) })
	if !strings.Contains(out, "Project Memories: 4 total, 0 in context") || strings.Contains(out, "[learning]") {
		t.Fatalf("a budget that fits nothing lists nothing:\n%s", out)
	}
}
