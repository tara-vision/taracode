# Changelog

All notable changes to taracode will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-01-31

### Changed

- **Open Source Release** - taracode is now open source under the MIT License
- **No Authentication Required** - removed login/logout, use immediately after install
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
- Edit preview mode with diff display

## Pre-1.0 History

The project evolved from v0.1.0 through v0.4.5 as a commercial product before
being open-sourced in v1.0.0.
