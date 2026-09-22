package assistant

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// StreamFilter handles real-time filtering of think tags during streaming
type StreamFilter struct {
	buffer      strings.Builder // Accumulates content that might be in a tag
	inThinkTag  bool            // Currently inside <think> block
	fullContent strings.Builder // Full unfiltered content for tool call parsing
}

// NewStreamFilter creates a new stream filter
func NewStreamFilter() *StreamFilter {
	return &StreamFilter{}
}

// Process handles a chunk of streaming content
// Returns the displayable portion (filters out <think> tags)
func (f *StreamFilter) Process(chunk string) string {
	f.fullContent.WriteString(chunk)

	var display strings.Builder

	for _, char := range chunk {
		if f.inThinkTag {
			f.buffer.WriteRune(char)
			// Check if buffer ends with </think>
			if strings.HasSuffix(f.buffer.String(), "</think>") {
				f.inThinkTag = false
				f.buffer.Reset()
			}
		} else {
			f.buffer.WriteRune(char)
			bufStr := f.buffer.String()

			// Check if we're starting a think tag
			if strings.HasPrefix("<think>", bufStr) {
				if bufStr == "<think>" {
					f.inThinkTag = true
					f.buffer.Reset()
				}
				// Otherwise keep buffering
			} else if strings.HasPrefix("<think", bufStr) { //nolint:revive // keep buffering
				// Partial match, keep buffering
			} else if len(bufStr) > 0 && bufStr[0] == '<' && len(bufStr) < 7 { //nolint:revive // keep buffering
				// Could still be <think, keep buffering up to 7 chars
			} else {
				// Not a think tag, flush buffer to display
				display.WriteString(bufStr)
				f.buffer.Reset()
			}
		}
	}

	return display.String()
}

// Flush returns any remaining buffered content (for end of stream)
func (f *StreamFilter) Flush() string {
	result := f.buffer.String()
	f.buffer.Reset()
	return result
}

// FullContent returns the complete unfiltered response
func (f *StreamFilter) FullContent() string {
	return f.fullContent.String()
}

// ToolCall represents a parsed tool call from the model's response
type ToolCall struct {
	ID     string                 `json:"id,omitempty"` // Tool call ID for native function calling
	Tool   string                 `json:"tool"`
	Params map[string]interface{} `json:"params"`
}

// cleanResponse removes thinking tags and extracts displayable content
func cleanResponse(response string) string {
	// Remove <think>...</think> blocks (DeepSeek R1 reasoning)
	thinkRe := regexp.MustCompile(`(?s)<think>.*?</think>`)
	cleaned := thinkRe.ReplaceAllString(response, "")

	// Also handle unclosed think tags
	if idx := strings.Index(cleaned, "</think>"); idx != -1 {
		cleaned = cleaned[idx+8:]
	}

	return strings.TrimSpace(cleaned)
}

// normalizeJSON cleans up JSON that may have been corrupted by model text wrapping
// It removes extra whitespace and newlines that aren't part of actual string content
func normalizeJSON(jsonStr string) string {
	// Remove carriage returns
	result := strings.ReplaceAll(jsonStr, "\r", "")

	// Process character by character to handle strings properly
	var normalized strings.Builder
	inString := false
	escaped := false

	for i := 0; i < len(result); i++ {
		c := result[i]

		if escaped {
			normalized.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && inString {
			normalized.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			normalized.WriteByte(c)
			continue
		}

		if inString {
			// Inside a string - convert actual newlines to \n escape sequence
			switch c {
			case '\n':
				normalized.WriteString("\\n")
			case '\t':
				normalized.WriteString("\\t")
			default:
				normalized.WriteByte(c)
			}
		} else {
			// Outside string - skip whitespace except single spaces
			if c == '\n' || c == '\t' {
				// Skip newlines and tabs outside strings
				continue
			} else if c == ' ' {
				// Collapse multiple spaces to single space
				if normalized.Len() > 0 {
					lastChar := normalized.String()[normalized.Len()-1]
					if lastChar != ' ' && lastChar != '{' && lastChar != '[' && lastChar != ':' && lastChar != ',' {
						normalized.WriteByte(c)
					}
				}
			} else {
				normalized.WriteByte(c)
			}
		}
	}

	return normalized.String()
}

// tryConvertToToolCall attempts to convert alternative JSON formats to standard tool call format.
// This handles models that output {"file_name": "X", "content": "Y"} instead of proper tool format,
// or {"tool_code": "web_search", "query": "..."} with flat params instead of nested "params" object.
// It tries each recognized pattern in turn (legacy JSON fallback parser, removed in 3.0).
func tryConvertToToolCall(jsonStr string) *ToolCall {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil
	}

	// Already has "tool" key - not a malformed call
	if _, hasToolKey := raw["tool"]; hasToolKey {
		return nil
	}

	if tc := detectAlternativeToolNamePattern(raw); tc != nil {
		return tc
	}
	if tc := detectWriteFilePattern(raw); tc != nil {
		return tc
	}
	if tc := detectReadFilePattern(raw); tc != nil {
		return tc
	}
	if tc := detectCommandExecutionPattern(raw); tc != nil {
		return tc
	}

	return nil
}

