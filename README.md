# taracode

<p align="center">
  <strong>DevOps & Cloud AI Assistant</strong><br>
  Expert guidance for Kubernetes, Terraform, Docker, and multi-cloud deployments.<br>
  Runs locally with Ollama for complete privacy.
</p>

<p align="center">
  <a href="https://github.com/tara-vision/taracode/releases"><img src="https://img.shields.io/github/v/release/tara-vision/taracode?style=for-the-badge&logo=github&color=blue" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License: MIT"></a>
  <a href="https://goreportcard.com/report/github.com/tara-vision/taracode"><img src="https://goreportcard.com/badge/github.com/tara-vision/taracode?style=for-the-badge" alt="Go Report Card"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go Version"></a>
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

- **DevOps Expertise** - Specialized in Kubernetes, Terraform, Docker, CI/CD, and cloud platforms
- **58 Built-in Tools** - DevOps, security scanning, file operations, git, web search
- **Multi-Agent System** - 7 specialized agents for complex tasks
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
brew install tara-vision/tap/taracode
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
> /init    # Initialize project features
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
| `/upgrade`     | Check for and install updates  |
| `/hosts`       | Multi-host status (v2.0)       |
| `/help`        | Show help                      |

## Configuration

Create `~/.taracode/config.yaml`:

```yaml
# Single Host (simple setup)
host: http://localhost:11434
model: gemma3:27b

# Multi-Host Setup (v2.0) - for multiple Ollama servers
hosts:
  primary:
    url: http://ollama.tara.lab
    models: [gemma3:27b, qwen2.5-coder:32b]
    priority: 1
  local:
    url: http://localhost:11434
    fallback: primary      # Use primary if local is down
    priority: 2
default_host: primary

# Search
search:
  primary: duckduckgo
  fallback: searxng
  brave_api_key: ""    # Optional: Brave Search API

# Memory
memory:
  enabled: true
  auto_capture: true

# Per-agent host assignment
agents:
  coder:
    host: primary
    model: qwen2.5-coder:32b
  reviewer:
    host: local
    model: llama3.2:3b
```

See [config.example.yaml](config.example.yaml) for all options.

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

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md)
and [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

For security issues, please see our [Security Policy](SECURITY.md).

## License

MIT License - see [LICENSE](LICENSE) for details.

---

<p align="center">
  Built with ❤️ by <a href="https://tara.vision">Tara Vision</a> · Created by <a href="https://github.com/dayanstef">Dejan Stefanoski</a>
</p>
