## Release Process

Follow these steps to release a new version of **taracode**. The CI/CD pipeline handles binary compilation, GitHub
Release creation, and the Homebrew formula updates automatically.

### Phase 1: Tag and Release

1. **Prepare and Push Code**
   Ensure your code is tested and ready on the `main` branch.

2. **Create and Push Tag**
   Pushing a tag starting with `v` triggers the automated release workflow.
    ```bash
    git tag -a vX.Y.Z -m "Release vX.Y.Z"
    git push origin vX.Y.Z
    ```

3. **Monitor Automation**
   Go to [GitHub Actions](https://github.com/tara-vision/taracode/actions) to watch the
   progress:

* **Build & Release:** Compiles Go binaries for macOS (Intel/Silicon) and Linux, then creates a GitHub Release.
* **Update Brew:** Calculates the new SHA-256 and automatically commits the updated formula to
  `tara-vision/homebrew-taracode`.
---

### Phase 2: Verify Installation

Once the GitHub Actions turn green, verify the new version is available to the world:

```bash
# Update Homebrew's local index
brew update

# Upgrade to the latest version
brew upgrade taracode

# Verify the injected version string matches your tag
taracode --version
```
---

### Environment Setup

For new machines, remember to point to your LLM server:

```bash
# Add this to your .zshrc or .bashrc

# For Ollama
export TARACODE_HOST=http://localhost:11434

# For vLLM
export TARACODE_HOST=http://your-vllm-server:8000

# For llama.cpp
export TARACODE_HOST=http://localhost:8080
```
