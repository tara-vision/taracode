# Contributing to Tara Code

Thank you for your interest in contributing to Tara Code! This document provides guidelines and information for contributors.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/taracode.git
   cd taracode/agent
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
cd agent
make build
```

### Running Tests

```bash
make test
```

### Code Style

- Follow standard Go conventions and formatting
- Run `go fmt` before committing
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
```

## Types of Contributions

### Bug Reports

- Use the GitHub issue tracker
- Include steps to reproduce
- Include your environment (OS, Go version, vLLM server info)
- Include relevant logs or error messages

### Feature Requests

- Open an issue describing the feature
- Explain the use case and why it would be valuable
- Be open to discussion about implementation approaches

### Code Contributions

- Bug fixes
- New tools
- Performance improvements
- Documentation improvements
- Test coverage improvements

## Project Structure

```
agent/
├── cmd/           # CLI commands (root.go, repl.go)
├── internal/
│   ├── assistant/ # Core AI loop and conversation handling
│   ├── context/   # Project context analysis
│   ├── storage/   # Session and plan persistence
│   ├── tools/     # Tool implementations
│   └── ui/        # Terminal UI (styles, spinner, markdown rendering)
├── Makefile       # Build and development commands
└── go.mod         # Go module definition
```

## Adding New Tools

1. Add your tool implementation in `internal/tools/`
2. Register the tool in `registry.go`
3. Add tests for your tool
4. Update documentation if needed

## Questions?

If you have questions, feel free to:
- Open an issue for discussion
- Check existing issues and PRs for context

## License

By contributing to Tara Code, you agree that your contributions will be licensed under the Apache License 2.0.
