#!/bin/bash
# Offline test of install.sh checksum verification. Run: bash scripts/test-install.sh
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=../install.sh
source ./install.sh

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

printf 'not really a binary' > "$tmp/taracode-linux-amd64"
good=$(sha256_of "$tmp/taracode-linux-amd64")
zero="0000000000000000000000000000000000000000000000000000000000000000"
printf '%s  taracode-linux-amd64\n%s  taracode-darwin-arm64\n' "$good" "$zero" > "$tmp/checksums.txt"

if ( verify_checksum "$tmp/taracode-linux-amd64" taracode-linux-amd64 "$tmp/checksums.txt" ) >/dev/null 2>&1; then
    echo "PASS: matching checksum accepted"
else
    echo "FAIL: matching checksum rejected"; exit 1
fi

if ( verify_checksum "$tmp/taracode-linux-amd64" taracode-darwin-arm64 "$tmp/checksums.txt" ) >/dev/null 2>&1; then
    echo "FAIL: mismatching checksum accepted"; exit 1
else
    echo "PASS: mismatching checksum rejected"
fi

if ( verify_checksum "$tmp/taracode-linux-amd64" taracode-linux-arm64 "$tmp/checksums.txt" ) >/dev/null 2>&1; then
    echo "FAIL: missing checksum entry accepted"; exit 1
else
    echo "PASS: missing checksum entry rejected"
fi
