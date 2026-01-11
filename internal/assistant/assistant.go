package assistant

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/tara-vision/taracode/internal/context"
	"github.com/tara-vision/taracode/internal/provider"
	"github.com/tara-vision/taracode/internal/storage"
	"github.com/tara-vision/taracode/internal/tools"
	"github.com/tara-vision/taracode/internal/ui"
	openai "github.com/sashabaranov/go-openai"
)

// Timeout and retry constants
const (
	defaultConnectTimeout = 10 * time.Second
	providerInitTimeout   = 2 * time.Minute // Timeout for provider initialization with retries
	maxRetries            = 3
	initialBackoff        = 1 * time.Second
	maxBackoff            = 30 * time.Second
	apiResponseTimeout    = 5 * time.Minute
)

// newHTTPClient creates an HTTP client for streaming LLM responses.
// Client-level timeout is disabled (0) to allow long-running streaming responses.
// Timeout is controlled via context (apiResponseTimeout) instead.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0, // Disabled - use context timeout for streaming
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   defaultConnectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// isRetryable checks if an error is transient and worth retrying
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Network timeouts
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Connection errors
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	// Check error message for common transient patterns
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "no such host") ||
		strings.Contains(errMsg, "temporary failure")
}

// withRetry executes fn with exponential backoff retry for transient errors
func withRetry[T any](ctx gocontext.Context, operation string, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		result, lastErr = fn()
		if lastErr == nil {
			return result, nil
		}
		if !isRetryable(lastErr) {
			return result, lastErr
		}
		if attempt < maxRetries {
			fmt.Printf("  ↻ %s failed, retrying in %v (%d/%d)...\n",
				operation, backoff, attempt, maxRetries)
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(backoff):
			}
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
	return result, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

type Assistant struct {
	provider     provider.Provider
	client       *openai.Client
	model        string
	conversation []openai.ChatCompletionMessage
	toolRegistry *tools.Registry
	workingDir   string
	streaming    bool // Enable streaming output (default: true)
	enableSpinner bool // Enable spinner animations (default: true)
	renderer     *ui.Renderer

	// Persistence fields
	storage    *storage.Manager
	session    *storage.Session
	projectCtx *context.ProjectContext

	// Token usage tracking
	sessionUsage *storage.TokenUsage
}

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
			} else if strings.HasPrefix("<think", bufStr) {
				// Partial match, keep buffering
			} else if len(bufStr) > 0 && bufStr[0] == '<' && len(bufStr) < 7 {
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

const baseSystemPrompt = `You are Tara Code, an AI CLI assistant with FULL ACCESS to the user's filesystem.

## CORE RULES (ALWAYS FOLLOW)

1. COMPLETE TASKS FULLY - Don't explain what you would do, actually DO IT
2. "file called X" or "create X.md" = YOU MUST use write_file tool
3. EXPLORE FIRST - Always read files before answering questions about them
4. Use REAL file names from the project, never make up names
5. NEVER git_commit or git_add without explicit user permission - only use git_status, git_diff, git_log freely

## TASK WORKFLOW

For ANY task:
1. EXPLORE: Use list_files, read_file to understand the project
2. ACT: Use appropriate tools (write_file, edit_file, etc.)
3. CONFIRM: Tell user what was done

Example - "explain project in a file called DOC.md":
1. list_files → read_file README.md, main.go
2. write_file DOC.md with content based on what you read
3. "Created DOC.md with project documentation"

## TOOLS

Use tools by outputting JSON: {"tool": "name", "params": {...}}

FILE TOOLS:
- read_file: {"tool": "read_file", "params": {"file_path": "path"}}
- write_file: {"tool": "write_file", "params": {"file_path": "path", "content": "..."}}
- edit_file: {"tool": "edit_file", "params": {"file_path": "path", "old_string": "find", "new_string": "replace"}}
- append_file: {"tool": "append_file", "params": {"file_path": "path", "content": "..."}}
- list_files: {"tool": "list_files", "params": {"directory": ".", "recursive": false}}
- find_files: {"tool": "find_files", "params": {"pattern": "*.go", "directory": "."}}
- copy_file: {"tool": "copy_file", "params": {"source_path": "src", "dest_path": "dst"}}
- move_file: {"tool": "move_file", "params": {"source_path": "src", "dest_path": "dst"}}
- delete_file: {"tool": "delete_file", "params": {"file_path": "path"}}
- create_directory: {"tool": "create_directory", "params": {"path": "dir/path"}}
- insert_lines: {"tool": "insert_lines", "params": {"file_path": "path", "line_number": 5, "content": "..."}}
- replace_lines: {"tool": "replace_lines", "params": {"file_path": "path", "start_line": 1, "end_line": 5, "content": "..."}}
- delete_lines: {"tool": "delete_lines", "params": {"file_path": "path", "start_line": 1, "end_line": 5}}

SEARCH:
- search_files: {"tool": "search_files", "params": {"pattern": "term", "directory": "."}}
- execute_command: {"tool": "execute_command", "params": {"command": "go build"}}

GIT (status/diff/log are free, add/commit require user permission):
- git_status: {"tool": "git_status", "params": {}}
- git_diff: {"tool": "git_diff", "params": {}}
- git_log: {"tool": "git_log", "params": {"limit": 10}}
- git_add: {"tool": "git_add", "params": {"files": ["file.go"]}} (ASK FIRST)
- git_commit: {"tool": "git_commit", "params": {"message": "feat: message"}} (ASK FIRST)
- git_branch: {"tool": "git_branch", "params": {}}

## MULTIPLE TOOLS

Call multiple tools at once for efficiency:
{"tool": "list_files", "params": {"directory": "."}}
{"tool": "read_file", "params": {"file_path": "README.md"}}

## OUTPUT FORMAT

For explanations, use this structure:
## Overview
[Brief description]

## Key Components
- **Component**: Description

## Important Files
- path/file - purpose`

// detectModel queries the /v1/models endpoint to get the served model
func detectModel(ctx gocontext.Context, httpClient *http.Client, host, apiKey string) (string, error) {
	return withRetry(ctx, "model detection", func() (string, error) {
		host = strings.TrimSuffix(host, "/")
		url := host + "/v1/models"

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err // Let isRetryable check this
		}
		defer resp.Body.Close()

		// 5xx errors are retryable (server overloaded)
		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("server error (status %d): %s", resp.StatusCode, string(body))
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("failed to query /v1/models (status %d): %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		var modelsResp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &modelsResp); err != nil {
			return "", fmt.Errorf("failed to parse /v1/models response: %w", err)
		}

		if len(modelsResp.Data) == 0 {
			return "", fmt.Errorf("no models returned from /v1/models")
		}

		// Return the first (typically only) model
		return modelsResp.Data[0].ID, nil
	})
}

