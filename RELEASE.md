## Release Process

Releases are built by goreleaser in GitHub Actions. Pushing a `v*` tag produces binaries for macOS and Linux
(amd64 and arm64), the macOS ones signed with the Developer ID certificate and notarized by Apple,
`checksums.txt` with a keyless cosign signature, deb and rpm packages, SLSA provenance, and a Homebrew cask
commit in `tara-vision/homebrew-taracode`.

The release needs six repository secrets: `HOMEBREW_TAP_TOKEN`, and the five that sign and notarize the macOS
binaries, `MACOS_SIGN_P12` (the base64 of the Developer ID .p12), `MACOS_SIGN_PASSWORD`,
`MACOS_NOTARY_ISSUER_ID`, `MACOS_NOTARY_KEY_ID` and `MACOS_NOTARY_KEY` (the base64 of the App Store Connect
.p8). `scripts/require-signing.sh` stops a run that lacks one of the five, before anything is published.

### 1. Prepare

1. Merge everything for the release into `main`.
2. Move the `[Unreleased]` notes in `CHANGELOG.md` under a new `[X.Y.Z] - YYYY-MM-DD` heading, point the link
   references at the new tag, and commit.
3. Run the local dry run: `make snapshot` (builds everything into `dist/` without publishing or signing).

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

The installer must print `Verifying checksum...` before installing. Both macOS binaries must be notarized:
download them from the release and run `spctl -a -t open --context context:primary-signature -vv` on each, which
must name `Notarized Developer ID`.

### Environment for manual testing

```bash
taraengine pull gemma4:12b      # 16 GB of RAM
taraengine pull glm-4.7-flash   # 32 GB of RAM
taraengine pull gemma4:31b      # 48 GB of RAM and up
export TARACODE_HOST=http://127.0.0.1:8272
```
