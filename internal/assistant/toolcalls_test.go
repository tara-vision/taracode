package assistant

import (
	"strings"
	"testing"
)

// TestParseToolCallsSingleObject covers the plain case: one well-formed tool call and nothing
// else in the reply, so there is no leading text to display.
func TestParseToolCallsSingleObject(t *testing.T) {
	calls, before := parseToolCalls(`{"tool":"read_file","params":{"file_path":"a"}}`)

	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want 1", calls)
	}
	if calls[0].Tool != "read_file" || calls[0].Params["file_path"] != "a" {
		t.Fatalf("call = %+v", calls[0])
	}
	if before != "" {
		t.Fatalf("text before = %q, want empty", before)
	}
}

// TestParseToolCallsTwoObjectsInOneReply covers a reply that asks for two tools at once, with
// prose ahead of the first call that must survive as the displayed text.
func TestParseToolCallsTwoObjectsInOneReply(t *testing.T) {
	reply := "I will read both files.\n" +
		`{"tool":"read_file","params":{"file_path":"a"}}` + "\n" +
		"Then the second one.\n" +
		`{"tool":"read_file","params":{"file_path":"b"}}`

	calls, before := parseToolCalls(reply)

	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want 2", calls)
	}
	if calls[0].Params["file_path"] != "a" || calls[1].Params["file_path"] != "b" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(before, "I will read both files.") {
		t.Fatalf("text before first tool call = %q", before)
	}
}

// TestParseToolCallsArrayOfToolCalls covers the JSON-array form the model sometimes uses instead
// of writing separate objects.
func TestParseToolCallsArrayOfToolCalls(t *testing.T) {
	reply := `[{"tool":"a","params":{}},{"tool":"b","params":{}}]`

	calls, _ := parseToolCalls(reply)

	if len(calls) != 2 || calls[0].Tool != "a" || calls[1].Tool != "b" {
		t.Fatalf("calls = %+v", calls)
	}
}

// TestParseToolCallsFallsBackToAlternativeShapes covers a reply with no "tool" key at all: the
// fallback converter recognizes the write_file shape and parseToolCalls picks up what it finds.
func TestParseToolCallsFallsBackToAlternativeShapes(t *testing.T) {
	reply := `{"file_path":"notes.txt","content":"hello"}`

	calls, _ := parseToolCalls(reply)

	if len(calls) != 1 || calls[0].Tool != "write_file" || calls[0].Params["file_path"] != "notes.txt" {
		t.Fatalf("calls = %+v", calls)
	}
}

// TestTryConvertToToolCallAlternativeNameSpellings covers the "tool_name" and "action" spellings
// tryConvertToToolCall recognizes in place of the standard "tool" key.
func TestTryConvertToToolCallAlternativeNameSpellings(t *testing.T) {
	byName := tryConvertToToolCall(`{"tool_name":"read_file","file_path":"a"}`)
	if byName == nil || byName.Tool != "read_file" || byName.Params["file_path"] != "a" {
		t.Fatalf("tool_name spelling: %+v", byName)
	}

	byAction := tryConvertToToolCall(`{"action":"list_files","directory":"."}`)
	if byAction == nil || byAction.Tool != "list_files" || byAction.Params["directory"] != "." {
		t.Fatalf("action spelling: %+v", byAction)
	}
}

// TestTryConvertToToolCallOtherPatterns covers the bare command pattern plus the two ways
// conversion refuses. detectReadFilePattern's action-gated shape is exercised separately below:
// through this entry point it is shadowed by detectAlternativeToolNamePattern (see that test).
func TestTryConvertToToolCallOtherPatterns(t *testing.T) {
	cmd := tryConvertToToolCall(`{"command":"ls -la"}`)
	if cmd == nil || cmd.Tool != "execute_command" || cmd.Params["command"] != "ls -la" {
		t.Fatalf("command pattern: %+v", cmd)
	}

	if tc := tryConvertToToolCall(`{"tool":"read_file","params":{}}`); tc != nil {
		t.Fatalf("a call that already has a tool key must not be converted: %+v", tc)
	}
	if tc := tryConvertToToolCall(`{"unrelated":"value"}`); tc != nil {
		t.Fatalf("an unrecognized shape must not be converted: %+v", tc)
	}
}

// TestDetectReadFilePatternRecognizesTheActionGatedShape unit-tests detectReadFilePattern
// directly rather than through tryConvertToToolCall.
//
// This is deliberate, not a style choice: through tryConvertToToolCall (its only caller),
// detectAlternativeToolNamePattern runs first and treats ANY non-empty "action" value, including
// exactly "read", "open" or "get", as an alternative tool NAME. So
// {"file_path": "a.txt", "action": "read"} is dispatched as a call to a tool literally named
// "read" (which does not exist in the registry) before detectReadFilePattern is ever reached.
// Confirmed directly: tryConvertToToolCall on that JSON returns Tool "read", not "read_file".
// detectReadFilePattern's action=="read"/"open"/"get" branch is therefore dead code in the
// current dispatch order - a pre-existing bug in this legacy fallback parser (already commented
// in toolcalls.go as "retired in Phase 2"), not something introduced or fixed by this task. This
// test documents the function's own intended behavior without asserting the shadowed bug.
func TestDetectReadFilePatternRecognizesTheActionGatedShape(t *testing.T) {
	tc := detectReadFilePattern(map[string]interface{}{"file_path": "a.txt", "action": "read"})
	if tc == nil || tc.Tool != "read_file" || tc.Params["file_path"] != "a.txt" {
		t.Fatalf("detectReadFilePattern = %+v", tc)
	}
}

// TestCleanResponseStripsThinkBlocks covers both the closed <think>...</think> case and an
// unclosed tag, which cleanResponse also strips.
func TestCleanResponseStripsThinkBlocks(t *testing.T) {
	closed := cleanResponse("<think>reasoning here</think>The answer is 42.")
	if closed != "The answer is 42." {
		t.Fatalf("closed tag: cleanResponse = %q", closed)
	}

	unclosed := cleanResponse("leftover reasoning</think>The answer is 43.")
	if unclosed != "The answer is 43." {
		t.Fatalf("unclosed tag: cleanResponse = %q", unclosed)
	}
}

// TestStreamFilterHidesThinkTagsAcrossChunks feeds the opening tag split mid-token, across two
// Process calls, to prove the filter buffers a partial tag instead of leaking it to the display.
func TestStreamFilterHidesThinkTagsAcrossChunks(t *testing.T) {
	f := NewStreamFilter()

	var visible strings.Builder
	visible.WriteString(f.Process("<thi"))
	visible.WriteString(f.Process("nk>hidden</think>shown"))
	visible.WriteString(f.Flush())

	if visible.String() != "shown" {
		t.Fatalf("visible output = %q, want %q", visible.String(), "shown")
	}
	if !strings.Contains(f.FullContent(), "hidden") {
		t.Fatalf("FullContent must keep the reasoning for tool-call parsing: %q", f.FullContent())
	}
}
