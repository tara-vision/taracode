# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 2.1.x   | :white_check_mark: |
| < 2.1   | :x:                |

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
- Issues with self-hosted LLM servers (Ollama, vLLM, llama.cpp)
- Social engineering attacks

## Verifying Downloads

Every release ships `checksums.txt`, a keyless cosign signature of it, and SLSA provenance.

```bash
VERSION=v2.1.0
BASE=https://github.com/tara-vision/taracode/releases/download/${VERSION}
curl -fsSLO ${BASE}/checksums.txt -O ${BASE}/checksums.txt.sig -O ${BASE}/checksums.txt.pem
cosign verify-blob --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/tara-vision/taracode/.github/workflows/release.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --ignore-missing -c checksums.txt   # shasum -a 256 -c on macOS
```

## Security Best Practices

When using Tara Code:

- Keep your installation updated to the latest version
- Review commands before execution when using the `execute_command` tool
- Be cautious with file operations in sensitive directories
- Ensure your self-hosted LLM servers (Ollama, vLLM, llama.cpp) are properly secured if exposed to a network
