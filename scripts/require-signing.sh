#!/usr/bin/env bash
# Stops a release whose run lacks a secret that signs or notarizes the macOS binaries. goreleaser skips the notarize
# step when the certificate is missing, so without this check a tag would publish unsigned binaries and the cask would
# hand them to every Mac, where Gatekeeper refuses a quarantined download. `goreleaser check` and a snapshot build need
# no secrets and do not run this.
#
# Usage: scripts/require-signing.sh, with the five secrets in the environment.
set -euo pipefail
for name in MACOS_SIGN_P12 MACOS_SIGN_PASSWORD MACOS_NOTARY_ISSUER_ID MACOS_NOTARY_KEY_ID MACOS_NOTARY_KEY; do
  if [ -z "${!name:-}" ]; then
    echo "release: $name is not set; a release ships only signed and notarized macOS binaries" >&2
    exit 1
  fi
done
