# taracode

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/tara-vision/taracode)](https://goreportcard.com/report/github.com/tara-vision/taracode)

**DevOps & Cloud AI Assistant** - Expert guidance for Kubernetes, Terraform, Docker, and multi-cloud deployments. Runs
locally with Ollama for complete privacy.

## Why taracode?

- **DevOps Expertise** - Specialized in Kubernetes, Terraform, Docker, CI/CD, and cloud platforms
- **58 Built-in Tools** - DevOps, security scanning, file operations, git, web search
- **Multi-Agent System** - 7 specialized agents for complex tasks
- **Privacy-first** - Runs fully local with Ollama, your data never leaves your machine
- **No Account Required** - Just install and use

## Quick Start

### 1. Install Ollama

```bash
# macOS / Linux
brew install ollama

# Or download from https://ollama.ai
```

### 2. Pull a Model

```bash
ollama pull gemma3:27b    # Recommended (16GB+ RAM)
ollama pull gemma3:12b    # For limited hardware
```

### 3. Install taracode

**Quick install (recommended):**

```bash
curl -fsSL https://code.tara.vision/install.sh | bash
```

**Homebrew (macOS / Linux):**

```bash
brew tap tara-vision/tap
brew install taracode
```

**Go install:**

```bash
go install github.com/tara-vision/taracode@latest
```

**Manual download:**
Download binaries from [GitHub Releases](https://github.com/tara-vision/taracode/releases)

### 4. Run

```bash
cd your-project
taracode
> /init    # Enable project features
```

That's it! Start asking questions about your infrastructure.

## Features

### Screen Monitoring (`/watch`)

Let the AI watch your screen and catch errors before you do:

```bash
> /watch this          # Capture and analyze all screens now
> /watch start         # Start continuous monitoring
> /watch stop          # Stop monitoring
```

### Multi-Agent System

7 specialized agents work together on complex tasks:

| Agent           | Specialty                                    |
|-----------------|----------------------------------------------|
| **Planner**     | Task decomposition and dependency analysis   |
| **Coder**       | Code generation and editing                  |
| **Tester**      | Test execution and output analysis           |
| **Reviewer**    | Code review and quality checks               |
| **DevOps**      | Infrastructure and deployment operations     |
| **Security**    | Security scanning and vulnerability analysis |
| **Diagnostics** | Failure analysis and root cause detection    |

```bash
> /agent list          # List all agents
> /agent use security  # Route next prompt to specific agent
```

### Autonomous Task Execution (`/task`)

Plan and execute multi-step tasks with checkpoints:

```bash
> /task "Add authentication to the API"
> /task "Deploy to production with blue-green strategy"
> /task templates      # List built-in templates
```

### Project Memory

Remember project-specific knowledge across sessions:

```bash
> /remember We use PostgreSQL for production databases
> /remember Always run tests before pushing #workflow
> /memory search database
```

### DevOps Tools

| Category       | Tools                                                          |
|----------------|----------------------------------------------------------------|
| **Kubernetes** | kubectl get/apply/delete/describe/logs/exec, helm list/install |
| **Terraform**  | init, plan, apply, destroy, output, state                      |
| **Docker**     | build, ps, logs, compose, exec                                 |
| **AWS**        | aws cli, ecs, eks operations                                   |
| **Azure**      | az cli, aks operations                                         |
| **GCP**        | gcloud cli, gke operations                                     |
| **Security**   | trivy, gitleaks, SAST, tfsec, kubesec, dependency audit        |

### Security Mode

Full DevSecOps capabilities with audit logging:

```bash
> /mode security       # Switch to security mode

# Security scanning
> Scan this image for vulnerabilities: nginx:latest
> Check for secrets in the current directory
> Run a SAST scan on the codebase
```

## Commands

| Command        | Description                    |
|----------------|--------------------------------|
| `/init`        | Initialize project             |
| `/mode`        | Switch mode (devops, security) |
| `/model`       | Switch between models          |
| `/task`        | Execute multi-step tasks       |
| `/agent`       | Manage specialized agents      |
| `/watch`       | Screen monitoring              |
| `/memory`      | Project memory management      |
| `/permissions` | Tool permission controls       |
| `/audit`       | Security audit log             |
| `/history`     | File operation history         |
| `/undo`        | Undo file modifications        |
| `/diff`        | Show session changes           |
| `/tools`       | List available tools           |
| `/help`        | Show help                      |

## Configuration

Create `~/.taracode/config.yaml`:

```yaml
# Ollama Server
host: http://localhost:11434
model: gemma3:27b

# Search
search:
  primary: duckduckgo
  fallback: searxng
  brave_api_key: ""    # Optional: Brave Search API

# Memory
memory:
  enabled: true
  auto_capture: true

# Agents
agents:
  enabled: true
```

## Supported LLM Backends

| Backend       | Setup                 | Notes                      |
|---------------|-----------------------|----------------------------|
| **Ollama**    | `brew install ollama` | Recommended, easiest setup |
| **vLLM**      | Self-hosted           | For production deployments |
| **llama.cpp** | Self-hosted           | Lightweight option         |

## Development

```bash
make deps      # Install dependencies
make build     # Build binary
make test      # Run tests
make install   # Install to /usr/local/bin
```

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

## About

Built by [Tara Vision, LLC](https://tara.vision) | Created by [Dejan Stefanoski](https://stefanoski.nl)
