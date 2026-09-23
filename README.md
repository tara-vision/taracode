# taracode

<p align="center">
  <strong>DevOps & Cloud AI Assistant</strong><br>
  Expert guidance for Kubernetes, Terraform, Docker, and multi-cloud deployments.<br>
  Runs locally with Ollama for complete privacy.
</p>

<p align="center">
  <a href="https://github.com/tara-vision/taracode/releases"><img src="https://img.shields.io/github/v/release/tara-vision/taracode?style=for-the-badge&logo=github&color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License: MIT"></a>
  <a href="https://github.com/sponsors/tara-vision"><img src="https://img.shields.io/badge/Sponsor-%E2%9D%A4-ea4aaa?style=for-the-badge&logo=githubsponsors&logoColor=white" alt="Sponsor tara-vision"></a>
  <a href="https://github.com/tara-vision/taracode/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/tara-vision/taracode/ci.yml?branch=main&style=for-the-badge&logo=github&label=CI" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version"></a>
</p>

<p align="center">
  <a href="https://github.com/tara-vision/taracode/stargazers"><img src="https://img.shields.io/github/stars/tara-vision/taracode?style=for-the-badge&logo=github&color=yellow" alt="Stars"></a>
  <a href="https://github.com/tara-vision/taracode/network/members"><img src="https://img.shields.io/github/forks/tara-vision/taracode?style=for-the-badge&logo=github&color=orange" alt="Forks"></a>
  <a href="https://github.com/tara-vision/taracode/issues"><img src="https://img.shields.io/github/issues/tara-vision/taracode?style=for-the-badge&logo=github&color=red" alt="Issues"></a>
</p>

<p align="center">
  <a href="#quick-start"><img src="https://img.shields.io/badge/Quick_Start-blue?style=flat-square" alt="Quick Start"></a>
  <a href="#features"><img src="https://img.shields.io/badge/Features-purple?style=flat-square" alt="Features"></a>
  <a href="#commands"><img src="https://img.shields.io/badge/Commands-teal?style=flat-square" alt="Commands"></a>
  <a href="https://code.tara.vision/documentation"><img src="https://img.shields.io/badge/Documentation-green?style=flat-square" alt="Documentation"></a>
  <a href="CONTRIBUTING.md"><img src="https://img.shields.io/badge/Contributing-orange?style=flat-square" alt="Contributing"></a>
</p>

---

## Why taracode?

- **Investigate-first** - Read-only by default, and never prompts you in that mode
- **Policy-gated operate mode** - Mutations pass through protected targets, deny patterns and required
  dry runs before a remembered permission or a prompt decides
- **Sixteen classified tools** - Every call is classified read or mutate from its arguments, not its name
- **Redaction and an audit log** - Secrets are stripped from tool output before anything sees it; every
  mutation is recorded
- **Privacy-first** - Runs fully local with Ollama, your data never leaves your machine
- **No Account Required** - Open source, just install and use

## Quick Start

### 1. Install Ollama

```bash
# macOS / Linux
brew install ollama

# Or download from https://ollama.ai
```

### 2. Pull a Model

```bash
ollama pull gemma4:12b     # 16 GB machines
ollama pull qwen3.8:27b    # 32 GB machines (needs Ollama 0.32.12 or newer)
ollama pull qwen3.6:35b    # 48 GB and up
```

