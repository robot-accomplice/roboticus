#!/bin/sh
# install.sh — Install or update the Roboticus autonomous agent runtime.
#
# Usage:
#   curl -fsSL https://roboticus.ai/install.sh | sh
#   curl -fsSL https://roboticus.ai/install.sh | sh -s -- --version v2026.04.10
#
# Installs to /usr/local/bin/roboticus (or uses sudo if needed).
# Supports Linux (amd64, arm64) and macOS (amd64, arm64).

set -eu

REPO="robot-accomplice/roboticus"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="roboticus"
PINNED_VERSION=""

# Parse arguments.
while [ $# -gt 0 ]; do
    case "$1" in
        --version|-v)
            PINNED_VERSION="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: install.sh [--version VERSION]"
            echo "  --version VERSION   Install a specific version (e.g., v2026.04.10)"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

# Detect OS.
detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)
            echo "Unsupported OS: $(uname -s)" >&2
            exit 1
            ;;
    esac
}

# Detect architecture.
detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)
            echo "Unsupported architecture: $(uname -m)" >&2
            exit 1
            ;;
    esac
}

# Fetch the latest release tag from GitHub API.
fetch_latest_version() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    else
        echo "Neither curl nor wget found. Please install one." >&2
        exit 1
    fi
}

# Download a URL to a file.
download() {
    url="$1"
    dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url"
    fi
}

# Verify SHA256 checksum.
verify_checksum() {
    file="$1"
    expected="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        echo "Warning: neither sha256sum nor shasum found; skipping verification." >&2
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        echo "Checksum verification failed!" >&2
        echo "  expected: $expected" >&2
        echo "  got:      $actual" >&2
        exit 1
    fi
}

# Main install flow.
main() {
    OS=$(detect_os)
    ARCH=$(detect_arch)
    ARTIFACT="${BINARY_NAME}-${OS}-${ARCH}"

    echo "Detected platform: ${OS}/${ARCH}"

    # Determine version.
    if [ -n "$PINNED_VERSION" ]; then
        VERSION="$PINNED_VERSION"
    else
        echo "Fetching latest version..."
        VERSION=$(fetch_latest_version)
    fi

    if [ -z "$VERSION" ]; then
        echo "Failed to determine version." >&2
        exit 1
    fi
    echo "Installing roboticus ${VERSION}..."

    # Download binary and checksums.
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    echo "Downloading ${ARTIFACT}..."
    download "${BASE_URL}/${ARTIFACT}" "${TMPDIR}/${ARTIFACT}"

    echo "Downloading SHA256SUMS.txt..."
    download "${BASE_URL}/SHA256SUMS.txt" "${TMPDIR}/SHA256SUMS.txt"

    # Extract expected checksum for this artifact.
    EXPECTED_HASH=$(grep "${ARTIFACT}" "${TMPDIR}/SHA256SUMS.txt" | awk '{print $1}')
    if [ -z "$EXPECTED_HASH" ]; then
        echo "No checksum found for ${ARTIFACT} in SHA256SUMS.txt" >&2
        exit 1
    fi

    # Verify.
    echo "Verifying checksum..."
    verify_checksum "${TMPDIR}/${ARTIFACT}" "$EXPECTED_HASH"
    echo "Checksum verified."

    # Install.
    chmod +x "${TMPDIR}/${ARTIFACT}"

    if [ -w "$INSTALL_DIR" ]; then
        mv "${TMPDIR}/${ARTIFACT}" "${INSTALL_DIR}/${BINARY_NAME}"
    else
        echo "Installing to ${INSTALL_DIR} (requires sudo)..."
        sudo mv "${TMPDIR}/${ARTIFACT}" "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    echo ""
    echo "roboticus installed to ${INSTALL_DIR}/${BINARY_NAME}"
    "${INSTALL_DIR}/${BINARY_NAME}" version
}

main
