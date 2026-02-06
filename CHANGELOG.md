# Changelog

All notable changes to taracode will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.3] - 2026-02-07

### Fixed

- **get_datetime permission** - `get_datetime` tool was incorrectly categorized as "destructive", requiring user
  approval for every date/time query. Now correctly categorized as a read-only operation (auto-allowed).
- **Date/time answers** - Date/time questions now always return the correct answer. The application
  detects date/time queries and injects the real datetime into context, so the LLM cannot hallucinate
  dates from training data. Previously the 27B model would ignore the `get_datetime` tool and guess wrong.
- **git_stash permission** - `git_stash` tool was missing from permission category map, defaulting to
  "destructive". Now correctly categorized as a git operation.
- **Help text completeness** - `/permissions ask` subcommand was missing from `/help` output. `/hosts`
  commands were not listed in `/help`. Both are now included.
- **Permission error message** - `mcp` category was missing from the valid categories hint shown when
  an invalid tool or category name is provided.

### Changed

- Date/time questions are now intercepted at the application level with automatic datetime injection,
  removing dependency on LLM tool-calling behavior for reliable answers
- System prompt (both compact and full variants) now includes a dedicated CRITICAL RULE section for
  date/time handling
- Removed private host references (`ollama.tara.lab`) from all public documentation and examples, replaced
  with generic `gpu-server:11434` or `localhost:11434`

## [2.0.2] - 2026-02-06

### Added

- **Context Window Management** - Intelligent context budget management for local LLMs with limited context windows
  - **Tool output truncation** - Automatically truncates large tool outputs to prevent context overflow
    (configurable: `context.max_tool_output_lines`, `context.max_tool_output_chars`)
  - **Conversation compaction** - Auto-summarizes older messages via LLM when context usage exceeds threshold,
    keeping system prompt and recent messages intact (`context.compaction_enabled`, `context.compaction_threshold`)
  - **Tool-specific truncation hints** - Truncation notices include actionable hints per tool
    (e.g., "Use start_line/end_line" for read_file, "Use --tail" for kubectl_logs)
  - **Binary content detection** - Automatically detects and truncates binary tool output
- **`/compact` command** - Force conversation compaction on demand
- **`/stats` command** - Session statistics showing context usage, compaction history, truncation events,
  file operations, and settings
- **Tool execution duration display** - Tool status output now shows execution time for operations
  taking longer than 1 second (e.g., `[2.3s]`)
- **Enhanced `/context` command** - Shows detailed context budget breakdown (system prompt, tool definitions,
  conversation tokens, available space), compaction history, and truncation events
- **Configurable max tool iterations** - `context.max_tool_iterations` config option and `--max-iterations`
  CLI flag to limit consecutive tool calls per message (default: 10)
- **CLI flags** - `--max-tool-output`, `--max-iterations`, `--no-compaction` for runtime overrides

### Changed

- Context budget display in `/context` now shows per-component token breakdown
- Tool iteration limit is now configurable instead of hardcoded

### Fixed

- **Help text alignment** - Consistent column width across all `/help` command entries
- **Session delete UX** - `/session delete <id>` now checks session existence before prompting for confirmation
- **UTF-8 safety** - String truncation uses rune-safe slicing to prevent splitting multi-byte characters
- **Grammar** - Singular/plural handling in compaction summaries ("1 tool call" vs "2 tool calls")

## [2.0.1] - 2026-02-05

### Fixed

- **Multi-host fallback now works in main chat loop** - HostPool is properly wired to Assistant for automatic
  fallback when the primary host becomes unavailable
- **Accurate host health status display** - `/hosts` now shows correct healthy/total count; hosts are only marked
  healthy after connectivity verification via `DetectModels()`
- **Fallback notification** - Users now see clear feedback when fallback occurs:
  "Primary host unavailable, switched to: <host>"
- **Fallback failure reporting** - When no fallback is available, the error is now properly reported to the user

### Added

- `isHostRetryableError()` helper for detecting connection errors that should trigger fallback
- `switchToFallbackProvider()` method in Assistant for seamless host switching
- `SetHostPool()` method to wire HostPool to Assistant

## [2.0.0] - 2026-02-04

### Added

