# Tara Code

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tara-vision/taracode)](go.mod)
[![Release](https://img.shields.io/github/v/release/tara-vision/taracode)](https://github.com/tara-vision/taracode/releases)
[![CI](https://github.com/tara-vision/taracode/actions/workflows/build.yml/badge.svg)](https://github.com/tara-vision/taracode/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/tara-vision/taracode)](https://goreportcard.com/report/github.com/tara-vision/taracode)

An AI-powered CLI assistant for software development, powered by local LLMs.

## Why Tara Code?

- **Privacy-first**: Your code never leaves your infrastructure
- **Local LLMs**: Run on your own hardware with vLLM, Ollama, or llama.cpp
- **Multi-vendor**: Auto-detects and works with multiple LLM providers
- **No subscriptions**: One-time setup, no recurring costs
- **Full control**: Choose your model, configure your workflow

## Officially Supported Model

**Qwen3** is the officially supported model family for Tara Code.

| Model | Size | RAM Required | Recommendation |
|-------|------|--------------|----------------|
| `qwen3:8b` | 8B | ~8GB | Good for basic tasks |
| `qwen3:14b` | 14B | ~16GB | Balanced performance |
| `qwen3:30b` | 30B | ~32GB | **Recommended** - Best results |

```bash
# Install with Ollama
ollama pull qwen3:30b

# Or for smaller hardware
ollama pull qwen3:14b
```

> **Note**: Other models may work but are not officially tested or supported. Qwen3 provides the best balance of instruction following, tool calling, and code understanding.

## Requirements

- Go 1.21 or later (for building from source)
- A running LLM server:
  - [vLLM](https://github.com/vllm-project/vllm) - High-performance inference server
  - [Ollama](https://ollama.ai) - Easy local LLM deployment
  - [llama.cpp](https://github.com/ggerganov/llama.cpp) - Lightweight C++ inference

## Installation

### Homebrew (macOS and Linux)

```bash
brew tap tara-vision/taracode
brew install taracode
```

### Pre-built Binaries

Download the latest binary for your platform from the [Releases](https://github.com/tara-vision/taracode/releases) page.

### Build from Source

```bash
git clone https://github.com/tara-vision/taracode.git
cd taracode
make build
```

## Quick Start

1. Configure your LLM server:

   ```bash
   # For Ollama
   export TARACODE_HOST=http://localhost:11434

   # For vLLM
   export TARACODE_HOST=http://your-vllm-server:8000

   # For llama.cpp
   export TARACODE_HOST=http://localhost:8080
   ```

2. Run Tara Code:

   ```bash
   taracode
   ```

The vendor and model are automatically detected from your server.

## Features

- **Multi-vendor support**: Works with vLLM, Ollama, and llama.cpp servers
- **Auto-detection**: Automatically detects vendor type from host URL
- **Multi-turn tool calling**: LLM can execute multiple tools in a single response for efficiency
- **Error recovery**: Automatic retry with exponential backoff for transient network errors
- **Token tracking**: Track token usage with `/usage` command
- **Streaming responses**: Real-time output as the LLM generates text
- **Thinking indicators**: Animated spinners while waiting for responses
- **Syntax highlighting**: Code blocks rendered with colors via Glamour
- **Auto model detection** from server
- **File references** using `@` to include files in conversations
- **File operations**: read, write, edit, copy, move, delete, and surgical line edits
- **Git integration**: status, diff, log, add, commit, and branch management
- **Search**: grep patterns and glob file finding
- **Project awareness**: `/init` creates context for the AI to understand your codebase

## Commands

| Command   | Description                |
|-----------|----------------------------|
| `/init`   | Initialize project context |
| `/reload` | Reload project context     |
| `/clear`  | Clear conversation         |
| `/usage`  | Show token usage stats     |
| `/help`   | Show help                  |
| `exit`    | Exit                       |

## File References

Include files in your conversations using the `@` symbol:

```bash
# Interactive selection
> @

# Direct reference
> Review @main.go and suggest improvements

# Multiple files
> Compare @internal/tools/file_tools.go with @internal/tools/git_tools.go
```

## Configuration

Set `TARACODE_HOST` environment variable or create `~/.taracode/config.yaml`:

```yaml
host: http://localhost:11434  # LLM server URL
model: qwen3:30b              # Recommended model (see Officially Supported Model)
vendor: ""                    # auto, vllm, ollama, llama.cpp (empty = auto-detect)
key: ""                       # optional API key
```

> **Tip**: Always specify `model: qwen3:30b` (or `qwen3:14b` for smaller hardware) for best results.

### CLI Flags

| Flag           | Description                               |
|----------------|-------------------------------------------|
| `--host`       | LLM server URL                            |
| `--vendor`     | LLM vendor (auto, vllm, ollama, llama.cpp)|
| `--key`        | API key (optional)                        |
| `--model`      | Model name (auto-detected if not set)     |
| `--no-stream`  | Disable streaming (show response at once) |
| `--no-spinner` | Disable spinner animations                |
| `--config`     | Custom config file path                   |

## Development

```bash
make deps      # Install dependencies
make build     # Build binary
make test      # Run tests
make build-all # Cross-compile for all platforms
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## Security

To report security vulnerabilities, see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