func New(host, apiKey, configModel, vendor string, streaming bool, enableSpinner bool) (*Assistant, error) {
	renderer := ui.NewRenderer()

	// Create context with timeout for provider initialization
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), providerInitTimeout)
	defer cancel()

	// Create provider (auto-detects vendor if not specified)
	prov, err := provider.New(ctx, host, vendor, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	// Get OpenAI-compatible client from provider
	client := prov.CreateClient()

	// Auto-detect model from server
	models, err := prov.DetectModels(ctx)
	var model string
	if err != nil {
		// Fallback to config model if detection fails
		if configModel != "" {
			fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not auto-detect model (%v), using configured model: %s", err, configModel)))
			model = configModel
		} else {
			return nil, fmt.Errorf("failed to detect model and no fallback configured: %w", err)
		}
	} else if len(models) > 0 {
		// Check if configured model is available on server
		if configModel != "" {
			configModelFound := false
			for _, m := range models {
				if m == configModel {
					configModelFound = true
					break
				}
			}
			if configModelFound {
				model = configModel
				fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Using configured model: %s", model)))
			} else {
				model = models[0]
				fmt.Println(renderer.WarningMessage(fmt.Sprintf("Configured model '%s' not available on server. Available: %v. Using: %s", configModel, models, model)))
			}
		} else {
			model = models[0]
			fmt.Println(renderer.SuccessMessage(fmt.Sprintf("Auto-detected model: %s", model)))
		}
	} else {
		if configModel != "" {
			model = configModel
		} else {
			return nil, fmt.Errorf("no models available and no fallback configured")
		}
	}

	// Update provider with selected model
	prov.SetModel(model)

	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Initialize storage manager (non-fatal if fails)
	var storageMgr *storage.Manager
	var session *storage.Session
	var projectCtx *context.ProjectContext

	storageMgr, err = storage.NewManager(workingDir)
	if err != nil {
		// Storage initialization failed - continue without persistence
		fmt.Println(renderer.WarningMessage(fmt.Sprintf("Could not initialize storage: %v", err)))
	} else {
		// Try to load or create active session
		session, _ = storageMgr.GetActiveSession()
		if session == nil {
			session, _ = storageMgr.CreateSession("")
		}

		// Load project context if available
		projectCtx, _ = storageMgr.LoadProjectContext()
	}

	// Build system prompt with project context if available
	systemPrompt := buildSystemPrompt(workingDir, storageMgr)

	systemMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}

	return &Assistant{
		provider:      prov,
		client:        client,
		model:         model,
		conversation:  []openai.ChatCompletionMessage{systemMessage},
		toolRegistry:  tools.NewRegistry(),
		workingDir:    workingDir,
		streaming:     streaming,
		enableSpinner: enableSpinner,
		renderer:      renderer,
		storage:       storageMgr,
		session:       session,
		projectCtx:    projectCtx,
		sessionUsage:  &storage.TokenUsage{},
	}, nil
}

