#!/bin/bash
#
# taracode installer
# Usage: curl -fsSL https://code.tara.vision/install.sh | bash
#

set -e

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
    echo -e "${RED}Error:${NC} $1"
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

# Main installation
main() {
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

    OS=$(detect_os)
    ARCH=$(detect_arch)

    info "Detected: ${OS}/${ARCH}"

    # Linux only supports amd64
    if [ "$OS" = "linux" ] && [ "$ARCH" = "arm64" ]; then
        error "Linux arm64 is not currently supported. Please use amd64 or build from source."
    fi

    info "Fetching latest version..."
    VERSION=$(get_latest_version)

    if [ -z "$VERSION" ]; then
        error "Could not determine latest version"
    fi

    info "Latest version: ${VERSION}"

    # Construct download URL
    BINARY="taracode-${OS}-${ARCH}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"

    info "Downloading ${BINARY}..."

    TMP_DIR=$(mktemp -d)
    trap "rm -rf ${TMP_DIR}" EXIT

    if ! curl -fsSL "${DOWNLOAD_URL}" -o "${TMP_DIR}/${BINARY_NAME}"; then
        error "Failed to download ${DOWNLOAD_URL}"
    fi

    chmod +x "${TMP_DIR}/${BINARY_NAME}"

    # Install
    info "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."

    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        warn "Need sudo to install to ${INSTALL_DIR}"
        sudo mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    # Verify installation
    if command -v taracode &> /dev/null; then
        INSTALLED_VERSION=$(taracode --version 2>/dev/null | head -1 || echo "unknown")
        info "Installed: ${INSTALLED_VERSION}"
    else
        warn "taracode installed but not in PATH. Add ${INSTALL_DIR} to your PATH."
    fi

    echo ""
    info "Installation complete!"
    echo ""
    echo "  Next steps:"
    echo "    1. Install Ollama:  brew install ollama"
    echo "    2. Pull a model:    ollama pull gemma3:27b"
    echo "    3. Run taracode:    taracode"
    echo ""
    echo "  Documentation: https://github.com/${REPO}"
    echo ""
}

main "$@"