Any model that Ollama tags with the `tools` capability works. On Ollama, taracode refuses a model without that
capability at startup or on `/model` instead of falling back to JSON-in-content tool calls (run `taracode doctor`
to see which installed models qualify). taracode requests its context window itself on every turn (`context.window`,
default auto = 32,768 tokens, or the model's native maximum when that is smaller) instead of relying on the
server's default; `OLLAMA_CONTEXT_LENGTH` only matters for servers taracode does not control, such as vLLM and
llama.cpp.

### 3. Install taracode

**Quick install (recommended):**

```bash
curl -fsSL https://code.tara.vision/install.sh | bash
```

**Homebrew (macOS / Linux):**

```bash
brew install --cask tara-vision/taracode/taracode
```

Upgrading from a version installed as a formula? Run brew uninstall taracode once, then the command above.

**Go install:**

```bash
go install github.com/tara-vision/taracode@latest
```

**Manual download:**

Download binaries from [GitHub Releases](https://github.com/tara-vision/taracode/releases). Every release ships checksums, a cosign signature and SLSA provenance; see [SECURITY.md](SECURITY.md#verifying-downloads) to verify a download.

### 4. Run

```bash
cd your-project
taracode
```

taracode starts in investigate mode right away, no `/init` needed: read-only tools, nothing saved. Run
`/init` when you want sessions, memory, history and operate mode (it also writes a starter
`.taracode/policy.yaml`). Start asking questions about your infrastructure.

## Context window and thinking

Three `config.yaml` keys control how taracode talks to the model:

```yaml
context:
  window: auto    # "auto" requests 32768 tokens, or less on a smaller model; set a token count to go higher
think: auto        # auto, off, on, low, medium, or high
keep_alive: ""     # how long Ollama keeps the model loaded; "" = server default, "-1" = keep loaded
```

`context.window: auto` (the default) requests 32,768 tokens, or the model's native maximum when that is
smaller; it never asks for more than 32,768 tokens on its own, which keeps the KV cache affordable on 16 GB
and 32 GB machines. Set a number instead to request an explicit window, clamped to the model's native
maximum, on a model that supports going higher. A non-numeric value falls back to auto with a warning. A
session warns when the resulting window is below 16,384 tokens, since tool-heavy sessions compact early at
that size.

`think` sets the reasoning mode sent with requests. Change it without restarting taracode with `/think`
(`/think` alone shows the current mode, `/think low` changes it).

Run `taracode doctor` (or `/doctor` inside a session) to check the server, the installed models and their
capabilities, your machine's RAM tier and the registry's recommended model for it, and the external CLIs
taracode's tools shell out to.

`context.window` and `keep_alive` are controlled on the native Ollama client; vLLM and llama.cpp keep the
OpenAI-compatible path, where only `think low|medium|high` reaches the server (as `reasoning_effort`).

## Features

### Modes and policy

taracode starts in **investigate** mode: only tools with a read form are exposed, and nothing ever prompts
you. **operate** mode exposes every tool; each mutation goes through the policy, in order:

1. **Protected targets** - kube contexts, namespaces, cloud accounts, paths and hosts named in the policy
   are a hard deny, with the reason printed. A mutation of every namespace (`-A`) counts as touching the
   protected ones. Protected paths cover the file `write_file` or `edit_file` changes, the directory the
   `terraform` tool runs in, and in a `shell` command the targets of its redirects and the files it hands to
   a file-writing program (`tee`, `sed -i`, `cp`, `mv`, `rm`, `touch`, `chmod`, `ln`, `dd`, `sort -o`,
   `curl -o`, ...), also after a literal `cd`. A path a command builds at run time (a variable, a command
   substitution) is not seen, which is why the built-in deny patterns also refuse any mutation that names
   `.taracode/policy.yaml`.
2. **Deny patterns** - command globs that are refused outright.
3. **Required dry runs** - `kubectl apply` shows a server-side diff first, `terraform apply` requires a plan
   produced in this session and shows its summary, `helm upgrade` runs `--dry-run` first.
4. **Permission** - the remembered allow/ask/deny rule for the tool, or a prompt.

```bash
> /mode investigate|operate   # show or switch mode (or --mode at startup)
```

A call to a tool the session does not offer (one the model repeats from a resumed session, or makes up) is
refused. `offline` hides the two web tools, but it does not reach into `shell`: curl, `wget -O-`, dig,
nslookup, host and ping still count as reads there.

The policy comes from `.taracode/policy.yaml` merged over `~/.taracode/policy.yaml` (lists unioned, booleans
take the stricter value); with neither file, a built-in policy identical to the one below applies. `/init`
writes this starter:

```yaml
# taracode policy (see: taracode doctor, /policy show).
# The model never sees this file. .taracode/policy.yaml merges over ~/.taracode/policy.yaml:
# lists are unioned and booleans take the stricter value. With no policy file at all, taracode
# uses a built-in policy identical to this one. Patterns are globs (* ? and ** in paths).
version: 1
mode: investigate               # the mode a session starts in: investigate or operate
protected:                      # never mutated in operate mode (hard deny, printed reason)
  kube_contexts: ["*prod*", "*production*"]
  kube_namespaces: ["kube-system"]
  cloud_accounts: []            # AWS account ids, Azure subscription ids, GCP project ids, or *globs*
  paths: ["**/*.tfstate", ".git/**"]
  hosts: []
deny:                           # refused outright; the last pattern keeps the policy files safe
  commands: ["rm -rf /*", "kubectl delete namespace *", "terraform destroy*", "*.taracode/policy.yaml*"]
require_dry_run:                # shown before the permission prompt
  kubectl_apply: true           # kubectl diff first
  terraform_apply: true         # a plan from this session, its summary first
  helm_upgrade: true            # helm --dry-run first (upgrade and install)
redact:
  enabled: true                 # secrets in tool output become [redacted:<kind>]
  extra_patterns: []            # additional Go regular expressions
```

`/policy show` prints the effective policy and where it came from. `/permissions` manages the remembered
per-tool rules (`/permissions allow|deny|ask <tool|all>`, `/permissions reset`). Every mutation, allowed or
denied, is appended to `.taracode/audit.jsonl` before it runs; `/audit`, `/audit all` and `/audit export json`
read it.

Redaction runs on every tool result before the model, the session or the history sees it. The live output of
a `shell` command is redacted a line at a time as it reaches the screen, so a secret that spans lines, such
as a PEM private key block, is redacted in the tool result but not in the live view
(`no_stream_commands: true` in `config.yaml` turns the live view off).

### Tools

Sixteen tools replace the old 58; every call is classified read or mutate from its arguments, not from the
tool's name. Investigate mode exposes the tools that have a read form (fourteen, twelve when `offline` is set).

| Tool | Arguments (summary) | Read when | Mutate when |
|---|---|---|---|
| `read_file` | path, start_line, end_line | always | never |
| `list_files` | path, glob, recursive, max | always | never |
| `search_files` | pattern, path, glob, max | always | never |
| `write_file` | path, content | never | always |
| `edit_file` | path, old, new, preview | never | always |
| `shell` | command, timeout | command matches the read-only allowlist (cat, ls, grep, find, ps, df, du, curl GET, dig, nslookup, jq, git read verbs, kubectl read verbs, terraform read verbs, ...) | otherwise |
| `git` | args | status, diff, log, show, branch (list), blame | add, commit, stash, checkout, reset, push, merge, rebase |
| `kubectl` | verb, resource, name, namespace, context, args, output | get, describe, logs, events, top, explain, api-resources, version, diff, dry-run | apply, delete, patch, edit, scale, rollout, exec, cp, drain, cordon |
| `helm` | args | list, status, get, history, show, template, lint, diff | install, upgrade, rollback, uninstall |
| `terraform` | command, dir, args | init (with -backend=false), validate, fmt -check, plan (always -json, post-processed), show, state list, output, graph | apply, destroy, import, taint, state mv/rm/push, workspace delete |
| `docker` | args | ps, images, logs, inspect, stats, compose ps/config/logs | build, run, rm, rmi, exec, compose up/down/restart, push |
| `cloud` | provider (aws, az, gcloud), args | verbs describe, get, list, ls, show | otherwise |
| `scan` | scanner (trivy, gitleaks, tfsec, kubesec, dependency), target, severity | always | never |
| `web_search` | query, max | always (external, disabled by `offline`) | never |
| `web_fetch` | url | always (external, disabled by `offline`) | never |
| `get_datetime` | format, timezone | always | never |

MCP tools join the same registry: a server that annotates a tool `readOnlyHint: true` gets a read form;
every other MCP tool is a mutation and stays hidden in investigate mode.

### Project Memory

Remember project-specific knowledge across sessions:

```bash
> /remember We use PostgreSQL for production databases
> /remember Always run tests before pushing #workflow
> /memory search database
```

## Commands

| Command | Description |
|---|---|
| `/init` | Initialize the project (creates TARACODE.md and .taracode/) |
| `/reload` | Reload project context from TARACODE.md |
| `/status` | Show project and session status |
| `/session [new [name]\|load <id>\|delete <id>\|rename <id> <name>]` | Show or manage the current session |
| `/sessions` | List all sessions |
| `/clear` | Clear the conversation (new session) |
| `/model` | Switch between available models |
| `/hosts [check\|reconnect]` | Multi-host status and health |
| `/think [auto\|off\|on\|low\|medium\|high]` | Show or set the reasoning mode |
| `/mode [investigate\|operate]` | Show or switch the operating mode |
| `/permissions [allow\|deny\|ask <tool\|all>\|reset]` | Remembered answers for mutations |
| `/audit [all\|export json\|clear]` | Mutations recorded in this project |
| `/policy show` | Effective policy and where it comes from |
| `/plan` | Show the active plan |
| `/context` | Context window budget breakdown |
| `/compact` | Force conversation compaction |
| `/stats` | Session statistics |
| `/usage` | Token usage for this session |
| `/history [n\|all]` | File operation history |
| `/undo [n\|--dry-run]` | Undo file modifications |
| `/diff [export]` | Show or export session changes |
| `/remember <text> [#tag]` | Save a memory about this project |
| `/memory [search <q>\|delete <id>\|export\|import <file>\|stats\|cleanup\|clear]` | Project memories |
| `/mcp [connect\|disconnect <name>\|tools]` | MCP servers and their tools |
| `/tools` | List available tools |
| `/upgrade [check\|now\|skip\|changelog\|status]` | Check for and install updates |
| `/doctor` | Diagnose the LLM server and tools |
| `/help` | Show this help |

## Configuration

Create `~/.taracode/config.yaml`:

```yaml
# Single host (simple setup)
host: http://localhost:11434

# Multi-host setup - for multiple Ollama servers
hosts:
  primary:
    url: http://gpu-server:11434
    models: [ qwen3.8:27b, gemma4:12b ]
    priority: 1
  local:
    url: http://localhost:11434
    fallback: primary      # Use primary if local is down
    priority: 2
default_host: primary

# Generation options for the main chat
generation:
  temperature: 0.7     # Sampling randomness (0.0-2.0)
  top_p: 0.9           # Nucleus sampling threshold (0.0-1.0)
  num_predict: 0       # Max tokens per response (0 = model default)

# Starting mode: investigate or operate (unset starts in investigate, or the mode a policy file names)
mode: investigate

# Security scanning
scan:
  default_severity: ""   # e.g. "HIGH,CRITICAL"

# Search
search:
  primary: duckduckgo
  fallback: searxng
  brave_api_key: ""    # Optional: Brave Search API

# Memory
memory:
  enabled: true
  auto_capture: true
```

See [config.example.yaml](config.example.yaml) for all options.

## Supported LLM Backends

| Backend       | Setup                 | Notes                      |
|---------------|-----------------------|----------------------------|
| **Ollama**    | `brew install ollama` | Recommended, easiest setup |
| **vLLM**      | Self-hosted           | For production deployments |
| **llama.cpp** | Self-hosted           | Lightweight option         |

## Roadmap

Phase 2 of the v3 plan (investigate and operate modes, the sixteen classified tools, the policy engine,
redaction and the audit log) shipped in 3.0.0-alpha.2. Evals are next: a suite of offline DevOps tasks with
recorded fixtures, `taracode eval`, and a published scoreboard by RAM tier. See [ROADMAP.md](ROADMAP.md).

## Development

```bash
make deps           # Install dependencies
make build          # Build binary
make test           # Run tests
make coverage-gate   # Check per-package coverage floors
make install         # Install to /usr/local/bin
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md)
and [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

For security issues, please see our [Security Policy](SECURITY.md).

## Sponsoring

taracode is free, MIT-licensed and built without telemetry or a cloud service. If it saves you time, you can
[sponsor Tara Vision on GitHub](https://github.com/sponsors/tara-vision) to keep the releases, the model registry and the
eval scoreboard coming.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  Built with ❤️ by <a href="https://tara.vision">Tara Vision</a> · Created by <a href="https://github.com/dayanstef">Dejan Stefanoski</a>
</p>