// detectAlternativeToolNamePattern handles alternative tool name keys:
// "tool_code", "tool_name", "function", "action"
func detectAlternativeToolNamePattern(raw map[string]interface{}) *ToolCall {
	var toolName string
	for _, key := range []string{"tool_code", "tool_name", "function", "action"} {
		if name, ok := raw[key].(string); ok && name != "" {
			toolName = name
			break
		}
	}
	if toolName == "" {
		return nil
	}
	// Collect remaining keys as params (exclude the tool name key and known metadata)
	params := make(map[string]interface{})
	skipKeys := map[string]bool{
		"tool_code": true, "tool_name": true, "function": true,
		"action": true, "source": true, "id": true,
	}
	for k, v := range raw {
		if !skipKeys[k] {
			params[k] = v
		}
	}
	return &ToolCall{
		Tool:   toolName,
		Params: params,
	}
}

// detectWriteFilePattern detects the write_file pattern: has "content" and some file path key
func detectWriteFilePattern(raw map[string]interface{}) *ToolCall {
	content, hasContent := raw["content"]
	if !hasContent {
		return nil
	}
	var filePath string
	for _, key := range []string{"file_path", "file_name", "filename", "path", "name"} {
		if fp, ok := raw[key].(string); ok && fp != "" {
			filePath = fp
			break
		}
	}
	if filePath == "" {
		return nil
	}
	return &ToolCall{
		Tool: "write_file",
		Params: map[string]interface{}{
			"file_path": filePath,
			"content":   content,
		},
	}
}

// detectReadFilePattern detects the read_file pattern: has a file path but no content
func detectReadFilePattern(raw map[string]interface{}) *ToolCall {
	for _, key := range []string{"file_path", "file_name", "filename", "path", "file"} {
		fp, ok := raw[key].(string)
		if !ok || fp == "" {
			continue
		}
		// Check if this looks like a read operation (no content, or has "read" action)
		if _, hasContent := raw["content"]; !hasContent {
			if action, ok := raw["action"].(string); ok {
				if action == "read" || action == "open" || action == "get" {
					return &ToolCall{
						Tool:   "read_file",
						Params: map[string]interface{}{"file_path": fp},
					}
				}
			}
		}
		break
	}
	return nil
}

// detectCommandExecutionPattern detects the command execution pattern
func detectCommandExecutionPattern(raw map[string]interface{}) *ToolCall {
	for _, key := range []string{"command", "cmd", "shell", "exec"} {
		if cmd, ok := raw[key].(string); ok && cmd != "" {
			return &ToolCall{
				Tool:   "execute_command",
				Params: map[string]interface{}{"command": cmd},
			}
		}
	}
	return nil
}

// extractJSONObjects finds JSON objects using brace matching
// If strictToolFormat is true, only finds objects starting with {"tool"
// If false, finds any JSON object (for fallback conversion)
func extractJSONObjects(text string) []string {
	return extractJSONObjectsWithPattern(text, `\{\s*"tool"\s*:`)
}

// extractAllJSONObjects finds any JSON object in text (for fallback parsing)
func extractAllJSONObjects(text string) []string {
	return extractJSONObjectsWithPattern(text, `\{`)
}

func extractJSONObjectsWithPattern(text string, pattern string) []string {
	var results []string

	// Find all positions where a JSON object might start
	re := regexp.MustCompile(pattern)
	indices := re.FindAllStringIndex(text, -1)

	for _, idx := range indices {
		start := idx[0]
		depth := 0
		inString := false
		escaped := false
		end := -1

		for i := start; i < len(text); i++ {
			c := text[i]

			if escaped {
				escaped = false
				continue
			}

			if c == '\\' && inString {
				escaped = true
				continue
			}

			if c == '"' {
				inString = !inString
				continue
			}

			if !inString {
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						end = i + 1
						break
					}
				}
			}
		}

		if end > start {
			jsonStr := text[start:end]
			results = append(results, jsonStr)
		}
	}

	return results
}