// GetSession returns the current session
func (a *Assistant) GetSession() *storage.Session {
	return a.session
}

// GetStorage returns the storage manager
func (a *Assistant) GetStorage() *storage.Manager {
	return a.storage
}

// GetUsage returns the current session token usage
func (a *Assistant) GetUsage() *storage.TokenUsage {
	return a.sessionUsage
}

// GetProviderInfo returns information about the current LLM provider
func (a *Assistant) GetProviderInfo() *provider.Info {
	if a.provider == nil {
		return nil
	}
	return a.provider.Info()
}

// ListSessions returns all available sessions
func (a *Assistant) ListSessions() ([]storage.SessionMetadata, error) {
	if a.storage == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	return a.storage.ListSessions()
}

// NewSession creates a new conversation session
func (a *Assistant) NewSession(name string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.CreateSession(name)
	if err != nil {
		return err
	}

	a.session = session

	// Reset conversation to just system message
	systemPrompt := buildSystemPrompt(a.workingDir, a.storage)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	return nil
}

// LoadSession loads a previous session by ID
func (a *Assistant) LoadSession(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}

	session, err := a.storage.GetSession(id)
	if err != nil {
		return err
	}

	a.session = session
	a.storage.SetActiveSession(id)

	// Rebuild conversation from session messages
	systemPrompt := buildSystemPrompt(a.workingDir, a.storage)
	a.conversation = []openai.ChatCompletionMessage{{
		Role:    openai.ChatMessageRoleSystem,
		Content: systemPrompt,
	}}

	// Add messages from session
	for _, msg := range session.Messages {
		role := openai.ChatMessageRoleUser
		if msg.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		})
	}

	return nil
}

// buildSystemPrompt creates the system prompt, including project context if available
func buildSystemPrompt(workingDir string, storageMgr *storage.Manager) string {
	prompt := baseSystemPrompt

	// Check for TARACODE.md in current directory
	taracodeFile := filepath.Join(workingDir, "TARACODE.md")
	if content, err := os.ReadFile(taracodeFile); err == nil {
		prompt += fmt.Sprintf("\n\n## PROJECT CONTEXT\nThe following is project-specific guidance from TARACODE.md:\n\n%s", string(content))
	}

	// Include active plan if exists
	if storageMgr != nil {
		if plan, err := storageMgr.GetActivePlan(); err == nil && plan != nil {
			prompt += "\n\n## ACTIVE PLAN\n"
			prompt += fmt.Sprintf("**%s**\n", plan.Title)
			for i, task := range plan.Tasks {
				status := "[ ]"
				if task.Status == storage.TaskStatusCompleted {
					status = "[x]"
				} else if task.Status == storage.TaskStatusInProgress {
					status = "[>]"
				}
				prompt += fmt.Sprintf("%d. %s %s\n", i+1, status, task.Content)
			}
			prompt += "\nUpdate task status as you complete them."
		}
	}

	// Add working directory context
	prompt += fmt.Sprintf("\n\nCurrent working directory: %s", workingDir)

	return prompt
}

// InitProject analyzes the project and creates TARACODE.md with comprehensive context
func InitProject(workingDir string) error {
	fmt.Println("Analyzing project structure...")

	// Initialize storage manager (creates .taracode/ structure)
	storageMgr, err := storage.NewManager(workingDir)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Explore project with smart filtering
	fmt.Println("  Exploring directories...")
	opts := context.DefaultExplorerOptions()
	tree, err := context.ExploreProject(workingDir, opts)
	if err != nil {
		return fmt.Errorf("failed to explore project: %w", err)
	}

	// Analyze important files
	fmt.Println("  Analyzing key files...")
	analyses := context.AnalyzeImportantFiles(workingDir, tree)

	// Build project context
	projectCtx := &context.ProjectContext{
		RootPath:       workingDir,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Structure:      tree,
		ImportantFiles: analyses,
	}

	// Detect project type and extract metadata
	detectProjectType(workingDir, projectCtx)
	extractBuildCommands(workingDir, projectCtx)
	extractGitInfo(workingDir, projectCtx)

	// Save context to .taracode/context/project.json
	if err := storageMgr.SaveProjectContext(projectCtx); err != nil {
		fmt.Printf("  Warning: Could not save project context: %v\n", err)
	}

	// Generate TARACODE.md from context
	if err := generateTaracodeMD(workingDir, projectCtx); err != nil {
		return fmt.Errorf("failed to generate TARACODE.md: %w", err)
	}

	// Print summary
	printInitSummary(projectCtx)

	return nil
}

