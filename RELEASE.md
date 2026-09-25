## Release Process

Releases are built by goreleaser in GitHub Actions. Pushing a `v*` tag produces binaries for macOS and Linux
(amd64 and arm64), `checksums.txt` with a keyless cosign signature, deb and rpm packages, SLSA provenance, and
a Homebrew cask commit in `tara-vision/homebrew-taracode`.

### 1. Prepare

1. Merge everything for the release into `main`.
2. Move the `[Unreleased]` notes in `CHANGELOG.md` under a new `[X.Y.Z] - YYYY-MM-DD` heading and commit.
3. Run the local dry run: `make snapshot` (builds everything into `dist/` without publishing).

### 2. Tag

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

### 3. Verify

Watch [GitHub Actions](https://github.com/tara-vision/taracode/actions) until `Release` and `SLSA provenance`
are green, then:

```bash
brew update && brew upgrade --cask taracode && taracode --version
curl -fsSL https://code.tara.vision/install.sh | INSTALL_DIR=$(mktemp -d) bash
```

The installer must print `Verifying checksum...` before installing.

### Environment for manual testing

```bash
ollama pull glm-4.7-flash   # 32 GB machines
ollama pull gemma4:12b      # 16 GB machines
export TARACODE_HOST=http://localhost:11434
```
