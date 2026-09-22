// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/history"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
)

// cmdPlan is the /plan command: show the active plan.
func (r *repl) cmdPlan(_ []string) {
	handleShowPlan(r.asst)
}

// cmdContext is the /context command: context window budget breakdown.
func (r *repl) cmdContext(_ []string) {
	handleContext(r.asst, r.memory)
}

// cmdCompact is the /compact command: force conversation compaction.
func (r *repl) cmdCompact(_ []string) {
	handleCompact(r.asst)
}

// cmdStats is the /stats command: session statistics.
func (r *repl) cmdStats(_ []string) {
	handleStats(r.asst, r.history)
}

// cmdUsage is the /usage command: token usage for this session.
func (r *repl) cmdUsage(_ []string) {
	usage := r.asst.GetSessionUsage()
	fmt.Println("Session Usage:")
	fmt.Printf("  Prompt tokens:     %d\n", usage.PromptTokens)
	fmt.Printf("  Completion tokens: %d\n", usage.CompletionTokens)
	fmt.Printf("  Total tokens:      %d\n", usage.TotalTokens)
	fmt.Println()
}

// handleContext displays what's in the LLM context window
// boxWidth is the content width for /context and /history boxes (excluding borders)
const boxWidth = 67

// formatBoxLine formats a line to fit within the box, with proper padding
func formatBoxLine(content string) string {
	if len(content) > boxWidth {
		content = content[:boxWidth-3] + "..."
	}
	return fmt.Sprintf("│  %-*s│", boxWidth, content)
}