// detectProjectType identifies the project type from manifest files
func detectProjectType(workingDir string, ctx *context.ProjectContext) {
	// Go project
	if content, err := os.ReadFile(filepath.Join(workingDir, "go.mod")); err == nil {
		ctx.ProjectType = "Go"
		lines := strings.Split(string(content), "\n")
		if len(lines) > 0 {
			ctx.ModuleName = strings.TrimPrefix(strings.TrimSpace(lines[0]), "module ")
		}
		// Extract dependencies
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "require ") || (strings.HasPrefix(line, "\t") && strings.Contains(line, " v")) {
				parts := strings.Fields(line)
				if len(parts) >= 1 {
					dep := strings.TrimPrefix(parts[0], "require")
					dep = strings.TrimSpace(dep)
					if dep != "" && dep != "(" && dep != ")" {
						ctx.Dependencies = append(ctx.Dependencies, dep)
					}
				}
			}
		}
		return
	}

	// Node.js project
	if content, err := os.ReadFile(filepath.Join(workingDir, "package.json")); err == nil {
		ctx.ProjectType = "Node.js"
		var pkg map[string]interface{}
		if json.Unmarshal(content, &pkg) == nil {
			if name, ok := pkg["name"].(string); ok {
				ctx.ModuleName = name
			}
			// Extract dependencies
			if deps, ok := pkg["dependencies"].(map[string]interface{}); ok {
				for dep := range deps {
					ctx.Dependencies = append(ctx.Dependencies, dep)
				}
			}
		}
		return
	}

	// Python project
	if _, err := os.Stat(filepath.Join(workingDir, "pyproject.toml")); err == nil {
		ctx.ProjectType = "Python"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "requirements.txt")); err == nil {
		ctx.ProjectType = "Python"
		return
	}
	if _, err := os.Stat(filepath.Join(workingDir, "setup.py")); err == nil {
		ctx.ProjectType = "Python"
		return
	}

	// Rust project
	if content, err := os.ReadFile(filepath.Join(workingDir, "Cargo.toml")); err == nil {
		ctx.ProjectType = "Rust"
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "name = ") {
				ctx.ModuleName = strings.Trim(strings.TrimPrefix(line, "name = "), `"'`)
				break
			}
		}
		return
	}
}

// extractBuildCommands extracts build commands from Makefile
func extractBuildCommands(workingDir string, ctx *context.ProjectContext) {
	content, err := os.ReadFile(filepath.Join(workingDir, "Makefile"))
	if err != nil {
		return
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		// Match targets that are not indented and end with :
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, ".") && !strings.HasPrefix(line, " ") {
			target := strings.TrimSuffix(line, ":")
			// Skip targets with special characters or spaces
			if !strings.ContainsAny(target, " \t$%") {
				ctx.BuildCommands = append(ctx.BuildCommands, fmt.Sprintf("make %s", target))
			}
		}
	}
}

// extractGitInfo extracts git repository information
func extractGitInfo(workingDir string, ctx *context.ProjectContext) {
	gitDir := filepath.Join(workingDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return
	}

	ctx.GitInfo = &context.GitInfo{}

	// Get current branch
	if out, err := exec.Command("git", "-C", workingDir, "branch", "--show-current").Output(); err == nil {
		ctx.GitInfo.Branch = strings.TrimSpace(string(out))
	}

	// Get remote URL
	if out, err := exec.Command("git", "-C", workingDir, "remote", "get-url", "origin").Output(); err == nil {
		ctx.GitInfo.RemoteURL = strings.TrimSpace(string(out))
	}

	// Check for uncommitted changes
	if out, err := exec.Command("git", "-C", workingDir, "status", "--porcelain").Output(); err == nil {
		ctx.GitInfo.HasUncommitted = len(strings.TrimSpace(string(out))) > 0
	}

	// Get last commit
	if out, err := exec.Command("git", "-C", workingDir, "log", "-1", "--format=%h %s").Output(); err == nil {
		ctx.GitInfo.LastCommit = strings.TrimSpace(string(out))
	}
}

