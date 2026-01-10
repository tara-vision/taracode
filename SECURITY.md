# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please report it responsibly.

### How to Report

**Please use GitHub's private vulnerability reporting feature:**

1. Go to the [Security tab](https://github.com/tara-vision/taracode/security) of this repository
2. Click "Report a vulnerability"
3. Fill out the vulnerability report form

This ensures your report is kept confidential until a fix is available.

### What to Include

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact of the vulnerability
- Any suggested fixes (optional)

### What to Expect

- **Acknowledgment:** We will acknowledge receipt of your report within 48 hours
- **Assessment:** We will investigate and provide an initial assessment within 7 days
- **Resolution:** We aim to release a fix within 30 days for critical vulnerabilities
- **Disclosure:** We will coordinate with you on public disclosure timing

### Scope

This security policy covers:

- The Tara Code CLI application
- Official distribution channels (GitHub releases, Homebrew tap)

### Out of Scope

- Vulnerabilities in third-party dependencies (please report to the upstream project)
- Issues with self-hosted vLLM servers (not maintained by this project)
- Social engineering attacks

## Security Best Practices

When using Tara Code:

- Keep your installation updated to the latest version
- Review commands before execution when using the `execute_command` tool
- Be cautious with file operations in sensitive directories
- Ensure your vLLM server is properly secured if exposed to a network
