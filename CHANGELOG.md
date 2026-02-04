# Changelog

All notable changes to taracode will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

- Version bump from 1.x to 2.x due to configuration schema changes
- Agent registry now supports initialization from host pool
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

[Unreleased]: https://github.com/tara-vision/taracode/compare/v2.0.0...HEAD

[2.0.0]: https://github.com/tara-vision/taracode/compare/v1.0.3...v2.0.0

[1.0.3]: https://github.com/tara-vision/taracode/compare/v1.0.2...v1.0.3

[1.0.2]: https://github.com/tara-vision/taracode/compare/v1.0.1...v1.0.2

[1.0.1]: https://github.com/tara-vision/taracode/compare/v1.0.0...v1.0.1

[1.0.0]: https://github.com/tara-vision/taracode/releases/tag/v1.0.0
