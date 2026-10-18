# taracode

<p align="center">
  <strong>DevOps & Cloud SI Assistant</strong><br>
  Expert guidance for Kubernetes, Terraform, Docker, and multi-cloud deployments.<br>
  Runs locally on taraengine for complete privacy.
</p>

<p align="center">
  <a href="https://github.com/tara-vision/taracode/releases"><img src="https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fcode.tara.vision%2Fapi%2Fversion&query=%24.version&label=release&style=for-the-badge&logo=github&color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License: MIT"></a>
  <a href="https://github.com/sponsors/tara-vision"><img src="https://img.shields.io/badge/Sponsor-%E2%9D%A4-ea4aaa?style=for-the-badge&logo=githubsponsors&logoColor=white" alt="Sponsor tara-vision"></a>
  <a href="https://github.com/tara-vision/taracode/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/tara-vision/taracode/ci.yml?branch=main&style=for-the-badge&logo=github&label=CI" alt="CI"></a>
  <a href="https://app.codecov.io/github/tara-vision/taracode"><img src="https://img.shields.io/codecov/c/github/tara-vision/taracode?branch=main&style=for-the-badge&logo=codecov&logoColor=white&label=coverage" alt="Coverage"></a>
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
  <a href="#the-scoreboard"><img src="https://img.shields.io/badge/Scoreboard-blueviolet?style=flat-square" alt="Scoreboard"></a>
  <a href="https://code.tara.vision/documentation"><img src="https://img.shields.io/badge/Documentation-green?style=flat-square" alt="Documentation"></a>
  <a href="CONTRIBUTING.md"><img src="https://img.shields.io/badge/Contributing-orange?style=flat-square" alt="Contributing"></a>
</p>

---

## Why taracode?

- **Investigate-first** - Read-only by default, and never prompts you in that mode
- **Policy-gated operate mode** - Mutations pass through protected targets, deny patterns and required
  dry runs before a remembered permission or a prompt decides
- **Fifteen classified tools** - Every call is classified read or mutate from its arguments, not its name
- **Redaction and an audit log** - Secrets are stripped from tool output before anything sees it; every
  mutation is recorded
- **A public scoreboard** - Which local models can actually do DevOps work, measured on taracode's own
  loop, with speed and memory per model
- **Privacy-first** - Runs fully local on taraengine, your data never leaves your machine
- **No Account Required** - Open source, just install and use

## The scoreboard

Which local model can actually do DevOps work? The eight models of taraengine's catalog, 33 offline tasks
each, on two machines, measured on taracode's own loop with taracode 3.3.1 on taraengine 0.3.1. The top five
on each:

On one NVIDIA RTX 5090 (32 GB):

| #   | Model           | Pass rate | Tokens/s | VRAM     |
|-----|-----------------|-----------|----------|----------|
| 1   | `qwen3.8:27b`   | 97%       | 126      | 23.2 GiB |
| 2   | `glm-4.7-flash` | 91%       | 209      | 18.4 GiB |
| 3   | `gemma4:31b`    | 91%       | 164      | 24.8 GiB |
| 4   | `gemma4:12b`    | 88%       | 308      | 10.0 GiB |
| 5   | `qwen3.6:27b`   | 85%       | 127      | 23.3 GiB |

On one Apple M4 Max (48 GB):

| #   | Model           | Pass rate | Tokens/s | Memory     |
|-----|-----------------|-----------|----------|------------|
| 1   | `qwen3.8:27b`   | 94%       | 39       | ~23.4 GiB  |
| 2   | `glm-4.7-flash` | 91%       | 48       | ~17.8 GiB  |
| 3   | `gemma4:31b`    | 88%       | 42       | ~23.9 GiB  |
| 4   | `gemma4:12b`    | 88%       | 83       | ~9.6 GiB   |
| 5   | `qwen3.6:35b`   | 88%       | 124      | ~22.0 GiB  |

