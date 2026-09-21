#!/bin/bash
#
# taracode installer
# Usage: curl -fsSL https://code.tara.vision/install.sh | bash
#
# Environment:
#   INSTALL_DIR       target directory (default /usr/local/bin)
#   TARACODE_VERSION  tag to install (default: latest release)
#

set -euo pipefail

REPO="tara-vision/taracode"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY_NAME="taracode"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}==>${NC} $1"
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

error() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Darwin*) echo "darwin" ;;
        Linux*)  echo "linux" ;;
        *)       error "Unsupported operating system: $(uname -s)" ;;
    esac
}

# Detect architecture
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)            error "Unsupported architecture: $(uname -m)" ;;
    esac
}

# Get latest version from GitHub
get_latest_version() {
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
        grep '"tag_name":' |
        sed -E 's/.*"([^"]+)".*/\1/'
}

# sha256_of <file> - print the SHA-256 of a file
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        error "Need sha256sum or shasum to verify the download"
    fi
}

# verify_checksum <file> <asset-name> <checksums-file>
# Exits with an error unless the file's SHA-256 matches the asset's line in checksums.txt.
verify_checksum() {
    local file="$1" asset="$2" sums="$3" expected actual
    expected=$(grep -E "^[0-9a-f]{64}  ${asset}\$" "$sums" || true)
    expected=${expected%%[[:space:]]*}
    if [ -z "$expected" ]; then
        error "No checksum for ${asset} in checksums.txt; refusing to install an unverified binary"
    fi
    actual=$(sha256_of "$file" || true)
    if [ -z "$actual" ]; then
        error "Could not compute the SHA-256 of ${file}"
    fi
    if [ "$expected" != "$actual" ]; then
        error "Checksum mismatch for ${asset}: expected ${expected}, got ${actual}"
    fi
}

banner() {
    echo ""
    echo "  ████████╗ █████╗ ██████╗  █████╗  ██████╗ ██████╗ ██████╗ ███████╗"
    echo "  ╚══██╔══╝██╔══██╗██╔══██╗██╔══██╗██╔════╝██╔═══██╗██╔══██╗██╔════╝"
    echo "     ██║   ███████║██████╔╝███████║██║     ██║   ██║██║  ██║█████╗  "
    echo "     ██║   ██╔══██║██╔══██╗██╔══██║██║     ██║   ██║██║  ██║██╔══╝  "
    echo "     ██║   ██║  ██║██║  ██║██║  ██║╚██████╗╚██████╔╝██████╔╝███████╗"
    echo "     ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝"
    echo ""
    echo "  DevOps & Cloud AI Assistant"
    echo ""
}

# Main installation
main() {
    banner

    local os arch version asset base tmp_dir
    os=$(detect_os)
    arch=$(detect_arch)
    info "Detected: ${os}/${arch}"

    version="${TARACODE_VERSION:-}"
    if [ -z "$version" ]; then
        info "Fetching latest version..."
        version=$(get_latest_version || true)
    fi
    if [ -z "$version" ]; then
        error "Could not determine the version to install"
    fi
    info "Version: ${version}"

    asset="taracode-${os}-${arch}"
    base="https://github.com/${REPO}/releases/download/${version}"

    tmp_dir=$(mktemp -d)
    # Bake the path into the trap now: main's locals are gone by the time EXIT fires.
    trap "rm -rf '${tmp_dir}'" EXIT

    info "Downloading ${asset}..."
    if ! curl -fsSL "${base}/${asset}" -o "${tmp_dir}/${BINARY_NAME}"; then
        error "Failed to download ${base}/${asset}"
    fi

    info "Downloading checksums.txt..."
    if ! curl -fsSL "${base}/checksums.txt" -o "${tmp_dir}/checksums.txt"; then
        error "This release has no checksums.txt; refusing to install unverified binaries (releases from v2.1.0 ship one)"
    fi

    info "Verifying checksum..."
    verify_checksum "${tmp_dir}/${BINARY_NAME}" "${asset}" "${tmp_dir}/checksums.txt"
    chmod +x "${tmp_dir}/${BINARY_NAME}"

    info "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
    if [ -w "$INSTALL_DIR" ]; then
        mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        warn "Need sudo to install to ${INSTALL_DIR}"
        sudo mv "${tmp_dir}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    if command -v taracode >/dev/null 2>&1; then
        info "Installed: $(taracode --version 2>/dev/null | head -1 || echo unknown)"
    else
        warn "taracode installed but not in PATH. Add ${INSTALL_DIR} to your PATH."
    fi

    echo ""
    info "Installation complete!"
    echo ""
    echo "  Next steps:"
    echo "    1. Install Ollama:  brew install ollama    (or https://ollama.com/download)"
    echo "    2. Pull a model:    ollama pull gemma4:12b   # 16 GB machines"
    echo "                        ollama pull qwen3.8:27b  # 32 GB machines"
    echo "    3. Run taracode:    cd your-project && taracode"
    echo ""
    echo "  Documentation: https://github.com/${REPO}"
    echo ""
}

# Run main when executed or piped into bash; stay quiet when sourced by tests.
if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