func handleContext(asst *assistant.Assistant, mm *memory.Manager) {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Context Window                                                     │")
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")

	ctxInfo := asst.GetContextInfo()
	printContextBudget(ctxInfo)
	printContextCompactionHistory(ctxInfo)
	printContextTruncationEvents(ctxInfo)

	session := asst.GetSession()
	printContextSessionInfo(session)
	printContextTokenUsage(asst)
	printContextProjectInfo(asst)
	printContextMemories(mm)
	printContextFilesRead(session)
	printContextModeSettings(asst, ctxInfo)

	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// printContextBudget prints the context budget breakdown (v2.0.2): total, system prompt, tool
// definitions, server/requested context window, conversation and available tokens.
func printContextBudget(ctxInfo assistant.ContextInfo) {
	usedPct := 0
	if ctxInfo.MaxTokens > 0 {
		usedPct = ctxInfo.TotalTokens * 100 / ctxInfo.MaxTokens
	}
	availableTokens := ctxInfo.MaxTokens - ctxInfo.TotalTokens
	if availableTokens < 0 {
		availableTokens = 0
	}

	budgetLine := fmt.Sprintf("Context Budget: %.1fk / %.1fk tokens (%d%%)",
		float64(ctxInfo.TotalTokens)/1000.0,
		float64(ctxInfo.MaxTokens)/1000.0,
		usedPct)
	fmt.Println(formatBoxLine(budgetLine))
	fmt.Println(formatBoxLine(""))
	fmt.Println(formatBoxLine(fmt.Sprintf("  System prompt:    %.1fk tokens", float64(ctxInfo.SystemPromptTokens)/1000.0)))
	fmt.Println(formatBoxLine(fmt.Sprintf("  Tool definitions: %.1fk tokens", float64(ctxInfo.ToolDefsTokens)/1000.0)))
	if ctxInfo.ServerContextTokens > 0 {
		fmt.Println(formatBoxLine(fmt.Sprintf(
			"  Server context:   %.1fk tokens (Ollama num_ctx)", float64(ctxInfo.ServerContextTokens)/1000.0)))
	}
	if ctxInfo.ContextWindow > 0 {
		fmt.Println(formatBoxLine(fmt.Sprintf(
			"  Context window (requested): %.1fk tokens", float64(ctxInfo.ContextWindow)/1000.0)))
	}
	compactionNote := ""
	if len(ctxInfo.CompactionEvents) > 0 {
		compactionNote = fmt.Sprintf(", %d compactions", len(ctxInfo.CompactionEvents))
	}
	fmt.Println(formatBoxLine(fmt.Sprintf("  Conversation:     %.1fk tokens (%d messages%s)",
		float64(ctxInfo.ConversationTokens)/1000.0,
		ctxInfo.MessageCount,
		compactionNote)))
	fmt.Println(formatBoxLine(fmt.Sprintf("  Available:        %.1fk tokens", float64(availableTokens)/1000.0)))
}

// printContextCompactionHistory prints the compaction history section, when there is one.
func printContextCompactionHistory(ctxInfo assistant.ContextInfo) {
	if len(ctxInfo.CompactionEvents) == 0 {
		return
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine("Compaction History"))
	for i, evt := range ctxInfo.CompactionEvents {
		fmt.Println(formatBoxLine(fmt.Sprintf("  #%d: %.1fk -> %.1fk (%d messages summarized)",
			i+1,
			float64(evt.TokensBefore)/1000.0,
			float64(evt.TokensAfter)/1000.0,
			evt.MessagesBefore-evt.MessagesAfter)))
	}
}

// printContextTruncationEvents prints the truncated-output section, when there is one.
func printContextTruncationEvents(ctxInfo assistant.ContextInfo) {
	if len(ctxInfo.TruncationEvents) == 0 {
		return
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine(fmt.Sprintf("Truncated Outputs: %d", len(ctxInfo.TruncationEvents))))
	showCount := len(ctxInfo.TruncationEvents)
	if showCount > 5 {
		showCount = 5
	}
	for i := 0; i < showCount; i++ {
		evt := ctxInfo.TruncationEvents[i]
		fmt.Println(formatBoxLine(fmt.Sprintf("  %s: %d -> %d lines", evt.ToolName, evt.OrigLines, evt.KeptLines)))
	}
	if len(ctxInfo.TruncationEvents) > 5 {
		fmt.Println(formatBoxLine(fmt.Sprintf("  ... and %d more", len(ctxInfo.TruncationEvents)-5)))
	}
}

// printContextSessionInfo prints the session message/tool-call counts, when there is a session.
func printContextSessionInfo(session *storage.Session) {
	if session == nil {
		return
	}
	userMsgs := 0
	assistMsgs := 0
	toolCalls := 0
	for _, msg := range session.Messages {
		switch msg.Role {
		case "user":
			userMsgs++
		case "assistant":
			assistMsgs++
		}
		toolCalls += len(msg.ToolCalls)
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine(fmt.Sprintf("Session: %s", ui.TruncateID(session.ID, 0))))
	fmt.Println(formatBoxLine(fmt.Sprintf("  Messages: %d user, %d assistant", userMsgs, assistMsgs)))
	if toolCalls > 0 {
		fmt.Println(formatBoxLine(fmt.Sprintf("  Tool calls: %d", toolCalls)))
	}
}

// printContextTokenUsage prints the LLM token usage line, when there is any.
func printContextTokenUsage(asst *assistant.Assistant) {
	usage := asst.GetSessionUsage()
	if usage == nil || usage.TotalTokens == 0 {
		return
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine(fmt.Sprintf("LLM Tokens: %d (prompt: %d, completion: %d)",
		usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)))
}

// printContextProjectInfo prints the TARACODE.md-derived project context, when there is one.
func printContextProjectInfo(asst *assistant.Assistant) {
	projectCtx := asst.GetProjectContext()
	if projectCtx == nil {
		return
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine("Project Context (from TARACODE.md)"))
	fmt.Println(formatBoxLine(fmt.Sprintf("  Type: %s", projectCtx.ProjectType)))
	if projectCtx.ModuleName != "" {
		moduleName := projectCtx.ModuleName
		if len(moduleName) > 50 {
			moduleName = moduleName[:47] + "..."
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("  Module: %s", moduleName)))
	}
	if len(projectCtx.ImportantFiles) > 0 {
		fmt.Println(formatBoxLine(fmt.Sprintf("  Important files: %d", len(projectCtx.ImportantFiles))))
	}
	if len(projectCtx.DetectedTools) > 0 {
		toolList := strings.Join(projectCtx.DetectedTools, ", ")
		if len(toolList) > 45 {
			toolList = toolList[:42] + "..."
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("  Detected tools: %s", toolList)))
	}
}

