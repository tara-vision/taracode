// Package cmd implements taracode's CLI commands and interactive REPL.
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/memory"
	"github.com/tara-vision/taracode/internal/storage"
)

// cmdRemember is the /remember command: save a memory about this project.
func (r *repl) cmdRemember(args []string) {
	handleRemember(r.memory, args, &r.asst)
}

// cmdMemory is the /memory command: project memories.
func (r *repl) cmdMemory(args []string) {
	handleMemory(r.memory, args)
}

// handleRemember saves a new memory about the project
func handleRemember(mm *memory.Manager, args []string, asst **assistant.Assistant) {
	if mm == nil {
		fmt.Println("Memory not available.")
		fmt.Println("Initialize the project with /init first.")
		fmt.Println()
		return
	}

	if len(args) == 0 {
		fmt.Println("Usage: /remember <text to remember>")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  /remember Use snake_case for all variable names")
		fmt.Println("  /remember Database is PostgreSQL on port 5432")
		fmt.Println("  /remember Always run tests before committing")
		fmt.Println()
		return
	}

	content := strings.Join(args, " ")

	// Detect category from content
	category := detectMemoryCategory(content)

	// Extract tags from content (words starting with #)
	var tags []string
	words := strings.Fields(content)
	cleanedWords := make([]string, 0, len(words))
	for _, word := range words {
		if strings.HasPrefix(word, "#") && len(word) > 1 {
			tags = append(tags, strings.TrimPrefix(word, "#"))
		} else {
			cleanedWords = append(cleanedWords, word)
		}
	}
	content = strings.Join(cleanedWords, " ")

	mem, err := mm.Create(category, content, "", tags, storage.MemorySourceManual)
	if err != nil {
		fmt.Printf("Error saving memory: %v\n", err)
		fmt.Println()
		return
	}

	categoryIcon := getCategoryIcon(category)
	fmt.Printf("%s Saved [%s] (ID: %s)\n", categoryIcon, category, mem.ID)
	if len(tags) > 0 {
		fmt.Printf("   Tags: %s\n", strings.Join(tags, ", "))
	}
	fmt.Println()

	// Refresh system prompt to include the new memory
	if asst != nil && *asst != nil {
		(*asst).RefreshSystemPrompt()
	}
}

// detectMemoryCategory determines the category based on content keywords
func detectMemoryCategory(content string) storage.MemoryCategory {
	lower := strings.ToLower(content)

	// Decision indicators
	decisionKeywords := []string{
		"decided", "decision", "chose", "chosen", "will use", "using", "selected", "architecture", "design",
	}
	for _, kw := range decisionKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryDecision
		}
	}

	// Pattern indicators
	patternKeywords := []string{
		"always", "never", "convention", "style", "naming", "format", "prefer",
		"use case", "camelcase", "snake_case", "pattern",
	}
	for _, kw := range patternKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryPattern
		}
	}

	// Error indicators
	errorKeywords := []string{
		"error", "bug", "fix", "issue", "problem", "crash", "fail", "exception", "timeout", "solution",
	}
	for _, kw := range errorKeywords {
		if strings.Contains(lower, kw) {
			return storage.MemoryCategoryError
		}
	}

	// Default to learning
	return storage.MemoryCategoryLearning
}

// getCategoryIcon returns an icon for the memory category
func getCategoryIcon(cat storage.MemoryCategory) string {
	switch cat {
	case storage.MemoryCategoryDecision:
		return "🏛️"
	case storage.MemoryCategoryPattern:
		return "📐"
	case storage.MemoryCategoryError:
		return "🐛"
	case storage.MemoryCategoryLearning:
		return "💡"
	default:
		return "📝"
	}
}

// handleMemory handles the /memory command and its subcommands
func handleMemory(mm *memory.Manager, args []string) {
	if mm == nil {
		fmt.Println("Memory not available.")
		fmt.Println("Initialize the project with /init first.")
		fmt.Println()
		return
	}

	// No args - list all memories
	if len(args) == 0 {
		listMemories(mm)
		return
	}

	switch args[0] {
	case "search":
		if len(args) < 2 {
			fmt.Println("Usage: /memory search <query>")
			fmt.Println()
			return
		}
		query := strings.Join(args[1:], " ")
		searchMemories(mm, query)

	case "delete":
		if len(args) < 2 {
			fmt.Println("Usage: /memory delete <id>")
			fmt.Println()
			return
		}
		deleteMemory(mm, args[1])

	case "export":
		filename := ""
		if len(args) > 1 {
			filename = args[1]
		}
		exportMemories(mm, filename)

	case "import":
		if len(args) < 2 {
			fmt.Println("Usage: /memory import <file>")
			fmt.Println()
			return
		}
		importMemories(mm, args[1])

	case "stats":
		showMemoryStats(mm)

	case "cleanup":
		days := 90 // Default retention
		if len(args) > 1 {
			if d, err := strconv.Atoi(args[1]); err == nil && d > 0 {
				days = d
			}
		}
		cleanupMemories(mm, days)

	case "clear":
		clearMemories(mm)

	default:
		fmt.Printf("Unknown subcommand: %s\n", args[0])
		fmt.Println("Usage: /memory [search|delete|export|import|stats|cleanup|clear]")
		fmt.Println()
	}
}