// generateTaracodeMD creates the TARACODE.md file from project context
func generateTaracodeMD(workingDir string, ctx *context.ProjectContext) error {
	var sb strings.Builder

	sb.WriteString("# TARACODE.md\n\n")
	sb.WriteString("This file provides context to Tara Code. Auto-generated by `/init`.\n\n")

	// Project overview
	sb.WriteString("## Project Overview\n\n")
	if ctx.ProjectType != "" {
		sb.WriteString(fmt.Sprintf("**Type:** %s project\n", ctx.ProjectType))
	}
	if ctx.ModuleName != "" {
		sb.WriteString(fmt.Sprintf("**Module:** %s\n", ctx.ModuleName))
	}
	sb.WriteString("\n")

	// Project structure (tree view)
	sb.WriteString("## Project Structure\n\n```\n")
	writeTreeStructure(&sb, ctx.Structure, "", true)
	sb.WriteString("```\n\n")

	// Important files with summaries
	if len(ctx.ImportantFiles) > 0 {
		sb.WriteString("## Key Files\n\n")
		for _, file := range ctx.ImportantFiles {
			sb.WriteString(fmt.Sprintf("- **`%s`** - %s\n", file.Path, file.Summary))
		}
		sb.WriteString("\n")
	}

	// Build commands
	if len(ctx.BuildCommands) > 0 {
		sb.WriteString("## Build Commands\n\n```bash\n")
		for _, cmd := range ctx.BuildCommands {
			sb.WriteString(cmd + "\n")
		}
		sb.WriteString("```\n\n")
	}

	// Git info
	if ctx.GitInfo != nil && ctx.GitInfo.Branch != "" {
		sb.WriteString("## Git Info\n\n")
		sb.WriteString(fmt.Sprintf("- **Branch:** %s\n", ctx.GitInfo.Branch))
		if ctx.GitInfo.RemoteURL != "" {
			sb.WriteString(fmt.Sprintf("- **Remote:** %s\n", ctx.GitInfo.RemoteURL))
		}
		if ctx.GitInfo.LastCommit != "" {
			sb.WriteString(fmt.Sprintf("- **Last commit:** %s\n", ctx.GitInfo.LastCommit))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n*Edit this file to add custom instructions for Tara Code.*\n")

	return os.WriteFile(filepath.Join(workingDir, "TARACODE.md"), []byte(sb.String()), 0644)
}

// writeTreeStructure writes the directory tree in a visual format
func writeTreeStructure(sb *strings.Builder, node *context.DirectoryTree, prefix string, isLast bool) {
	if node == nil {
		return
	}

	// Handle root node specially
	if node.Path == "" {
		for i, child := range node.Children {
			writeTreeStructure(sb, child, "", i == len(node.Children)-1)
		}
		return
	}

	connector := "├── "
	if isLast {
		connector = "└── "
	}

	displayName := node.Name
	if node.IsDir {
		displayName += "/"
	}

	sb.WriteString(prefix + connector + displayName + "\n")

	if node.IsDir && len(node.Children) > 0 {
		newPrefix := prefix
		if isLast {
			newPrefix += "    "
		} else {
			newPrefix += "│   "
		}

		for i, child := range node.Children {
			writeTreeStructure(sb, child, newPrefix, i == len(node.Children)-1)
		}
	}
}

// printInitSummary prints a summary of the initialization
func printInitSummary(ctx *context.ProjectContext) {
	fmt.Println()
	fmt.Println("✓ Project initialized successfully!")
	fmt.Println()

	if ctx.ProjectType != "" {
		fmt.Printf("  Type: %s", ctx.ProjectType)
		if ctx.ModuleName != "" {
			fmt.Printf(" (%s)", ctx.ModuleName)
		}
		fmt.Println()
	}

	fileCount := context.CountFiles(ctx.Structure)
	dirCount := context.CountDirs(ctx.Structure)
	fmt.Printf("  Structure: %d files, %d directories\n", fileCount, dirCount)
	fmt.Printf("  Key files analyzed: %d\n", len(ctx.ImportantFiles))

	if len(ctx.BuildCommands) > 0 {
		fmt.Printf("  Build commands: %d\n", len(ctx.BuildCommands))
	}

	if ctx.GitInfo != nil && ctx.GitInfo.Branch != "" {
		fmt.Printf("  Git branch: %s\n", ctx.GitInfo.Branch)
	}

	fmt.Println()
	fmt.Println("  Created:")
	fmt.Println("    - TARACODE.md (project context for AI)")
	fmt.Println("    - .taracode/ (storage for history, plans, state)")
	fmt.Println()
	fmt.Println("Edit TARACODE.md to add custom instructions.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ToolCall represents a parsed tool call from the model's response
type ToolCall struct {
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
			if c == '\n' {
				normalized.WriteString("\\n")
			} else if c == '\t' {
				normalized.WriteString("\\t")
			} else {
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

// extractJSONObjects finds JSON objects starting with {"tool" using brace matching
func extractJSONObjects(text string) []string {
	var results []string

	// Find all positions where a tool call JSON might start
	toolPattern := regexp.MustCompile(`\{\s*"tool"\s*:`)
	indices := toolPattern.FindAllStringIndex(text, -1)

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

// parseToolCalls extracts ALL tool calls from the model's response (supports multiple tools)
func parseToolCalls(response string) ([]*ToolCall, string) {
	cleaned := cleanResponse(response)
	var toolCalls []*ToolCall
	seen := make(map[string]bool) // Track seen tool calls to avoid duplicates
	var firstToolIdx int = -1

	// Extract JSON objects using brace matching (handles nested objects and multiline)
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
					toolCalls = append(toolCalls, &toolCall)

					// Track position of first tool call
					if firstToolIdx == -1 {
						firstToolIdx = strings.Index(cleaned, jsonStr)
					}
				}
			}
		}
	}

	// Also try to find JSON arrays of tool calls
	// [{"tool": "...", "params": {...}}, {"tool": "...", "params": {...}}]
	arrayPattern := regexp.MustCompile(`\[\s*\{`)
	if arrayIdx := arrayPattern.FindStringIndex(cleaned); arrayIdx != nil {
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
							toolCalls = append(toolCalls, &arrayToolCalls[i])
						}
					}
				}
				if firstToolIdx == -1 || start < firstToolIdx {
					firstToolIdx = start
				}
			}
		}
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

