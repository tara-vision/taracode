# Roadmap

taracode is becoming the local-first DevOps operator: a terminal agent that investigates infrastructure by
default, changes it only through an explicit policy, runs on Ollama with any modern open model, and publishes
a reproducible scoreboard of which local models can actually do DevOps work.

## v2.1 (2026-09-21, the previous stable line)

Supported Go toolchain, clean vulnerability scan, lint and scan gates in CI, signed four-platform releases with
checksums and provenance, deb and rpm packages, a Homebrew cask, an installer that verifies checksums, 2026 model
recommendations, and a warning when Ollama's context window is smaller than the tool budget.

## v3.0 (stable since 3.0.0, 2026-09-25)

1. **Native Ollama core** (shipped in 3.0.0-alpha.1) - `/api/chat` client with context-window control,
   thinking levels, structured output, capability detection; a model registry and `taracode doctor` that
   recommends a model for your RAM.
2. **Sixteen tools and a policy engine** (shipped in 3.0.0-alpha.2) - `investigate` mode is read-only and
   never prompts; `operate` mode gates every mutation through a project policy (protected contexts,
   namespaces, accounts and paths, deny patterns, dry-run before apply). Tool output is secret-redacted
   before the model sees it. `/task` and the seven-agent orchestrator were removed here rather than kept
   until Phase 4 (ruling R1): their templates called v2 tool names and the code under them was untested, so
   Phase 4 builds the runbook engine fresh instead of carrying it forward. `search.ollama_api_key` is left
   out of alpha.2 as a non-breaking addition for later (ruling R12).
3. **Evals** (shipped in 3.0.0-beta.1) - a corpus of 33 offline DevOps tasks (Kubernetes, Helm, Terraform,
   Docker, secrets, cloud and refusal cases) replayed from recorded and hand-authored fixtures through
   `taracode eval run|record|report|lint`, and a first scoreboard across the whole model registry, published
   in `docs/evals/scoreboard.md` and on code.tara.vision, refreshed every release. Carries two Phase 2 safety
   follow-ups forward: the shell-classifier differential harness (`make classify-diff`) and the MCP trust
   switch (`policy.mcp`).
4. **Runbooks, MCP server and skills pack** (3.2 and 3.3) - ten built-in runbooks on the existing checkpoint engine,
   `taracode mcp serve` for Claude Code, OpenCode, Codex and Pi users, and skills in the Agent Skills format.
5. **Removed in 3.0** - the seven-agent orchestration system, `/watch`, the security mode and the JSON-in-content
   tool fallback. Their jobs move to modes, policy and runbooks.

## v3.1 (2026-09-25, the stable line)

Fifteen tools and one host. `get_datetime` retired: `date` is on the shell tool's read-only allowlist and every
system prompt carries today's date. The v2 multi-host pool, its `hosts:` config and `/hosts` are gone: taracode
talks to the one Ollama host in `host:`. 3.1.1 taught the cloud tool the three CLIs' command shapes; 3.1.2
re-ran the scoreboard on the line. 3.1.2 stays the stable release while the line is used and tested in depth.
Runbooks, the MCP server and the skills pack move to 3.2 and 3.3.

## Later

A terminal UI, Windows builds, and whatever the scoreboard and the issue tracker say matters most.

Discuss the roadmap in GitHub Discussions.