// listMemories displays all project memories
func listMemories(mm *memory.Manager) {
	memories := mm.List()

	if len(memories) == 0 {
		fmt.Println("No memories saved for this project.")
		fmt.Println("Use /remember <text> to save project knowledge.")
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("Project Memories (%d total):\n", len(memories))
	fmt.Println(strings.Repeat("─", 60))

	for _, meta := range memories {
		icon := getCategoryIcon(meta.Category)
		age := formatAge(meta.CreatedAt)
		useInfo := ""
		if meta.UseCount > 0 {
			useInfo = fmt.Sprintf(" (used %dx)", meta.UseCount)
		}
		fmt.Printf("\n%s [%s] %s%s\n", icon, meta.ID, string(meta.Category), useInfo)
		fmt.Printf("   %s\n", meta.Preview)
		fmt.Printf("   \033[90m%s\033[0m\n", age)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("Commands: /memory search <query> | /memory delete <id>")
	fmt.Println()
}

// searchMemories searches for memories matching a query
func searchMemories(mm *memory.Manager, query string) {
	results, err := mm.Search(query, nil)
	if err != nil {
		fmt.Printf("Error searching: %v\n", err)
		fmt.Println()
		return
	}

	if len(results) == 0 {
		fmt.Printf("No memories found matching: %s\n", query)
		fmt.Println()
		return
	}

	fmt.Println()
	fmt.Printf("Search Results for \"%s\" (%d matches):\n", query, len(results))
	fmt.Println(strings.Repeat("─", 60))

	for _, meta := range results {
		icon := getCategoryIcon(meta.Category)
		fmt.Printf("\n%s [%s] %s\n", icon, meta.ID, string(meta.Category))
		fmt.Printf("   %s\n", meta.Preview)
	}

	fmt.Println()
}

// deleteMemory removes a memory by ID
func deleteMemory(mm *memory.Manager, id string) {
	// Show the memory first
	mem, err := mm.Get(id)
	if err != nil {
		fmt.Printf("Memory not found: %s\n", id)
		fmt.Println()
		return
	}

	fmt.Printf("Delete memory [%s]?\n", mem.ID)
	fmt.Printf("  %s\n", mem.Content)
	fmt.Print("Confirm (y/N): ")

	var confirm string
	_, _ = fmt.Scanln(&confirm)

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("Cancelled.")
		fmt.Println()
		return
	}

	if err := mm.Delete(id); err != nil {
		fmt.Printf("Error deleting: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Println("Memory deleted.")
	fmt.Println()
}

// exportMemories exports all memories to a JSON file
func exportMemories(mm *memory.Manager, filename string) {
	data, err := mm.ExportJSON()
	if err != nil {
		fmt.Printf("Error exporting: %v\n", err)
		fmt.Println()
		return
	}

	if filename == "" {
		timestamp := time.Now().Format("20060102-150405")
		filename = fmt.Sprintf("memories-%s.json", timestamp)
	}

	//nolint:gosec // /memory export writes to a filename the user chose (or a generated default)
	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Exported %d memories to: %s\n", mm.Count(), filename)
	fmt.Println()
}

// importMemories imports memories from a JSON file
func importMemories(mm *memory.Manager, filename string) {
	//nolint:gosec // /memory import reads a filename the user explicitly passed on the command line
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		fmt.Println()
		return
	}

	imported, err := mm.ImportJSON(data)
	if err != nil {
		fmt.Printf("Error importing: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Imported %d memories from: %s\n", imported, filename)
	fmt.Println()
}

// showMemoryStats displays memory statistics
func showMemoryStats(mm *memory.Manager) {
	stats := mm.GetStats()

	fmt.Println()
	fmt.Println("Memory Statistics")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("Total memories:     %d\n", stats.TotalMemories)
	fmt.Printf("Total context uses: %d\n", stats.TotalUseCount)
	fmt.Printf("Unused memories:    %d\n", stats.UnusedCount)
	fmt.Printf("Est. tokens:        ~%d\n", stats.EstimatedTokens)
	fmt.Println()

	if len(stats.ByCategory) > 0 {
		fmt.Println("By Category:")
		for cat, count := range stats.ByCategory {
			icon := getCategoryIcon(storage.MemoryCategory(cat))
			fmt.Printf("  %s %-12s %d\n", icon, cat, count)
		}
		fmt.Println()
	}

	if len(stats.BySource) > 0 {
		fmt.Println("By Source:")
		for src, count := range stats.BySource {
			fmt.Printf("  %-12s %d\n", src, count)
		}
		fmt.Println()
	}

	if stats.MostUsedID != "" {
		fmt.Printf("Most Used: [%s] (%dx)\n", stats.MostUsedID, stats.MostUsedCount)
		if len(stats.MostUsedContent) > 50 {
			fmt.Printf("  %s...\n", stats.MostUsedContent[:50])
		} else {
			fmt.Printf("  %s\n", stats.MostUsedContent)
		}
		fmt.Println()
	}
}

// cleanupMemories removes old unused memories
func cleanupMemories(mm *memory.Manager, days int) {
	deleted, err := mm.Cleanup(days)
	if err != nil {
		fmt.Printf("Error during cleanup: %v\n", err)
		fmt.Println()
		return
	}

	if deleted == 0 {
		fmt.Printf("No memories older than %d days to clean up.\n", days)
	} else {
		fmt.Printf("Removed %d memories not used in %d days.\n", deleted, days)
	}
	fmt.Println()
}

// clearMemories removes all project memories
func clearMemories(mm *memory.Manager) {
	count := mm.Count()
	if count == 0 {
		fmt.Println("No memories to clear.")
		fmt.Println()
		return
	}

	fmt.Printf("Clear all %d memories? This cannot be undone.\n", count)
	fmt.Print("Type 'yes' to confirm: ")

	var confirm string
	_, _ = fmt.Scanln(&confirm)

	if strings.ToLower(strings.TrimSpace(confirm)) != "yes" {
		fmt.Println("Cancelled.")
		fmt.Println()
		return
	}

	if err := mm.Clear(); err != nil {
		fmt.Printf("Error clearing memories: %v\n", err)
		fmt.Println()
		return
	}

	fmt.Printf("Cleared %d memories.\n", count)
	fmt.Println()
}

// formatAge returns a human-readable age string
func formatAge(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Hour {
		mins := int(duration.Minutes())
		if mins <= 1 {
			return "just now"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}

	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}

	days := int(duration.Hours() / 24)
	if days == 1 {
		return "yesterday"
	}
	if days < 30 {
		return fmt.Sprintf("%d days ago", days)
	}

	months := days / 30
	if months == 1 {
		return "1 month ago"
	}
	return fmt.Sprintf("%d months ago", months)
}

// checkAutoCapture checks if user message contains patterns that suggest memory-worthy content
func checkAutoCapture(mm *memory.Manager, userMessage string) {
	lower := strings.ToLower(userMessage)

	// Skip short messages
	if len(userMessage) < 20 {
		return
	}

	// Skip questions
	if strings.HasSuffix(strings.TrimSpace(userMessage), "?") {
		return
	}

	var suggestion string
	var category storage.MemoryCategory

	// Correction patterns: user is correcting AI's understanding
	correctionPatterns := []string{
		"no,", "that's wrong", "actually,", "i meant", "not like that",
		"don't do", "never do", "stop doing", "instead of", "wrong approach",
	}
	for _, pattern := range correctionPatterns {
		if strings.Contains(lower, pattern) {
			suggestion = extractMemorySuggestion(userMessage)
			category = storage.MemoryCategoryPattern
			break
		}
	}

	// Convention patterns: user is stating a convention
	if suggestion == "" {
		conventionPatterns := []string{
			"always use", "we always", "never use", "we never", "our convention",
			"in this project", "our style", "our pattern", "we prefer", "standard is",
		}
		for _, pattern := range conventionPatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryPattern
				break
			}
		}
	}

	// Knowledge patterns: user is sharing important info
	if suggestion == "" {
		knowledgePatterns := []string{
			"remember that", "note that", "important:", "keep in mind",
			"database is", "server is", "port is", "api key", "config",
		}
		for _, pattern := range knowledgePatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryLearning
				break
			}
		}
	}

	// Decision patterns: user is stating a decision
	if suggestion == "" {
		decisionPatterns := []string{
			"we decided", "decision is", "chosen approach", "will use",
			"going with", "selected", "architecture is", "design is",
		}
		for _, pattern := range decisionPatterns {
			if strings.Contains(lower, pattern) {
				suggestion = userMessage
				category = storage.MemoryCategoryDecision
				break
			}
		}
	}

	if suggestion != "" && len(suggestion) > 10 {
		// Clean up the suggestion
		suggestion = strings.TrimSpace(suggestion)
		if len(suggestion) > 200 {
			suggestion = suggestion[:197] + "..."
		}

		icon := getCategoryIcon(category)
		fmt.Printf("\n%s Remember: \"%s\"? [y/N] ", icon, suggestion)

		var confirm string
		_, _ = fmt.Scanln(&confirm)

		if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
			mem, err := mm.Create(category, suggestion, "", nil, storage.MemorySourceAuto)
			if err != nil {
				fmt.Printf("Error saving: %v\n", err)
			} else {
				fmt.Printf("Saved [%s] (ID: %s)\n", category, mem.ID)
			}
		}
		fmt.Println()
	}
}

// extractMemorySuggestion extracts the key content from a correction message
func extractMemorySuggestion(message string) string {
	lower := strings.ToLower(message)

	// Remove common correction prefixes
	prefixes := []string{
		"no,", "no ", "that's wrong,", "that's wrong ", "actually,", "actually ",
		"i meant", "not like that,", "not like that ",
	}

	result := message
	for _, prefix := range prefixes {
		idx := strings.Index(lower, prefix)
		if idx == 0 {
			result = strings.TrimSpace(message[len(prefix):])
			break
		}
	}

	// Clean up
	result = strings.TrimLeft(result, " ,.-")

	return result
}