// formatToolStatus returns a concise, human-friendly status for tool execution
func formatToolStatus(tool string, params map[string]interface{}, result string, isError bool) string {
	gray := "\033[90m"
	green := "\033[32m"
	red := "\033[31m"
	reset := "\033[0m"

	if isError {
		return fmt.Sprintf("%s✗ %s failed%s", red, tool, reset)
	}

	switch tool {
	case "read_file":
		filePath, _ := params["file_path"].(string)
		lines := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Read %s (%d lines)%s", gray, filepath.Base(filePath), lines, reset)

	case "search_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No matches") {
			return fmt.Sprintf("%s→ Searched for \"%s\" (no matches)%s", gray, pattern, reset)
		}
		return fmt.Sprintf("%s→ Searched for \"%s\" (%d matches)%s", gray, pattern, matches, reset)

	case "list_files":
		dir, _ := params["directory"].(string)
		if dir == "" || dir == "." {
			dir = "current directory"
		}
		items := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Listed %s (%d items)%s", gray, dir, items, reset)

	case "execute_command":
		cmd, _ := params["command"].(string)
		if len(cmd) > 40 {
			cmd = cmd[:37] + "..."
		}
		return fmt.Sprintf("%s→ Executed: %s%s", gray, cmd, reset)

	case "write_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Wrote %s%s", green, filepath.Base(filePath), reset)

	case "append_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Appended to %s%s", green, filepath.Base(filePath), reset)

	case "edit_file":
		filePath, _ := params["file_path"].(string)
		return fmt.Sprintf("%s✓ Edited %s%s", green, filepath.Base(filePath), reset)

	case "insert_lines":
		filePath, _ := params["file_path"].(string)
		lineNum, _ := params["line_number"].(float64)
		return fmt.Sprintf("%s✓ Inserted at line %d in %s%s", green, int(lineNum), filepath.Base(filePath), reset)

	case "replace_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return fmt.Sprintf("%s✓ Replaced lines %d-%d in %s%s", green, int(startLine), int(endLine), filepath.Base(filePath), reset)

	case "delete_lines":
		filePath, _ := params["file_path"].(string)
		startLine, _ := params["start_line"].(float64)
		endLine, _ := params["end_line"].(float64)
		return fmt.Sprintf("%s✓ Deleted lines %d-%d from %s%s", green, int(startLine), int(endLine), filepath.Base(filePath), reset)

	case "copy_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return fmt.Sprintf("%s✓ Copied %s to %s%s", green, filepath.Base(src), filepath.Base(dst), reset)

	case "move_file":
		src, _ := params["source_path"].(string)
		dst, _ := params["dest_path"].(string)
		return fmt.Sprintf("%s✓ Moved %s to %s%s", green, filepath.Base(src), filepath.Base(dst), reset)

	case "delete_file":
		filePath, _ := params["file_path"].(string)
		recursive, _ := params["recursive"].(bool)
		if recursive {
			return fmt.Sprintf("%s✓ Deleted %s (recursive)%s", green, filepath.Base(filePath), reset)
		}
		return fmt.Sprintf("%s✓ Deleted %s%s", green, filepath.Base(filePath), reset)

	case "create_directory":
		dirPath, _ := params["path"].(string)
		return fmt.Sprintf("%s✓ Created directory %s%s", green, filepath.Base(dirPath), reset)

	case "find_files":
		pattern, _ := params["pattern"].(string)
		matches := strings.Count(result, "\n")
		if strings.Contains(result, "No files found") {
			return fmt.Sprintf("%s→ Find \"%s\" (no matches)%s", gray, pattern, reset)
		}
		return fmt.Sprintf("%s→ Find \"%s\" (%d files)%s", gray, pattern, matches, reset)

	case "git_status":
		if strings.Contains(result, "clean") {
			return fmt.Sprintf("%s→ Git status: clean%s", gray, reset)
		}
		changes := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Git status: %d changes%s", gray, changes, reset)

	case "git_diff":
		if strings.Contains(result, "No changes") {
			return fmt.Sprintf("%s→ Git diff: no changes%s", gray, reset)
		}
		lines := strings.Count(result, "\n")
		return fmt.Sprintf("%s→ Git diff: %d lines%s", gray, lines, reset)

	case "git_log":
		commits := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Git log: %d commits%s", gray, commits, reset)

	case "git_add":
		return fmt.Sprintf("%s✓ Git: staged files%s", green, reset)

	case "git_commit":
		return fmt.Sprintf("%s✓ Git: commit created%s", green, reset)

	case "git_branch":
		branches := strings.Count(result, "\n") + 1
		return fmt.Sprintf("%s→ Git branches: %d%s", gray, branches, reset)

	default:
		return fmt.Sprintf("%s→ %s completed%s", gray, tool, reset)
	}
}

