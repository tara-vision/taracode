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
ollama pull gemma3:27b

# Run taracode
./taracode
```

### Code Style

- Follow standard Go conventions and formatting
- Run `go fmt` before committing
- Run `go vet` to catch common issues
- Keep functions focused and well-documented
- Write tests for new functionality

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
├── cmd/                    # CLI commands
│   ├── root.go             # Cobra CLI setup
│   ├── repl.go             # Interactive REPL
│   └── ...
├── internal/
│   ├── agent/              # Multi-agent system
│   ├── assistant/          # Core AI loop
│   ├── context/            # Project context analysis
│   ├── history/            # Operation history
│   ├── mcp/                # Model Context Protocol
│   ├── memory/             # Project memory
│   ├── orchestrator/       # Agent orchestration
│   ├── permissions/        # Tool permissions
│   ├── provider/           # LLM providers (Ollama, vLLM, llama.cpp)
│   ├── search/             # Web search providers
│   ├── storage/            # Session persistence
│   ├── tools/              # Tool implementations
│   ├── ui/                 # Terminal UI
│   └── watch/              # Screen monitoring
├── Makefile
├── go.mod
└── README.md
```

## Adding New Tools

1. Add your tool implementation in `internal/tools/`
2. Add the tool definition in `definitions.go`
3. Register the tool in `registry.go`
4. Add tests for your tool
5. Update documentation if needed

## Adding New Providers

1. Implement the `Provider` interface in `internal/provider/`
2. Add provider creation in `factory.go`
3. Update auto-detection in `detect.go` if applicable
4. Add tests
5. Update README with provider documentation

## Questions?

If you have questions, feel free to:
- Open an issue for discussion
- Check existing issues and PRs for context

## License

By contributing to taracode, you agree that your contributions will be licensed under the MIT License.
