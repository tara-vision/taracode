# Tara Code Roadmap

**Officially Supported Model:** Qwen3 family (`qwen3:30b` recommended)

## Implemented Features

### Core
- [x] Interactive REPL with readline support
- [x] Multi-vendor support (vLLM, Ollama, llama.cpp)
- [x] Auto vendor detection from host URL or endpoint probing
- [x] Auto model detection from `/v1/models` endpoint
- [x] Project context via TARACODE.md
- [x] Slash commands (`/init`, `/reload`, `/clear`, `/help`, `/status`)
- [x] Configuration via env vars, config file, or CLI flags
- [x] DeepSeek R1 `<think>` tag handling
- [x] Clean tool output (concise status instead of raw dumps)
- [x] Streaming responses (real-time output as it generates)
- [x] Multi-turn tool calling (multiple tools in single LLM response)
- [x] Error recovery with retry logic and timeouts
- [x] Token usage tracking (`/usage` command)

### File Operations
- [x] `read_file` - Read files (with optional line range)
- [x] `write_file` - Create/overwrite files
- [x] `append_file` - Append to files
- [x] `edit_file` - Find and replace text
- [x] `insert_lines` - Insert at specific line
- [x] `replace_lines` - Replace line range
- [x] `delete_lines` - Delete line range
- [x] `copy_file` - Copy files
- [x] `move_file` - Move/rename files
- [x] `delete_file` - Delete files
- [x] `create_directory` - Create directories (with parents)
- [x] `list_files` - List directory (recursive option)
- [x] `find_files` - Glob pattern search

### Search & Commands
- [x] `search_files` - Grep pattern search
- [x] `execute_command` - Shell command execution

### Git Operations
- [x] `git_status` - Repository status
- [x] `git_diff` - Show changes (staged option)
- [x] `git_log` - Commit history
- [x] `git_add` - Stage files
- [x] `git_commit` - Create commits
- [x] `git_branch` - List branches

### Testing
- [x] Comprehensive test suite for all tools

---

## Pending Features

### High Priority
- [x] Multi-turn tool calling in a single response
- [x] Better error recovery and retry logic
- [x] Token usage tracking and display

### Git Operations
- [ ] `git_checkout` - Switch branches
- [ ] `git_push` - Push to remote
- [ ] `git_pull` - Pull from remote
- [ ] `git_stash` - Stash changes
- [ ] `git_merge` - Merge branches
- [ ] `git_rebase` - Rebase branches

### File Operations
- [ ] File permissions handling

### Code Intelligence
- [ ] Syntax-aware editing (AST-based)
- [ ] Go-specific refactoring tools
- [ ] Import management
- [ ] Code formatting integration

### UI/UX
- [x] Syntax highlighting in output (Glamour markdown rendering)
- [x] Progress indicators for long operations (thinking spinner, tool spinners)
- [x] Styled terminal output (Lipgloss styling)
- [ ] Conversation history persistence
- [ ] Multiple conversation sessions

### Configuration
- [ ] Per-project config files
- [ ] Custom tool definitions
- [ ] Prompt templates/customization

### Platform
- [ ] Linux testing and packaging
- [x] Homebrew formula publishing
- [x] Binary releases via GitHub Actions

---

## Version History

### v0.2.2 (Current)
- **Officially Supported Model**: Qwen3 family selected as the official model
- Recommended: `qwen3:30b` for best results, `qwen3:14b` for smaller hardware
- Optimized system prompt for consistent tool calling behavior
- Added git safety: models cannot commit without explicit user permission

### v0.2.1
- **Streaming Fix**: Disabled HTTP client timeout for streaming LLM responses
- Prevents premature disconnection during long-running streaming responses
- Improves reliability for models with longer generation times

### v0.2.0
- **Multi-Vendor Support**: Works with vLLM, Ollama, and llama.cpp servers
- Auto-detection of vendor from host URL patterns (ollama.*, vllm.*, etc.)
- Endpoint probing fallback for unknown hosts
- `--vendor` flag for explicit vendor selection
- `/status` command shows provider info (type, host, model)
- Provider abstraction layer (`internal/provider/` package)
- Updated documentation for multi-vendor usage

### v0.1.9
- **Token Usage Tracking**: Track and display LLM token consumption
- `/usage` command to show session token statistics
- Tracks prompt tokens, completion tokens, and total
- Works in both streaming and non-streaming modes
- TokenUsage struct for per-message and session-wide tracking

### v0.1.8
- **Error Recovery & Retry Logic**: Production-grade resilience for network operations
- HTTP client with configurable timeouts (30s default, 10s connect)
- Exponential backoff retry for transient errors (connection refused, timeouts)
- Automatic retry up to 3 times with visual feedback ("↻ retrying...")
- Context timeout for API responses (5 minute limit)
- Command execution timeout (60s default, configurable via `timeout` param)
- Improved error messages guiding users on recovery

### v0.1.7
- **Multi-turn Tool Calling**: LLM can now make multiple tool calls in a single response
- Parse multiple JSON tool objects or JSON arrays of tool calls
- Sequential execution with progress indicators ("Running 1/3...")
- Aggregated results sent back to LLM for efficient multi-step tasks
- Significantly reduces round-trips for gathering information

### v0.1.6
- **Claude Code-like UX**: Response buffered while "Thinking..." spinner shows
- Both streaming and non-streaming modes now render through Glamour
- Spinners for thinking and tool execution in both modes
- Clean, formatted output with syntax highlighting appears when ready
- No more raw markdown flashing on screen

### v0.1.5
- **Smart Markdown Re-rendering**: Streaming output is automatically re-rendered with syntax highlighting when complete
- Code blocks now display with proper colors and formatting after streaming finishes
- Improved user experience: fast streaming feedback + beautiful final output

### v0.1.4
- **UI/UX Overhaul**: Polished terminal output using Charm libraries
- Thinking spinner: Animated indicator while waiting for LLM response
- Tool execution spinners: Visual feedback during tool operations
- Markdown rendering: Syntax highlighting for code blocks (Glamour)
- Styled output: Consistent styling with Lipgloss
- New `internal/ui/` package with styles, spinner, markdown, renderer
- Added `--no-spinner` flag to disable spinner animations
- Updated welcome message with styled rendering

### v0.1.3
- **Streaming responses**: Real-time output as the LLM generates text
- Added `create_directory` tool for creating directories with parent paths
- Added `--no-stream` flag to disable streaming (for compatibility)
- StreamFilter for filtering `<think>` tags during streaming
- 21 tools implemented

### v0.1.2
- Added file manipulation tools: `copy_file`, `move_file`, `delete_file`
- 20 tools implemented
- Comprehensive test coverage for new tools
- Improved file operations capabilities
- Bug fixes and improvements

### v0.1.1
- Initial release
- 17 tools implemented
- Auto model detection
- Clean output formatting
