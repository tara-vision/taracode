# Changelog

All notable changes to taracode will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [3.0.0-alpha.2] - 2026-09-23

Second pre-release of the v3 line (ROADMAP.md, Phase 2 "Tools and policy"). Breaking: the tool set,
the modes, the configuration layout and the permission store change; migration notes below.

### Added
- **Investigate and operate modes.** Investigate (the default) exposes read-only tools and never prompts;
  operate exposes every tool and routes each mutation through the policy: protected targets are hard denies
  (kubectl and helm run through `shell` carry their context and namespace too, also inside loops and
  subshells, and a command that touches every namespace, several contexts or namespaces, or one taracode
  cannot determine before it runs counts as touching the protected ones, as after a context switch, a
  `KUBECONFIG` assignment or a sourced file earlier on the line; kubectl or helm inside another program's
  string, `sh -c "kubectl ..."`, is not seen), deny patterns are refused, `kubectl apply`, `terraform apply`
  and `helm upgrade` dry-run first (a release with a `--post-renderer` is refused, since its dry run would run
  the renderer), then the remembered permission or a prompt decides. `/mode investigate|operate`, `--mode`.
  MCP tools are gated by the per-tool permission only: protected targets and deny patterns do not apply to
  them.
- **Policy files.** `.taracode/policy.yaml` merged over `~/.taracode/policy.yaml` (lists unioned, booleans
  stricter); a built-in policy applies when neither exists; `/init` writes a starter; `/policy show`;
  `taracode doctor` reports the policy status; a broken policy locks the session to investigate mode.
- **Sixteen tools with a per-call classifier** (read_file, list_files, search_files, write_file, edit_file,
  shell, git, kubectl, helm, terraform, docker, cloud, scan, web_search, web_fetch, get_datetime). Every call
  is classified read or mutate from its arguments: `git status` and `kubectl get` are reads, `git push` and
  `kubectl delete` are mutations, a shell pipeline is a read only when every command is on the read-only
  list with no file redirect. Schemas total under 8 KB.
- **Terraform plans as summaries.** `terraform plan` keeps the plan file for the session and returns adds,
  changes, destroys, replacements and a risk list; `terraform apply` applies that plan and refuses without one.
- **Redaction** of AWS, GCP (API keys and OAuth access tokens), GitHub, Slack (including app-level tokens),
  JWT, private-key (PEM, and base64 PEM as in a kubeconfig's `*-data` fields), URL-password and
  `password=`/`token=`/`key-data:` secrets, plus the values of `*KEY*`, `*TOKEN*`, `*SECRET*`, `*PASSWORD*`
  environment variables, in every tool output before the model, the session or the screen sees it; counted
  in `/stats`. Live shell output is redacted a line at a time as it streams, so a secret that spans lines
  (a PEM private key block) is redacted in the tool result only, not on screen.
- **Audit log.** Every mutate-classified call is appended to `.taracode/audit.jsonl` before it runs, with the
  decision and the rule; `/audit`, `/audit all`, `/audit export json`, `/audit clear`.
- **AGENTS.md** is read as project context next to TARACODE.md. **`--offline`** disables the web tools and the
  update check; it does not stop shell commands that reach the network (curl, `wget -O-`, dig, nslookup, host,
  ping), which still count as reads.
- CI: no Go file over 800 lines, per-package coverage floors (`scripts/coverage-gate.sh`), `codecov.yml`.

### Changed
- **Configuration v3:** `model` is a string, sampling moved to `generation:`, `security.default_severity` to
  `scan.default_severity`, new `mode` and `offline`, `context.max_tool_iterations` defaults to 20. A 2.x
  `model:` section is read with a one-line warning; `agents:` and `watch:` are ignored with a warning.
- `/init` no longer gates the REPL: an uninitialised directory runs in investigate mode with nothing saved;
  `/init` enables sessions, memory, history and operate mode.
- `.taracode/permissions.json` is version 3, one rule per tool for mutations; a 2.x file is ignored once.
- `web_fetch` refuses loopback, private, link-local and carrier-grade NAT addresses.
- Tool executors take a context; each tool has its own timeout (shell: `timeout` argument, max 600 s).
- The REPL is split into files with one command table; `/help` is generated from it.
- The loop package is `internal/agent`.

### Removed
- The seven-agent system and the orchestrator, `/agent`, `/watch`, `/task` and the task templates (runbooks
  return in Phase 4), security mode (`/mode security`, `/audit export html`), the JSON-in-content tool
  fallback and the four prompt variants, the permission categories, and 42 of the 58 tools (folded into
  shell, git, kubectl, helm, terraform, docker, cloud and scan).

### Migration from 2.x and 3.0.0-alpha.1
- `~/.taracode/config.yaml`: rename `model:` (section) to `generation:` and set `model: <name>`; rename
  `security.default_severity` to `scan.default_severity`; delete `agents:` and `watch:`.
- Delete `.taracode/permissions.json` (or let taracode ignore it) and answer the prompts again.
- Scripts that relied on `/task`, `/agent` or `/watch` have no replacement in this release.

## [3.0.0-alpha.1] - 2026-09-22

Phase 1 of the v3 native core (see [ROADMAP.md](ROADMAP.md)): taracode talks to Ollama over its native
API instead of an OpenAI-compatible shim, and gets a model registry to recommend and diagnose against.

### Added

- **Native Ollama client** - `internal/llm` talks to Ollama's `/api/chat` directly, with streaming,
  thinking, native tool calls and context-window control. vLLM and llama.cpp keep working through a
  `go-openai` adapter behind the same `llm.Client` interface; both implementations sit behind provider
  host failover.
- **Context window control** - `context.window` (`auto`, or a token count) requests 32,768 tokens by
  default, or the model's native maximum when that is smaller (never more, to keep the KV cache affordable
  on 16 GB and 32 GB machines); set a token count instead to go higher on a model that supports it. Warns
  when the result is too small for tool-heavy sessions.
