package assistant

import (
	gocontext "context"
	"fmt"
	"time"

	"github.com/tara-vision/taracode/internal/llm"
)

// GetContextInfo returns detailed context budget information (v2.0.2)
func (a *Assistant) GetContextInfo() ContextInfo {
	systemTokens := 0
	conversationTokens := 0
	for i, msg := range a.conversation {
		tokens := EstimateTokens(msg.Content)
		if i == 0 {
			systemTokens = tokens
		} else {
			conversationTokens += tokens
		}
	}
	toolDefTokens := EstimateToolDefsTokens(a.toolDefs)
	totalTokens := systemTokens + conversationTokens + toolDefTokens
	maxTokens := a.compactionCfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 32768
	}

	return ContextInfo{
		SystemPromptTokens:  systemTokens,
		ToolDefsTokens:      toolDefTokens,
		ConversationTokens:  conversationTokens,
		TotalTokens:         totalTokens,
		MaxTokens:           maxTokens,
		MessageCount:        len(a.conversation),
		CompactionEvents:    a.compactionState.Events,
		TruncationEvents:    a.truncationEvents,
		CompactionEnabled:   a.compactionCfg.Enabled,
		CompactionThreshold: a.compactionCfg.Threshold,
		MaxIterations:       a.maxIterations,
		ServerContextTokens: a.serverContextTokens,
		ContextWindow:       a.contextWindow,
	}
}

// ContextInfo holds detailed context budget information for display
type ContextInfo struct {
	SystemPromptTokens  int
	ToolDefsTokens      int
	ConversationTokens  int
	TotalTokens         int
	MaxTokens           int
	MessageCount        int
	CompactionEvents    []CompactionEvent
	TruncationEvents    []TruncationResult
	CompactionEnabled   bool
	CompactionThreshold float64
	MaxIterations       int
	ServerContextTokens int // context window reported by the Ollama server (0 = unknown)
	ContextWindow       int // num_ctx requested for the current model (0 = unresolved)
}

// ForceCompact triggers immediate conversation compaction regardless of threshold
func (a *Assistant) ForceCompact() error {
	if len(a.conversation) < a.compactionCfg.KeepRecent*2+3 {
		return fmt.Errorf("conversation too short to compact (%d messages, need at least %d)",
			len(a.conversation), a.compactionCfg.KeepRecent*2+3)
	}

	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 60*time.Second)
	defer cancel()

	// Force compaction with a very low threshold
	forceCfg := a.compactionCfg
	forceCfg.Enabled = true
	forceCfg.Threshold = 0.0 // Always trigger

	// The summary request carries the session's own options (num_ctx, keep_alive) with thinking
	// off and a small token budget, the same as the automatic path in loop.go.
	options := a.requestOptions()
	options.NumPredict = compactionSummaryTokens
	options.Think = llm.ThinkOff
	compacted, event, err := CompactConversation(ctx, a.conversation, a.toolDefs, forceCfg, a.llm, a.model, options)
	if err != nil {
		return fmt.Errorf("compaction failed: %w", err)
	}
	if event != nil {
		a.conversation = compacted
		a.compactionState.Events = append(a.compactionState.Events, *event)
		a.compactionState.TotalCompacted += event.MessagesBefore - event.MessagesAfter
	}
	return nil
}