One run per row, ranked by pass rate, then mean score, then tokens per second. A model moves by up to four tasks between two runs
of the same board (one task is about 3 points of pass rate; the 2026-10-01 board was run twice to measure
it), so rows within four tasks of each other are a tie: all five on each machine. VRAM is the GPU memory the
engine measured with the model loaded at a 32,768-token context window, on the 32 GB card with nothing else
on it; on the Mac the engine estimates the memory (~). The full boards, with speed, memory and the method,
are on [code.tara.vision/evals](https://code.tara.vision/evals) and in
[docs/evals/scoreboard.md](docs/evals/scoreboard.md).

## Quick Start

### 1. Install taraengine

On an Apple Silicon Mac:

```bash
brew install --cask tara-vision/taracode/taraengine
```

On Linux:

```bash
curl -fsSL https://engine.tara.vision/install.sh | bash
```

Both install the engine and its service, which fetches the pinned llama.cpp build for the machine on its own.
The packages, the verified download by hand and everything else about the engine are on
[engine.tara.vision](https://engine.tara.vision). On an Intel Mac, point taracode at another OpenAI-compatible
server with `--host`.

### 2. Pull a Model

```bash
taraengine pull gemma4:12b     # 16 GB of RAM
taraengine pull glm-4.7-flash  # 32 GB of RAM
taraengine pull gemma4:31b     # 48 GB of RAM and up
```

`taracode doctor` names the catalog's model for your machine (`gemma4:e4b` on a smaller one). Every catalog
model calls tools natively; a GGUF file you import is accepted once the engine has loaded it and knows its
capabilities. taracode sets the context window when the model loads (`context.window`, default auto = 32,768
tokens, or the model's native maximum when that is smaller), so there is nothing to set on the server.

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

taracode finds the engine at `http://127.0.0.1:8272` without configuration, and starts in investigate mode
right away, no `/init` needed: read-only tools, nothing saved. Run `/init` when you want sessions, memory,
history and operate mode (it also writes a starter `.taracode/policy.yaml`). Start asking questions about
your infrastructure.

## Context window and thinking

Two `config.yaml` keys control how taracode talks to the model:

```yaml
context:
  window: auto    # "auto" loads the model at 32768 tokens, or less on a smaller model; set a token count to go higher
think: auto        # auto, off, on, low, medium, or high
```

`context.window: auto` (the default) loads the model at 32,768 tokens, or the model's native maximum when
that is smaller; it never asks for more than 32,768 tokens on its own, which keeps the KV cache affordable on
16 GB and 32 GB machines. Set a number instead to load the model at an explicit window, clamped to the
model's native maximum, on a model that supports going higher. A non-numeric value falls back to auto with a
warning. A session warns when the resulting window is below 16,384 tokens, since tool-heavy sessions compact
early at that size.

`think` sets the reasoning mode sent with requests. Change it without restarting taracode with `/think`
(`/think` alone shows the current mode, `/think low` changes it).

Run `taracode doctor` (or `/doctor` inside a session) to check the engine, the pulled models and their
capabilities, your machine's RAM tier and the engine's recommended model for it, and the external CLIs
taracode's tools shell out to.

`context.window` is set when the model loads on taraengine; vLLM and llama.cpp run on the OpenAI-compatible
path, where the server's own window applies and only `think low|medium|high` reaches the server (as
`reasoning_effort`).

## Features

### Modes and policy

taracode starts in **investigate** mode: only tools with a read form are exposed, and nothing ever prompts
you. **operate** mode exposes every tool; each mutation goes through the policy, in order:

1. **Protected targets** - kube contexts, namespaces, cloud accounts, paths and hosts named in the policy are
   a hard deny, with the reason printed. A `kubectl` or `helm` mutation run through `shell` carries its
   context and namespace like the dedicated tools (the current ones of the kubeconfig it names when it names
   none, `HELM_NAMESPACE` and `HELM_KUBECONTEXT` for helm), also inside loops, conditionals and subshells. A
   mutation of every namespace (`-A`), of several contexts or namespaces, or of one taracode cannot determine
   before it runs counts as touching the protected ones: a value the shell computes, an option repeated with
   two values, `sudo` or `xargs` in front, a kubeconfig that is not a small regular file, and a context
   switched earlier on the line (`kubectl config use-context`, `kubectx`, `kubens`, a `KUBECONFIG` assignment,
   a sourced file that can set `KUBECONFIG`, a script). A line taracode cannot see into (a shell's `-c`
   string, `eval`, an interpreter's code such as `python3 -c`, `xargs`, a script or a program it has no rule
   for) that names `kubectl` or `helm` anywhere, inside a string or a heredoc too (`sh -c "kubectl ..."`, with
   the string's quotes removed), counts as touching every context and namespace; a script that runs kubectl
   without naming it is not seen.
   A namespace a `kubectl` mutation changes as an object (`kubectl delete ns kube-system`,
   `kubectl label ns/kube-system ...`) is the namespace it touches, and on a `shell` line such a command with
   a glob or a brace list in any word, even a quoted JSON value (`kubectl patch ns shop -p '{"a":1,"b":2}'`),
   counts as touching every namespace; the `kubectl` tool, which runs no shell, reads the same words
   literally.
   Cloud account and host patterns also match a value glued to a flag or an assignment (`--profile=prod`,
   `AWS_PROFILE=prod`), a host after a user name (`admin@host`), the host of a URL, the words of the line once
   the shell removes its quotes, and a host name without its trailing dot.
   Protected paths cover the file `write_file` or `edit_file` changes, the directory the `terraform` tool runs
   in, and in a `shell` command the targets of its redirects and the files it hands to a file-writing program
   (`tee`, `sed -i`, `cp`, `mv`, `rm`, `touch`, `chmod`, `ln`, `rsync`, `dd`, `sort -o`, `curl -o`, ...), with
   its braces and globs expanded as the shell expands them (a `*` does not match `.git`), also after a literal
   `cd` (with `-P`, or to a glob), `env -C`, `sudo -D` or `git -C`, and inside the strings a shell's `-c`,
   `eval`, `su -c`, `watch`, `$(...)` or backticks run and the script a shell reads from a heredoc, eight levels
   deep. `cp`, `mv`, `install` and `ln` are checked by their destination's name and, when it is a directory,
   where each source lands in it; `ln`, `cp -l` and `cp -s` also by the paths they link to; and a copy that
   merges into its destination (`-T`, a source with a trailing slash, `rsync`), and an archive extracted into a
   directory (`tar -x -C`, `unzip -d`), as a write to it. The members `tar -x` and `unzip` name, and the paths
   `git rm`, `git mv`, `git checkout` and `git restore` act on, are checked by name. A `find` that deletes or
   runs a command is checked for the protected paths its name tests (`-name`, `-path`, `-regex`) reach under its
   start points (BSD's `-x`, `-s`, `-E` and `-f` included, and `.` when it names none), and the command it runs
   is read like the line's own; when a branch of its expression has no name test, or its command may act beyond
   what it finds (a shell, an interpreter, a script, `{}/..`), it is checked for all its start points hold, and a
   `find` that follows symbolic links (`-L`, `-follow`) is refused. On a mutating line taracode cannot see into,
   a word that names a protected path is refused, its globs expanded: `python3 -c "open('terraform.tfstate',
   'w')..."` is, and so is a read-only line of that kind, so read a protected file with `read_file` or a plain
   `cat`. A pattern ending in `/**` also covers the directory it names (`.git/**` covers `.git`), patterns match
   in any letter case, and a path is matched through its symlinks, a link to a directory included. A `shell`
   command whose target is a directory is refused when the directory holds a protected path: taracode looks for a
   pattern that names one path (`.git/**`, an absolute file) under it, and reads the directory for the other
   patterns, at most 50,000 entries and without entering `node_modules`, `vendor`, `.venv`, `venv`,
   `__pycache__`, `.tox`, `.gradle` and `.terraform`; a larger directory is refused, so remove it yourself or
   name a smaller one, and so is a path more than 256 parts deep. A line taracode cannot parse (an unclosed quote
   or expansion, a heredoc without its delimiter line) counts as a mutation it cannot see into.
   Not seen: a path a command builds at run time (a variable such as `$PWD`, a computed string), a directory an
   interpreter, a script or `xargs` acts on without naming a protected path, a script piped into a shell (`echo
   ... | sh`), what an archive extracted into the working directory or a commit restored inside a named directory
   overwrites, the files `git clean -x` or `-X` removes, a program git runs from its configuration
   (`core.hooksPath`), what a container reaches through a bind mount, the archive `tar -c` writes, a copy into a
   directory the same line makes first, strings run as lines nested deeper than eight levels, and a path that
   changes between the check and the run. That is why the built-in deny patterns also refuse any mutation that
   names `.taracode/policy.yaml`.
2. **Deny patterns** - command globs that are refused outright. A pattern is matched against the whole line, each
   command of a chain (`cd infra && terraform destroy`), each command without the wrappers in front of its
   program (`time`, `timeout 600`, `env X=1`, `sudo`) and by its program's base name
   (`/usr/local/bin/terraform`), each command without its leading one-word options (`terraform -chdir=infra
   destroy`), the command a `find` runs, and the strings a shell's `-c`, `eval`, `su -c`, `$(...)` or backticks
   run, with the script a shell reads from a heredoc. On a line taracode cannot see into, the lines of every
   heredoc are matched too. Each line and each pattern is also read in a canonical form: `kubectl` resource
   aliases are spelled out, and `terraform`, `tofu` and `terragrunt` read as `terraform`, with the words
   between the program and its verb (options, their values, terragrunt's run words) dropped from the line and
   an `apply` or `plan` that carries a true `-destroy` flag, on the line or in a `TF_CLI_ARGS` assignment in
   front of it, read as `destroy`, so `terraform destroy*` refuses `tofu destroy`, `terragrunt run-all
   destroy`, `terraform apply -auto-approve -destroy` and `terraform plan -destroy -out=tfplan` alike, and
   the apply of a plan the terraform tool made with `-destroy` carries a `terraform destroy` line of its own.
   A pattern keeps the words its operator wrote between the program and the verb, so `terraform
   -chdir=*prod* destroy*` refuses only what it names. Not seen: a teardown handed to another program
   (`aws-vault exec`, `ssh host`, `caffeinate`, `env -S`, a script, a `Makefile`), one run by anything but
   terraform, tofu and terragrunt, a verb fed to the program at run time (`echo destroy | xargs terraform`),
   and a flag built at run time.
3. **Required dry runs** - `kubectl apply` shows a server-side diff first, `terraform apply` requires a plan
   produced in this session and shows its summary (a `terraform apply` through the shell, which would run with
   no such plan, is refused and steered to the terraform tool while this is on, and so is a `tofu` or
   `terragrunt` apply: a policy that needs those sets `terraform_apply: false`), `helm upgrade` runs
   `--dry-run` first (a release with a `--post-renderer` is refused: the dry run would run the renderer before
   you approve).
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
  # terraform destroy* also reads tofu, terragrunt, and an apply or plan with -destroy (see the README)
  commands: ["rm -rf /*", "kubectl delete namespace *", "terraform destroy*", "*.taracode/policy.yaml*"]
require_dry_run:                # shown before the permission prompt
  kubectl_apply: true           # kubectl diff first
  terraform_apply: true         # a plan from this session, its summary first
  helm_upgrade: true            # helm --dry-run first (upgrade and install)
redact:
  enabled: true                 # secrets in tool output become [redacted:<kind>]
  extra_patterns: []            # additional Go regular expressions
mcp:
  trust_read_only_hint: true    # a server's readOnlyHint gives its tool a read form (investigate mode)
  read_only: {}                 # per server, the tools (globs) that count as reads when the hint is not trusted
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

Fifteen tools replace the old 58; every call is classified read or mutate from its arguments, not from the
tool's name. Investigate mode exposes the tools that have a read form (thirteen, eleven when `offline` is set).

| Tool | Arguments (summary) | Read when | Mutate when |
|---|---|---|---|
| `read_file` | path, start_line, end_line | always | never |
| `list_files` | path, glob, recursive, max | always | never |
| `search_files` | pattern, path, glob, max | always | never |
| `write_file` | path, content | never | always |
| `edit_file` | path, old, new, preview | never | always |
| `shell` | command, timeout | command matches the read-only allowlist (cat, ls, grep, find, ps, df, du, date, curl GET, dig, nslookup, jq, git read verbs, kubectl read verbs, terraform read verbs, ...) | otherwise |
| `git` | args | status, diff, log, show, branch (list), blame | add, commit, stash, checkout, reset, push, merge, rebase |
| `kubectl` | verb, resource, name, namespace, context, args, output | get, describe, logs, events, top, explain, api-resources, version, diff, dry-run | apply, delete, patch, edit, scale, rollout, exec, cp, drain, cordon |
| `helm` | args | list, status, get, history, show, template, lint, diff | install, upgrade, rollback, uninstall |
| `terraform` | command, dir, args | init (with -backend=false), validate, fmt -check, plan (always -json, post-processed), show, state list, output, graph | apply, destroy, import, taint, state mv/rm/push, workspace delete |
| `docker` | args | ps, images, logs, inspect, stats, compose ps/config/logs | build, run, rm, rmi, exec, compose up/down/restart, push |
| `cloud` | provider (aws, az, gcloud), args | verbs describe, get, list, ls, show | otherwise |
| `scan` | scanner (trivy, gitleaks, tfsec, kubesec, dependency), target, severity | always | never |
| `web_search` | query, max | always (external, disabled by `offline`) | never |
| `web_fetch` | url | always (external, disabled by `offline`) | never |

MCP tools join the same registry. With `mcp.trust_read_only_hint: true` (the default) a server that
annotates a tool `readOnlyHint: true` gives it a read form; every other MCP tool is a mutation and
stays hidden in investigate mode. Set it to `false` for a server you do not trust and list the tools
that may read under `mcp.read_only`, per server, by name or glob. Protected targets and deny patterns
do not apply to MCP tools: the per-tool permission is their only gate.

### Project Memory

Remember project-specific knowledge across sessions:

```bash
> /remember We use PostgreSQL for production databases
> /remember Always run tests before pushing #workflow
> /memory search database
```

## Commands

| Command                                                             | Description                                                |
|---------------------------------------------------------------------|------------------------------------------------------------|
| `/init`                                                             | Initialize the project (creates TARACODE.md and .taracode/) |
| `/reload`                                                           | Reload project context from TARACODE.md                    |
| `/status`                                                           | Show project and session status                            |
| `/session [new [name]\|load <id>\|delete <id>\|rename <id> <name>]` | Show or manage the current session                         |
| `/sessions`                                                         | List all sessions                                          |
| `/clear`                                                            | Clear the conversation (new session)                       |
| `/model`                                                            | Switch between the engine's pulled models                  |
| `/think [auto\|off\|on\|low\|medium\|high]`                         | Show or set the reasoning mode                             |
| `/mode [investigate\|operate]`                                      | Show or switch the operating mode                          |
| `/permissions [allow\|deny\|ask <tool\|all>\|reset]`                | Remembered answers for mutations                           |
| `/audit [all\|export json\|clear]`                                  | Mutations recorded in this project                         |
| `/policy show`                                                      | Effective policy and where it comes from                   |
| `/plan`                                                             | Show the active plan                                       |
| `/context`                                                          | Context window budget breakdown                            |
| `/compact`                                                          | Force conversation compaction                              |
| `/stats`                                                            | Session statistics                                         |
| `/usage`                                                            | Token usage for this session                               |
| `/history [n\|all]`                                                 | File operation history                                     |
| `/undo [n\|--dry-run]`                                              | Undo file modifications                                    |
| `/diff [export]`                                                    | Show or export session changes                             |
| `/remember <text> [#tag]`                                           | Save a memory about this project                           |
| `/memory [search <q>\|delete <id>\|export\|import <file>\|stats\|cleanup\|clear]` | Project memories                             |
| `/mcp [connect\|disconnect <name>\|tools]`                          | MCP servers and their tools                                |
| `/tools`                                                            | List available tools                                       |
| `/upgrade [check\|now\|skip\|changelog\|status]`                    | Check for and install updates                              |
| `/doctor`                                                           | Diagnose the engine and the tools                          |
| `/help`                                                             | Show this help                                             |

## Configuration

Create `~/.taracode/config.yaml`:

```yaml
# taraengine's address; leave it out to find the engine there (--host and TARACODE_HOST override it)
host: http://127.0.0.1:8272

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

| Backend        | Setup                                                                                                  | Notes                                                                     |
|----------------|--------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------|
| **taraengine** | `brew install --cask tara-vision/taracode/taraengine`, or `curl -fsSL https://engine.tara.vision/install.sh \| bash` | The native backend: window control, thinking, `/model` and the evals |
| **vLLM**       | Self-hosted                                                                                            | The OpenAI-compatible path                                                |
| **llama.cpp**  | Self-hosted                                                                                            | The OpenAI-compatible path                                                |

Any other OpenAI-compatible server runs on that path too, without window control, thinking on or off,
`/model` and the evals.

## Roadmap

Phase 2 of the v3 plan (investigate and operate modes, the classified tools, the policy engine, redaction
and the audit log) shipped in 3.0.0-alpha.2. Phase 3 shipped the evals and the first scoreboard in 3.0.0-beta.1,
and 3.0.0 made the line stable. 3.1.0 settled the tool set at fifteen and the connection at one host. 3.2.0
added speed, memory and a board per machine to the evals. 3.3.0 made taraengine the native backend. Runbooks,
the MCP server and the skills pack are next, as 3.4 and 3.5. See [ROADMAP.md](ROADMAP.md).

## Evals and the scoreboard

taracode ships an offline eval suite: 33 DevOps tasks (Kubernetes triage, Helm, Terraform plan review,
Docker and image security, secrets, cloud read-only investigation and refusal cases) whose tool calls
replay from fixtures recorded against real clusters, charts, Terraform directories and images. No task
runs anything for real, and every task drives the same loop, policy gate and redaction a session
uses. Each task scores 0.4 for the tool calls it expected, 0.5 for the answer and 0.1 for never
attempting a forbidden call; a refusal task also asserts that the gate denied the mutation, and a gate
that lets one through fails the run rather than the model.

The published scoreboard is in [docs/evals/scoreboard.md](docs/evals/scoreboard.md) (and on
[code.tara.vision/evals](https://code.tara.vision/evals)), regenerated with each new board. Every run also
records tokens per second, `--hardware` names the machine so the report can rank models per machine, and
the GPU memory of the loaded model is measured by the engine on NVIDIA; where the engine only estimates it
(a Mac), `--gpu-probe` records what the machine itself reports (the evals reference shows the command). Run
the suite against your own taraengine:

```bash
taracode eval run --model gemma4:12b --hardware "your GPU or machine"
taracode eval report
```

Results land in `docs/evals/results/`, transcripts in `evals/runs/`. Writing a task, recording fixtures and
the reproducibility notes are in [docs/evals/README.md](docs/evals/README.md).

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
[sponsor Tara Vision on GitHub](https://github.com/sponsors/tara-vision) to keep the releases, the engine's
catalog and the eval scoreboard coming.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  Built with ❤️ by <a href="https://tara.vision">Tara Vision</a> · Created by <a href="https://github.com/dayanstef">Dejan Stefanoski</a>
</p>