func (a *Assistant) ProcessMessage(userMessage string) error {
	if a.streaming {
		return a.processMessageStreaming(userMessage)
	}
	return a.processMessageNonStreaming(userMessage)
}

// processMessageStreaming handles messages with real-time streaming output
func (a *Assistant) processMessageStreaming(userMessage string) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	a.conversation = append(a.conversation, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	maxIterations := 10

	for i := 0; i < maxIterations; i++ {
		// Start thinking spinner
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewSpinner()
			thinkingSpinner.Start("Thinking...")
		}

		stream, err := a.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
			StreamOptions: &openai.StreamOptions{
				IncludeUsage: true,
			},
		})
		if err != nil {
			if thinkingSpinner != nil {
				thinkingSpinner.Stop()
			}
			return fmt.Errorf("failed to create stream: %w", err)
		}

		filter := NewStreamFilter()

		// Buffer the response while showing spinner (Claude Code style)
		for {
			chunk, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				if thinkingSpinner != nil {
					thinkingSpinner.Stop()
				}
				stream.Close()
				return fmt.Errorf("stream error: %w", err)
			}

			if len(chunk.Choices) > 0 {
				delta := chunk.Choices[0].Delta.Content
				filter.Process(delta)
			}

			// Capture usage from final chunk (when StreamOptions.IncludeUsage is true)
			if chunk.Usage != nil {
				a.sessionUsage.PromptTokens += chunk.Usage.PromptTokens
				a.sessionUsage.CompletionTokens += chunk.Usage.CompletionTokens
				a.sessionUsage.TotalTokens += chunk.Usage.TotalTokens
			}
		}
		stream.Close()

		// Stop spinner now that response is complete
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		// Flush any remaining buffered content
		filter.Flush()

		fullResponse := filter.FullContent()

		// Parse for tool calls from content (supports multiple)
		toolCalls, displayText := parseToolCalls(fullResponse)

		if len(toolCalls) == 0 {
			// No tool calls - render the response with Glamour
			displayedText := cleanResponse(fullResponse)
			if displayedText != "" {
				fmt.Println(ui.RenderMarkdown(displayedText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: fullResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   fullResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Tool calls detected - add assistant response to conversation
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: fullResponse,
		})

		// Execute all tool calls and aggregate results
		var allResults strings.Builder
		totalTools := len(toolCalls)

		for idx, toolCall := range toolCalls {
			// Start tool execution spinner with progress
			var toolSpinner *ui.Spinner
			if a.enableSpinner {
				toolSpinner = ui.NewSpinner()
				if totalTools > 1 {
					toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
				} else {
					toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
				}
			}

			// Execute the tool
			startTime := time.Now()
			result, err := a.toolRegistry.ExecuteTool(toolCall.Tool, toolCall.Params, a.workingDir)
			duration := time.Since(startTime).Milliseconds()
			isError := err != nil
			if isError {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Stop tool spinner
			if toolSpinner != nil {
				toolSpinner.Stop()
			}

			// Print concise tool status using renderer
			fmt.Println(a.renderer.FormatToolStatus(toolCall.Tool, toolCall.Params, result, isError))

			// Aggregate results for sending back to LLM
			if totalTools > 1 {
				allResults.WriteString(fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result))
			} else {
				allResults.WriteString(fmt.Sprintf("Tool result:\n%s", result))
			}

			// Save tool call to session
			if a.storage != nil && a.session != nil {
				toolMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   fullResponse,
					Timestamp: time.Now(),
					ToolCall: &storage.ToolCallRecord{
						Tool:     toolCall.Tool,
						Params:   toolCall.Params,
						Result:   result,
						Duration: duration,
						Success:  !isError,
					},
				}
				a.storage.AddMessage(a.session.ID, toolMsg)
			}
		}

		// Add all tool results to conversation in one message
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: allResults.String(),
		})
	}

	return nil
}