- **Multi-Host Support** - Connect to multiple Ollama/LLM hosts simultaneously with fallback logic
  - Configure named hosts in `~/.taracode/config.yaml` with the new `hosts:` section
  - Per-agent host assignment - run different agents on different hosts
  - Automatic fallback when a host becomes unavailable
  - Background health checking with configurable intervals
  - `/hosts` command to view status of all configured hosts
  - `/hosts check` to force health check on all hosts
  - `/hosts reconnect` to reconnect to unhealthy hosts
  - Priority-based host selection for optimal load distribution
- **Model Switching with Host Awareness** - `/model` now shows models from all healthy hosts
  - Seamlessly switch between models on different hosts
  - Model preference persists across assistant recreation

### Changed

- Version bumps from 1.x to 2.x due to configuration schema changes
- Agent registry now supports initialization from the host pool
- TaskBridge supports both single-host and multi-host operation modes

### Fixed

- Thread-safety improvements in HostPool with proper mutex handling
- Race condition in health check cancellation

### Migration Notes

- **Backward compatible**: Existing `host:` configuration continues to work unchanged
- To enable multi-host, add a `hosts:` section to config (see config.example.yaml)
- The `/hosts` command is only available when multi-host mode is configured

## [1.0.3] - 2026-02-04

### Fixed

- Minor bug fixes and stability improvements

## [1.0.2] - 2026-02-04

### Fixed

- **Memories not immediately available after `/remember`** - System prompt now refreshes after saving a memory,
  making it immediately available to the AI without needing to restart the session

## [1.0.1] - 2026-02-04

### Fixed

- **Memory manager not initializing after `/init`** - `/remember` and `/memory` commands now work immediately after
  running `/init` in a fresh session
- **History manager not initializing after `/init`** - `/history` and `/undo` commands now work immediately after
  running `/init` in a fresh session

### Changed

- Applied `gofmt -s` formatting across 32 files for Go Report Card compliance (89.3% → 100%)

## [1.0.0] - 2026-01-31

### Changed

- **Open Source Release** - taracode is now open source under the MIT License
- **No Authentication Required** - removed login/logout, use immediately after installation
- **Security Mode for All** - security mode available to all users, no plan restrictions
- **Simplified Configuration** - removed backend API integration

### Removed

- Authentication system (login/logout commands)
- Usage tracking and quota enforcement
- Subscription tier restrictions
- code.tara.vision API integration

### Features (carried from v0.4.5)

- 58 built-in DevOps and security tools
- Multi-agent system with 7 specialized agents
- Screen monitoring (`/watch`)
- Autonomous task execution (`/task`)
- Project memory (`/remember`, `/memory`)
- MCP (Model Context Protocol) support
- Local LLM support (Ollama, vLLM, llama.cpp)
- Permission controls for tool execution
- Session persistence with naming and summaries
- File reference autocomplete with `.gitignore` support
- Security audit logging
- Operation history and undo
- Context budget display
- Edit preview mode with a diff display

## Pre-1.0 History

The project evolved through the following milestones before being open-sourced:

- **v0.4.5** - Screen monitoring (`/watch`), multi-agent system enhancements
- **v0.4.2** - Multi-agent system with 7 specialized agents
- **v0.3.30** - Persistent project memory (`/remember`, `/memory`)
- **v0.3.27** - Autonomous task execution (`/task`)
- **v0.3.24** - MCP (Model Context Protocol) support
- **v0.3.18** - Security mode with audit logging
- **v0.3.15** - Web search resilience, context budget display
- **v0.3.12** - File reference autocomplete, permissions system
- **v0.3.8** - Native OpenAI function calling, security tools

[Unreleased]: https://github.com/tara-vision/taracode/compare/v2.0.3...HEAD

[2.0.3]: https://github.com/tara-vision/taracode/compare/v2.0.2...v2.0.3

[2.0.2]: https://github.com/tara-vision/taracode/compare/v2.0.1...v2.0.2

[2.0.1]: https://github.com/tara-vision/taracode/compare/v2.0.0...v2.0.1

[2.0.0]: https://github.com/tara-vision/taracode/compare/v1.0.3...v2.0.0

[1.0.3]: https://github.com/tara-vision/taracode/compare/v1.0.2...v1.0.3

[1.0.2]: https://github.com/tara-vision/taracode/compare/v1.0.1...v1.0.2

[1.0.1]: https://github.com/tara-vision/taracode/compare/v1.0.0...v1.0.1

[1.0.0]: https://github.com/tara-vision/taracode/releases/tag/v1.0.0
