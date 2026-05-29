#!/usr/bin/env bash
set -e

# Configuration
REPO="shiinasaku/hayate"
BINARY_NAME="hayate"

# ANSI Color Codes (Strictly No Unicode Icons)
CYAN='\033[0;36m'
GREEN='\033[0;32m'
RED='\033[0;31m'
GRAY='\033[1;30m'
NC='\033[0m' # No Color

echo -e "${CYAN}"
cat << "EOF"
  _   _    _ __   __  _  _____  ___ 
 | | | |  / \\ \ / / / \|_   _|/ _ \
 | |_| | / _ \ \ V / / _ \ | | |  _/
 |  _  |/ ___ \ | | / ___ \| | | |  
 |_| |_/_/   \_\_|/_/   \_\_| \___|
EOF
echo -e " Swift Cross-Device File Transfer${NC}\n"

log_info() { echo -e "${GRAY}[*]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_error() { echo -e "${RED}[ERR]${NC} $1"; exit 1; }

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    linux) OS_TARGET="linux" ;;
    darwin) OS_TARGET="darwin" ;;
    *) log_error "Unsupported operating system: $OS" ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH_TARGET="amd64" ;;
    aarch64|arm64) ARCH_TARGET="arm64" ;;
    armv7l|armv8l|arm) ARCH_TARGET="arm" ;;
    *) log_error "Unsupported architecture: $ARCH" ;;
esac

log_info "Detected environment: $OS_TARGET-$ARCH_TARGET"

# Check for Termux
if [ -n "$PREFIX" ] && [[ "$PREFIX" == *com.termux* ]]; then
    log_info "Termux environment detected."
    INSTALL_DIR="$PREFIX/bin"
    USE_SUDO=""
else
    INSTALL_DIR="/usr/local/bin"
    # Check if we need sudo to write to INSTALL_DIR
    if [ -w "$INSTALL_DIR" ]; then
        USE_SUDO=""
    else
        log_info "Elevated privileges required to install to $INSTALL_DIR"
        USE_SUDO="sudo"
    fi
fi

# Fetch latest release data from GitHub API
log_info "Fetching latest release metadata..."
RELEASE_URL="https://api.github.com/repos/${REPO}/releases/latest"
LATEST_TAG=$(curl -sL "$RELEASE_URL" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
    log_error "Failed to fetch the latest release tag from GitHub."
fi

# Construct download URL based on OS and Arch
# Note: Ensure your GitHub release assets are named like: hayate-linux-amd64
ASSET_NAME="${BINARY_NAME}-${OS_TARGET}-${ARCH_TARGET}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${ASSET_NAME}"

TMP_DIR=$(mktemp -d)
TMP_BIN="${TMP_DIR}/${BINARY_NAME}"

log_info "Downloading ${ASSET_NAME} (${LATEST_TAG})..."
if ! curl -# -sL -o "$TMP_BIN" "$DOWNLOAD_URL"; then
    log_error "Download failed. Please check your network connection."
fi

# Install Binary
log_info "Installing to ${INSTALL_DIR}..."
chmod +x "$TMP_BIN"

if ! $USE_SUDO mv "$TMP_BIN" "${INSTALL_DIR}/${BINARY_NAME}"; then
    log_error "Failed to move binary to $INSTALL_DIR. Do you have the right permissions?"
fi

rm -rf "$TMP_DIR"

log_success "Hayate ${LATEST_TAG} installed successfully."
echo -e "${GRAY}Run 'hayate --help' to get started.${NC}"
