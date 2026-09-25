# Contributing to taracode

Thank you for your interest in contributing to taracode! This document provides guidelines and information for contributors.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/taracode.git
   cd taracode
   ```
3. **Install dependencies**:
   ```bash
   make deps
   ```
4. **Create a branch** for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

## Development Workflow

### Building

```bash
make build
```

### Running Tests

```bash
make test
```

### Testing with LLM

taracode requires a local LLM backend. Ollama is recommended:

```bash
# Install Ollama
brew install ollama

# Pull recommended model
ollama pull glm-4.7-flash   # gemma4:12b on 16 GB machines

# Run taracode
./taracode
```

### Code Style

- Follow standard Go conventions and formatting
- Run `gofmt -s -w .` before committing (the `-s` flag simplifies code)
- Run `go vet ./...`, `make lint` (golangci-lint), `make vuln` (govulncheck) and `make coverage-gate`
  (per-package coverage floors) before opening a PR
- Keep functions focused and well-documented; CI fails on any Go file over 800 lines, test files included
- Write tests for new functionality

**Formatting check:**

```bash
# Check if any files need formatting
gofmt -s -l .

# Auto-format all files
gofmt -s -w .
```

## Submitting Changes

### Pull Request Process

1. **Ensure your code builds** and all tests pass
2. **Update documentation** if you're changing behavior
3. **Write clear commit messages** describing your changes
4. **Open a Pull Request** with a clear description of:
   - What the change does
   - Why it's needed
   - Any breaking changes

### Commit Message Guidelines

Use clear, descriptive commit messages:

```
feat: add new file operation tool
fix: resolve git diff parsing issue
docs: update README installation section
refactor: simplify tool registry logic
test: add tests for memory manager
```

## Types of Contributions

### Bug Reports

- Use the GitHub issue tracker
- Include steps to reproduce
- Include your environment (OS, Go version, provider)
- Include relevant logs or error messages

### Feature Requests

- Open an issue describing the feature
- Explain the use case and why it would be valuable
- Be open to discussion about implementation approaches

### Code Contributions

- Bug fixes
- New tools
- New LLM provider integrations
- Performance improvements
- Documentation improvements
- Test coverage improvements

## Project Structure

```
taracode/
├── cmd/                       # CLI commands and the REPL (one file per command group, e.g. mode_cmd.go)
│   ├── root.go                # Cobra CLI setup
│   ├── repl.go                # Interactive REPL loop
│   ├── commands.go            # The command table: dispatch, /help, completion
│   └── ...
├── internal/
│   ├── agent/                 # The agentic loop: classify, policy, audit, dry run, permission, execute
│   ├── policy/                # Modes, the policy YAML and its merge, the permission store
│   ├── tools/                 # The fifteen built-in tools and their registry
│   │   ├── classify/          # Per-invocation read/mutate classifiers (git, kubectl, helm, terraform, ...)
│   │   ├── shellwords/        # Shell command-line tokenizer
│   │   ├── redact/            # Secret redaction of tool output
│   │   └── tfplan/            # terraform plan -json summariser
│   ├── llm/                   # Transport to the model server (native Ollama, OpenAI-compatible adapter)
│   ├── models/                # Embedded model registry and host RAM diagnostics
│   ├── context/               # Project context analysis
│   ├── history/               # Operation history and undo
│   ├── mcp/                   # Model Context Protocol
│   ├── memory/                # Project memory
│   ├── provider/              # LLM providers (Ollama, vLLM, llama.cpp)
│   ├── search/                # Web search providers
│   ├── storage/               # Session persistence and the audit log
│   ├── upgrade/               # Auto-upgrade
│   └── ui/                    # Terminal UI
├── Makefile
├── go.mod
└── README.md
```

## Adding New Tools

taracode ships fifteen tools (`internal/tools/builtin.go`); there is no `definitions.go` or separate
registration step left from the old 58-tool set.

1. Add your tool as a `*tools.Tool` (name, description, params, a `Classify` function that returns
   `policy.Read` or `policy.Mutate` for each invocation, `Run`, and `DryRun` if the tool needs one) - see
   any file in `internal/tools/` such as `kubectl_tool.go` for the pattern.
2. Register it in `Builtin()` in `internal/tools/builtin.go`.
3. Add a classifier under `internal/tools/classify/` if the read/mutate split depends on the command line.
4. Add tests for the tool and its classifier.
5. Update the Tools table in `README.md` and the operator view.

## Adding New Providers

1. Implement the `Provider` interface in `internal/provider/`
2. Add provider creation in `factory.go`
3. Update auto-detection in `detect.go` if applicable
4. Add tests
5. Update README with provider documentation

## Adding an Eval Task

taracode's offline eval suite (`evals/tasks/`, `internal/evals`) needs new tasks as the tool set and the
model registry grow. Writing a task, recording its fixtures against the lab sandbox, authoring fixtures by
hand for a task that needs no live scenario, and running `taracode eval lint` over the corpus are all covered
in [docs/evals/README.md](docs/evals/README.md); start there.

## Questions?

If you have questions, feel free to:
- Open an issue for discussion
- Check existing issues and PRs for context

## License

By contributing to taracode, you agree that your contributions will be licensed under the MIT License.
