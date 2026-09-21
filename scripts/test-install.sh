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

# run_verify <file> <asset> <sums> - runs verify_checksum the way main does (errexit and
# pipefail on, no enclosing if), printing its stderr and returning its exit status.
run_verify() {
    local out rc=0
    out=$( ( set -euo pipefail; verify_checksum "$1" "$2" "$3" ) 2>&1 ) || rc=$?
    printf '%s' "$out"
    return "$rc"
}

if out=$(run_verify "$tmp/taracode-linux-amd64" taracode-linux-amd64 "$tmp/checksums.txt"); then
    echo "PASS: matching checksum accepted"
else
    echo "FAIL: matching checksum rejected: $out"; exit 1
fi

if out=$(run_verify "$tmp/taracode-linux-amd64" taracode-darwin-arm64 "$tmp/checksums.txt"); then
    echo "FAIL: mismatching checksum accepted"; exit 1
elif [[ "$out" == *"Checksum mismatch for taracode-darwin-arm64"* ]]; then
    echo "PASS: mismatching checksum rejected with a clear message"
else
    echo "FAIL: mismatch rejected without the expected message: $out"; exit 1
fi

if out=$(run_verify "$tmp/taracode-linux-amd64" taracode-linux-arm64 "$tmp/checksums.txt"); then
    echo "FAIL: missing checksum entry accepted"; exit 1
elif [[ "$out" == *"No checksum for taracode-linux-arm64"* ]]; then
    echo "PASS: missing checksum entry rejected with a clear message"
else
    echo "FAIL: missing entry rejected without the expected message: $out"; exit 1
fi

# A version lookup that returns no tag_name (for example a rate-limited GitHub API reply)
# must yield an empty version, not kill the script, so main can print its own error.
curl() { printf '{"message":"API rate limit exceeded"}'; }
if version=$( ( set -euo pipefail; get_latest_version || true ) ) && [ -z "$version" ]; then
    echo "PASS: version lookup failure yields an empty version"
else
    echo "FAIL: version lookup failure did not yield an empty version: '$version'"; exit 1
fi
unset -f curl