// processMessageNonStreaming handles messages without streaming
func (a *Assistant) processMessageNonStreaming(userMessage string) error {
	// Record user message to session
	if a.storage != nil && a.session != nil {
		userMsg := storage.ConversationMessage{
			Role:      "user",
			Content:   userMessage,
			Timestamp: time.Now(),
		}
		a.storage.AddMessage(a.session.ID, userMsg)
	}

	a.conversation = append(a.conversation, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	// Create context with timeout for API response
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), apiResponseTimeout)
	defer cancel()

	maxIterations := 10

	for i := 0; i < maxIterations; i++ {
		// Start thinking spinner
		var thinkingSpinner *ui.Spinner
		if a.enableSpinner {
			thinkingSpinner = ui.NewSpinner()
			thinkingSpinner.Start("Thinking...")
		}

		resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: a.conversation,
		})

		// Stop spinner
		if thinkingSpinner != nil {
			thinkingSpinner.Stop()
		}

		if err != nil {
			return fmt.Errorf("failed to get response: %w", err)
		}

		if len(resp.Choices) == 0 {
			return fmt.Errorf("no response choices returned")
		}

		// Track token usage
		if resp.Usage.TotalTokens > 0 {
			a.sessionUsage.PromptTokens += resp.Usage.PromptTokens
			a.sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
			a.sessionUsage.TotalTokens += resp.Usage.TotalTokens
		}

		assistantResponse := resp.Choices[0].Message.Content

		// Parse for tool calls from content (supports multiple)
		toolCalls, displayText := parseToolCalls(assistantResponse)

		// If no tool calls, render and print response
		if len(toolCalls) == 0 {
			if displayText != "" {
				// Render with Glamour for syntax highlighting
				fmt.Println(ui.RenderMarkdown(displayText))
			}
			a.conversation = append(a.conversation, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: assistantResponse,
			})

			// Save assistant response to session
			if a.storage != nil && a.session != nil {
				assistantMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   assistantResponse,
					Timestamp: time.Now(),
				}
				a.storage.AddMessage(a.session.ID, assistantMsg)
			}
			break
		}

		// Display any text before tool calls
		if displayText != "" {
			fmt.Println(ui.RenderMarkdown(displayText))
		}

		// Add assistant response to conversation
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: assistantResponse,
		})

		// Execute all tool calls and aggregate results
		var allResults strings.Builder
		totalTools := len(toolCalls)

		for idx, toolCall := range toolCalls {
			// Start tool execution spinner with progress
			var toolSpinner *ui.Spinner
			if a.enableSpinner {
				toolSpinner = ui.NewSpinner()
				if totalTools > 1 {
					toolSpinner.Start(fmt.Sprintf("Running %s (%d/%d)...", toolCall.Tool, idx+1, totalTools))
				} else {
					toolSpinner.Start(fmt.Sprintf("Running %s...", toolCall.Tool))
				}
			}

			// Execute the tool
			startTime := time.Now()
			result, err := a.toolRegistry.ExecuteTool(toolCall.Tool, toolCall.Params, a.workingDir)
			duration := time.Since(startTime).Milliseconds()
			isError := err != nil
			if isError {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Stop tool spinner
			if toolSpinner != nil {
				toolSpinner.Stop()
			}

			// Print concise tool status using renderer
			fmt.Println(a.renderer.FormatToolStatus(toolCall.Tool, toolCall.Params, result, isError))

			// Aggregate results for sending back to LLM
			if totalTools > 1 {
				allResults.WriteString(fmt.Sprintf("[%d] %s result:\n%s\n\n", idx+1, toolCall.Tool, result))
			} else {
				allResults.WriteString(fmt.Sprintf("Tool result:\n%s", result))
			}

			// Save tool call to session
			if a.storage != nil && a.session != nil {
				toolMsg := storage.ConversationMessage{
					Role:      "assistant",
					Content:   assistantResponse,
					Timestamp: time.Now(),
					ToolCall: &storage.ToolCallRecord{
						Tool:     toolCall.Tool,
						Params:   toolCall.Params,
						Result:   result,
						Duration: duration,
						Success:  !isError,
					},
				}
				a.storage.AddMessage(a.session.ID, toolMsg)
			}
		}

		// Add all tool results to conversation in one message
		a.conversation = append(a.conversation, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: allResults.String(),
		})
	}

	return nil
}