// printContextMemories prints the project memories in context, when there are any.
func printContextMemories(mm *memory.Manager) {
	if mm == nil {
		return
	}
	memCount := mm.Count()
	if memCount == 0 {
		return
	}
	maxTokens := viper.GetInt("memory.max_context_tokens")
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	relevantMems := mm.GetRelevantMemories("", maxTokens)
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine(fmt.Sprintf("Project Memories: %d total, %d in context", memCount, len(relevantMems))))
	if len(relevantMems) == 0 {
		return
	}
	showCount := len(relevantMems)
	if showCount > 3 {
		showCount = 3
	}
	for i := 0; i < showCount; i++ {
		mem := relevantMems[i]
		preview := mem.Content
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("  [%s] %s", mem.Category, preview)))
	}
	if len(relevantMems) > 3 {
		fmt.Println(formatBoxLine(fmt.Sprintf("  ... and %d more", len(relevantMems)-3)))
	}
}

// printContextFilesRead prints the unique files read this session, when there are any.
func printContextFilesRead(session *storage.Session) {
	if session == nil || len(session.Messages) == 0 {
		return
	}
	filesRead := extractFilesRead(session.Messages)
	if len(filesRead) == 0 {
		return
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	fmt.Println(formatBoxLine("Files Read This Session"))
	displayCount := len(filesRead)
	if displayCount > 10 {
		displayCount = 10
	}
	for i := 0; i < displayCount; i++ {
		file := filesRead[i]
		if len(file) > 60 {
			file = "..." + file[len(file)-57:]
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("  %s", file)))
	}
	if len(filesRead) > 10 {
		fmt.Println(formatBoxLine(fmt.Sprintf("  ... and %d more", len(filesRead)-10)))
	}
}

// printContextModeSettings prints the operating mode, compaction and iteration settings.
func printContextModeSettings(asst *assistant.Assistant, ctxInfo assistant.ContextInfo) {
	mode := asst.GetMode()
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	if mode == storage.ModeSecurity {
		fmt.Println(formatBoxLine(fmt.Sprintf("Mode: %s security (audit-first)", ui.IconShield)))
	} else {
		fmt.Println(formatBoxLine(fmt.Sprintf("Mode: %s (%d DevOps tools)", mode, tools.GetToolCount())))
	}
	compactionStatus := "disabled"
	if ctxInfo.CompactionEnabled {
		compactionStatus = fmt.Sprintf("enabled (threshold: %.0f%%)", ctxInfo.CompactionThreshold*100)
	}
	fmt.Println(formatBoxLine(fmt.Sprintf("Compaction: %s", compactionStatus)))
	fmt.Println(formatBoxLine(fmt.Sprintf("Max iterations: %d", ctxInfo.MaxIterations)))
}

// handleCompact forces immediate conversation compaction (v2.0.2)
func handleCompact(asst *assistant.Assistant) {
	fmt.Println()
	ctxInfo := asst.GetContextInfo()
	fmt.Printf("Current context: %.1fk / %.1fk tokens (%d messages)\n",
		float64(ctxInfo.TotalTokens)/1000.0,
		float64(ctxInfo.MaxTokens)/1000.0,
		ctxInfo.MessageCount)

	if !ctxInfo.CompactionEnabled {
		fmt.Println("Note: Auto-compaction is disabled. Forcing manual compaction.")
	}

	err := asst.ForceCompact()
	if err != nil {
		fmt.Printf("Compaction failed: %v\n", err)
		fmt.Println()
		return
	}

	newInfo := asst.GetContextInfo()
	fmt.Printf("Compacted: %.1fk -> %.1fk tokens (%d -> %d messages)\n",
		float64(ctxInfo.TotalTokens)/1000.0,
		float64(newInfo.TotalTokens)/1000.0,
		ctxInfo.MessageCount,
		newInfo.MessageCount)
	fmt.Println()
}