- **Thinking mode** - `think` (`auto`, `off`, `on`, `low`, `medium`, `high`) is sent with every request;
  change it mid-session with the new `/think` command.
- **`taracode doctor`** (and the REPL's `/doctor`) - diagnoses the LLM server, the installed models and
  their capabilities, the host's RAM tier, the registry's recommended model for it, and the external
  CLIs taracode's tools shell out to. A first run with no model configured prints the same
  recommendation.
- **Model registry** (`internal/models`) - an embedded, curated list of recommended Ollama models by RAM
  tier (16 GB, 32 GB, 48 GB and up), with download size, context length, capabilities and the first
  Ollama release that ships each model's tool-call parser.
- Scripted fake Ollama server (`internal/llm/ollamatest`) covering `/api/chat`, `/api/tags`, `/api/show`,
  `/api/ps`, `/api/version` and `/api/generate`, used across the new native-client test suite.
- Host RAM detection (`internal/models.HostRAMGB`) backing the registry's tier recommendation.

### Changed

- **Breaking: the tools-capability gate** - on Ollama, `assistant.New` and `SwitchModel` now refuse a
  model whose `/api/show` capabilities do not include `tools`, instead of falling back to
  JSON-in-content tool calls as v2 did; run `taracode doctor` to see which installed models qualify.
  The JSON-in-content fallback now only serves vLLM and llama.cpp.
- The assistant package was split from one large file into focused files by responsibility (loop,
  prompt, planning, tool calls, compaction, context, session, security, project init, context window).
- Agent and orchestrator model defaults (`internal/agent`, `internal/orchestrator`, `cmd/hosts_cmd.go`,
  `cmd/repl.go`, `internal/ui`) now come from the model registry instead of hard-coded literals; a
  repo-wide test guards against new model-name literals outside the registry and tests.
- `CompactConversation` takes an `llm.Client` instead of a raw `*openai.Client`.
- The `/api/ps` server-context check moved into the `llm` client layer; `ollama_ps.go` is removed.
- Streamed answers are still buffered behind the spinner and rendered once with glamour markdown when
  the reply completes, as in v2; the model's reasoning is now shown live, dimmed, before the answer.
- The Go Report Card badge in README.md is replaced by a CI status badge; goreportcard.com was sunset.

### Fixed

- A tool call blocked by a permission, security-audit or edit-preview gate no longer prints a success
  status line underneath the refusal.
- Declining an edit preview now sends the cancellation back to the model as the tool result, instead of
  the v2 dead store that dropped it.

### Removed

- Dead retry, detection, status and duplicate getter code from the assistant package.
- `Assistant.GetUsage()`.
- `ModelOptions.ApplyTo()`.

## [2.1.0] - 2026-09-21

### Added

- **Ollama context window check** - After the first reply of a session taracode reads the context window
  Ollama loaded the model with and warns once when it is smaller than the system prompt plus tool schemas
  (the Ollama default of 4,096 tokens on machines under 24 GB of GPU memory cuts the tool definitions off)
  or smaller than `max_context_tokens`. `/context` shows the server window.
- **Linux arm64** binaries, plus deb and rpm packages.
- **Signed releases** - `checksums.txt` with a keyless cosign signature and SLSA provenance on every release.
- **Homebrew cask** - `brew install --cask tara-vision/taracode/taracode` (goreleaser stopped generating formulas).
- `ROADMAP.md` describing v3.

### Changed

- Recommended models are now `gemma4:12b` (16 GB), `qwen3.8:27b` (32 GB) and `qwen3.6:35b` (48 GB and up);
  agent defaults follow. gemma3 is no longer referenced.
- Go toolchain 1.27.1 (language floor 1.26); CI runs golangci-lint and govulncheck; dependabot enabled.
- The installer verifies the downloaded binary against `checksums.txt` and refuses releases without one.
- Release pipeline moved to goreleaser.

### Fixed

- README pointed Homebrew users at a tap that does not exist (`tara-vision/tap`).

### Security

- Updated `golang.org/x/net`, `golang.org/x/text`, `goldmark` and the Go standard library; `govulncheck` reports
  no reachable vulnerabilities.

## [2.0.4] - 2026-02-07

### Added

- **Global model generation options** - Configure `temperature`, `top_p`, and `num_predict` in `config.yaml`
  under a new `model:` section. These apply to the main chat loop and serve as defaults for agents.
  - `model.temperature` - Sampling randomness, 0.0-2.0 (default: 0.7)
  - `model.top_p` - Nucleus sampling threshold, 0.0-1.0 (default: 0.9)
  - `model.num_predict` - Max tokens per response, 0 = model default (default: 0)
- **Agent TopP and NumPredict** - Agents now support `top_p` and `num_predict` settings in `agents.yaml`,
  inheriting global model options as defaults when not explicitly set
- **`/stats` model options** - Session statistics now display configured model generation options
- **`/agent config` TopP/NumPredict** - Agent config display now shows `top_p` and `num_predict` settings

### Fixed

- **Tool call format resilience** - Models outputting `tool_code`, `tool_name`, `function`, or `action`
  as the tool name key (instead of `tool`) are now recognized correctly
- **Empty response recovery** - When the LLM returns an empty response after tool execution, a nudge
  mechanism re-prompts with the original question for a direct answer
- **WebSearch parameter flexibility** - `web_search` now accepts `search_query`, `q`, and `search`
  as alternative parameter names for the query
- **get_datetime tool example** - Added JSON example for `get_datetime` in the system prompt to
  prevent models from hallucinating non-existent tool names like `datetime_tools`
- **datetime tool alias** - `datetime` is now recognized as an alias for `get_datetime` in both
  tool registry and permission system (models sometimes omit the `get_` prefix)

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
- Removed private lab host references from all public documentation and examples, replaced with generic
  `gpu-server:11434` or `localhost:11434`

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

[Unreleased]: https://github.com/tara-vision/taracode/compare/v3.0.0-alpha.2...HEAD

[3.0.0-alpha.2]: https://github.com/tara-vision/taracode/compare/v3.0.0-alpha.1...v3.0.0-alpha.2

[3.0.0-alpha.1]: https://github.com/tara-vision/taracode/compare/v2.1.0...v3.0.0-alpha.1

[2.1.0]: https://github.com/tara-vision/taracode/compare/v2.0.4...v2.1.0

[2.0.4]: https://github.com/tara-vision/taracode/compare/v2.0.3...v2.0.4

[2.0.3]: https://github.com/tara-vision/taracode/compare/v2.0.2...v2.0.3

[2.0.2]: https://github.com/tara-vision/taracode/compare/v2.0.1...v2.0.2

[2.0.1]: https://github.com/tara-vision/taracode/compare/v2.0.0...v2.0.1

[2.0.0]: https://github.com/tara-vision/taracode/compare/v1.0.3...v2.0.0

[1.0.3]: https://github.com/tara-vision/taracode/compare/v1.0.2...v1.0.3

[1.0.2]: https://github.com/tara-vision/taracode/compare/v1.0.1...v1.0.2

[1.0.1]: https://github.com/tara-vision/taracode/compare/v1.0.0...v1.0.1

[1.0.0]: https://github.com/tara-vision/taracode/releases/tag/v1.0.0