// parseToolCalls extracts ALL tool calls from the model's response (supports multiple tools).
// It runs each extraction phase in turn (legacy JSON fallback parser, removed in 3.0),
// sharing a dedup set, the accumulated tool calls, and the position of the first match.
func parseToolCalls(response string) ([]*ToolCall, string) {
	cleaned := cleanResponse(response)
	var toolCalls []*ToolCall
	seen := make(map[string]bool) // Track seen tool calls to avoid duplicates
	firstToolIdx := -1

	extractStandardToolCalls(cleaned, seen, &toolCalls, &firstToolIdx)
	extractArrayToolCalls(cleaned, seen, &toolCalls, &firstToolIdx)

	// Fallback: if no standard tool calls found, try to convert alternative JSON formats
	if len(toolCalls) == 0 {
		extractFallbackToolCalls(cleaned, seen, &toolCalls, &firstToolIdx)
	}

	// Extract text before first tool call for display
	textBefore := cleaned
	if len(toolCalls) > 0 && firstToolIdx > 0 {
		textBefore = strings.TrimSpace(cleaned[:firstToolIdx])
	} else if len(toolCalls) > 0 {
		textBefore = ""
	}

	return toolCalls, textBefore
}

// extractStandardToolCalls finds JSON objects using brace matching (handles nested objects and
// multiline) and unmarshals each into a ToolCall, appending new (non-duplicate) matches.
func extractStandardToolCalls(cleaned string, seen map[string]bool, toolCalls *[]*ToolCall, firstToolIdx *int) {
	jsonObjects := extractJSONObjects(cleaned)

	for _, jsonStr := range jsonObjects {
		// Normalize the JSON to fix text-wrapping artifacts
		normalized := normalizeJSON(jsonStr)

		// Try to unmarshal
		var toolCall ToolCall
		if err := json.Unmarshal([]byte(normalized), &toolCall); err == nil {
			if toolCall.Tool != "" {
				// Create a key to track duplicates
				key := toolCall.Tool + ":" + fmt.Sprintf("%v", toolCall.Params)
				if !seen[key] {
					seen[key] = true
					*toolCalls = append(*toolCalls, &toolCall)

					// Track position of first tool call
					if *firstToolIdx == -1 {
						*firstToolIdx = strings.Index(cleaned, jsonStr)
					}
				}
			}
		}
	}
}

// extractArrayToolCalls looks for a JSON array of tool calls -
// [{"tool": "...", "params": {...}}, {"tool": "...", "params": {...}}] - and appends any new
// (non-duplicate) matches.
func extractArrayToolCalls(cleaned string, seen map[string]bool, toolCalls *[]*ToolCall, firstToolIdx *int) {
	arrayPattern := regexp.MustCompile(`\[\s*\{`)
	arrayIdx := arrayPattern.FindStringIndex(cleaned)
	if arrayIdx == nil {
		return
	}
	// Find matching closing bracket
	start := arrayIdx[0]
	depth := 0
	inString := false
	escaped := false
	end := -1

	for i := start; i < len(cleaned); i++ {
		c := cleaned[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if !inString {
			if c == '[' {
				depth++
			} else if c == ']' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
	}

	if end > start {
		arrayStr := normalizeJSON(cleaned[start:end])
		var arrayToolCalls []ToolCall
		if err := json.Unmarshal([]byte(arrayStr), &arrayToolCalls); err == nil {
			for i := range arrayToolCalls {
				if arrayToolCalls[i].Tool != "" {
					key := arrayToolCalls[i].Tool + ":" + fmt.Sprintf("%v", arrayToolCalls[i].Params)
					if !seen[key] {
						seen[key] = true
						*toolCalls = append(*toolCalls, &arrayToolCalls[i])
					}
				}
			}
			if *firstToolIdx == -1 || start < *firstToolIdx {
				*firstToolIdx = start
			}
		}
	}
}

// extractFallbackToolCalls tries to convert alternative (non-standard) JSON formats found
// anywhere in the text into tool calls, appending any new (non-duplicate) matches.
func extractFallbackToolCalls(cleaned string, seen map[string]bool, toolCalls *[]*ToolCall, firstToolIdx *int) {
	allJSONObjects := extractAllJSONObjects(cleaned)
	for _, jsonStr := range allJSONObjects {
		normalized := normalizeJSON(jsonStr)
		if converted := tryConvertToToolCall(normalized); converted != nil {
			key := converted.Tool + ":" + fmt.Sprintf("%v", converted.Params)
			if !seen[key] {
				seen[key] = true
				*toolCalls = append(*toolCalls, converted)
				if *firstToolIdx == -1 {
					*firstToolIdx = strings.Index(cleaned, jsonStr)
				}
			}
		}
	}
}