// handleStats shows session statistics (v2.0.2)
func handleStats(asst *assistant.Assistant, hm *history.Manager) {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│  Session Statistics                                                 │")
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")

	// Context budget
	ctxInfo := asst.GetContextInfo()
	usedPct := 0
	if ctxInfo.MaxTokens > 0 {
		usedPct = ctxInfo.TotalTokens * 100 / ctxInfo.MaxTokens
	}
	fmt.Println(formatBoxLine(fmt.Sprintf("Context: %.1fk / %.1fk tokens (%d%%)",
		float64(ctxInfo.TotalTokens)/1000.0,
		float64(ctxInfo.MaxTokens)/1000.0,
		usedPct)))
	fmt.Println(formatBoxLine(fmt.Sprintf("Messages: %d", ctxInfo.MessageCount)))

	// LLM token usage
	usage := asst.GetSessionUsage()
	if usage != nil && usage.TotalTokens > 0 {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine(fmt.Sprintf("LLM Tokens: %d total", usage.TotalTokens)))
		fmt.Println(formatBoxLine(fmt.Sprintf("  Prompt: %d  Completion: %d", usage.PromptTokens, usage.CompletionTokens)))
	}

	// Compaction events
	if len(ctxInfo.CompactionEvents) > 0 {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine(fmt.Sprintf("Compactions: %d", len(ctxInfo.CompactionEvents))))
		for _, evt := range ctxInfo.CompactionEvents {
			fmt.Println(formatBoxLine(fmt.Sprintf("  %.1fk -> %.1fk (%d msgs removed)",
				float64(evt.TokensBefore)/1000.0,
				float64(evt.TokensAfter)/1000.0,
				evt.MessagesBefore-evt.MessagesAfter)))
		}
	}

	// Truncation events
	if len(ctxInfo.TruncationEvents) > 0 {
		fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
		fmt.Println(formatBoxLine(fmt.Sprintf("Truncated outputs: %d", len(ctxInfo.TruncationEvents))))
		totalSaved := 0
		for _, evt := range ctxInfo.TruncationEvents {
			totalSaved += evt.OrigChars - evt.KeptChars
		}
		fmt.Println(formatBoxLine(fmt.Sprintf("  Chars saved: %d", totalSaved)))
	}

	// File operations
	if hm != nil {
		ops := hm.GetAllHistory()
		if len(ops) > 0 {
			fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
			fmt.Println(formatBoxLine(fmt.Sprintf("File operations: %d", len(ops))))
			opTypes := make(map[history.OperationType]int)
			for _, op := range ops {
				opTypes[op.Type]++
			}
			for opType, count := range opTypes {
				fmt.Println(formatBoxLine(fmt.Sprintf("  %s: %d", string(opType), count)))
			}
		}
	}

	// Settings
	fmt.Println("├─────────────────────────────────────────────────────────────────────┤")
	compactionStatus := "disabled"
	if ctxInfo.CompactionEnabled {
		compactionStatus = fmt.Sprintf("enabled (%.0f%% threshold)", ctxInfo.CompactionThreshold*100)
	}
	fmt.Println(formatBoxLine(fmt.Sprintf("Compaction: %s", compactionStatus)))
	fmt.Println(formatBoxLine(fmt.Sprintf("Max iterations: %d per message", ctxInfo.MaxIterations)))
	// Model generation options
	temp := viper.GetFloat64("model.temperature")
	topP := viper.GetFloat64("model.top_p")
	numPredict := viper.GetInt("model.num_predict")
	numPredictStr := "model default"
	if numPredict > 0 {
		numPredictStr = fmt.Sprintf("%d", numPredict)
	}
	fmt.Println(formatBoxLine(fmt.Sprintf(
		"Model options: temp=%.1f top_p=%.1f num_predict=%s", temp, topP, numPredictStr)))

	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// extractFilesRead extracts unique file paths from read_file tool calls
func extractFilesRead(messages []storage.ConversationMessage) []string {
	seen := make(map[string]bool)
	var files []string

	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.Tool == "read_file" {
				if filePath, ok := tc.Params["file_path"].(string); ok && filePath != "" {
					if !seen[filePath] {
						seen[filePath] = true
						files = append(files, filePath)
					}
				}
			}
		}
	}

	return files
}

// handleShowPlan displays the active task plan
func handleShowPlan(asst *assistant.Assistant) {
	storage := asst.GetStorage()
	if storage == nil {
		fmt.Println("Storage not initialized. Run /init first.")
		fmt.Println()
		return
	}

	plan, err := storage.GetActivePlan()
	if err != nil || plan == nil {
		fmt.Println("No active plan.")
		fmt.Println()
		return
	}

	fmt.Printf("Plan: %s\n", plan.Title)
	fmt.Printf("Status: %s\n", plan.Status)
	fmt.Println()

	for i, task := range plan.Tasks {
		status := "[ ]"
		switch task.Status {
		case "completed":
			status = "[x]"
		case "in_progress":
			status = "[>]"
		case "skipped":
			status = "[-]"
		}
		fmt.Printf("  %d. %s %s\n", i+1, status, task.Content)
	}
	fmt.Println()
}
