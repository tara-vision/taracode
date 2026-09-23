package agent

import (
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
