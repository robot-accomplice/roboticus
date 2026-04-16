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
#
# Pre-v1.0.6: if GitHub returned HTML (maintenance page), a rate-limit
# payload ({"message": "API rate limit exceeded..."}), or any non-JSON
# response, grep '"tag_name"' would silently return empty and sed would
# collapse to empty, leaving callers with a blank VERSION and no idea
# why. The error later would be a cryptic "download failed" instead of
# "GitHub said no."
#
# v1.0.6: prefer jq when available for robust parsing; fall back to
# grep+sed but validate the result is non-empty and looks like a
# version tag. Surface specific HTTP / payload errors when they occur
# so the operator gets an actionable message.
fetch_latest_version() {
    url="https://api.github.com/repos/${REPO}/releases/latest"
    raw=""

    if command -v curl >/dev/null 2>&1; then
        # -f: fail on HTTP >= 400 with non-zero exit (so we see rate-limit etc.)
        # We want to CAPTURE 4xx bodies for diagnostics, so drop -f and inspect.
        raw=$(curl -sSL -w "\nHTTP_STATUS=%{http_code}" "$url" 2>/dev/null) || {
            echo "ERROR: failed to reach GitHub API at $url" >&2
            return 1
        }
    elif command -v wget >/dev/null 2>&1; then
        # --server-response prints "HTTP/1.1 NNN ..." lines to stderr.
        # Capture both streams so we can parse the status code instead
        # of guessing. Pre-v1.0.6-self-audit this branch unconditionally
        # appended "HTTP_STATUS=200" which was a lie whenever wget
        # fetched a non-2xx body (some wget builds silently succeed
        # with 4xx bodies depending on server config).
        wget_combined=$(wget --server-response -qO- "$url" 2>&1) || {
            echo "ERROR: failed to reach GitHub API at $url" >&2
            printf '%s\n' "$wget_combined" | head -5 >&2
            return 1
        }
        # Extract the LAST HTTP status line (wget may print multiple if
        # following redirects) and pull the 3-digit code.
        wget_status=$(printf '%s\n' "$wget_combined" | grep -oE 'HTTP/[0-9.]+ [0-9]{3}' | tail -1 | awk '{print $2}')
        if [ -z "$wget_status" ]; then
            # No HTTP status line found — wget hit the URL without
            # a valid HTTP response (unusual: typically means a raw
            # TCP error or wget built without --server-response
            # support). Treat as unreachable.
            echo "ERROR: wget did not return a parseable HTTP status for $url" >&2
            printf '%s\n' "$wget_combined" | head -5 >&2
            return 1
        fi
        # Separate status from body: wget prints status lines to stderr
        # (we merged streams with 2>&1) and body to stdout. A simple
        # heuristic: lines matching HTTP/... NNN ... are status metadata;
        # lines starting with two spaces are wget's own response headers;
        # everything else is body. Filter those out for body extraction.
        wget_body=$(printf '%s\n' "$wget_combined" | grep -Ev '^(HTTP/|  |[[:space:]]*$)')
        raw="$wget_body
HTTP_STATUS=$wget_status"
    else
        echo "ERROR: neither curl nor wget found. Install one to continue." >&2
        return 1
    fi

    # Strip the HTTP_STATUS marker for parsing; keep a copy for diagnostics.
    status=$(printf '%s\n' "$raw" | sed -n 's/^HTTP_STATUS=\([0-9][0-9]*\)$/\1/p' | tail -1)
    body=$(printf '%s\n' "$raw" | sed '/^HTTP_STATUS=/d')

    if [ -n "$status" ] && [ "$status" != "200" ]; then
        echo "ERROR: GitHub API returned HTTP $status for $url" >&2
        # Show up to 5 lines of the body so rate-limit messages etc. are visible.
        printf '%s\n' "$body" | head -5 >&2
        return 1
    fi

    tag=""
    if command -v jq >/dev/null 2>&1; then
        tag=$(printf '%s\n' "$body" | jq -r '.tag_name // empty' 2>/dev/null)
    else
        tag=$(printf '%s\n' "$body" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    fi

    # Version tags in this repo follow v<year>.<month>.<patch> — validate
    # the shape so an accidental HTML response that happens to include the
    # string "tag_name" doesn't slip a garbage value through.
    case "$tag" in
        v[0-9]*.[0-9]*.[0-9]*) printf '%s\n' "$tag" ;;
        "")
            echo "ERROR: GitHub response contained no tag_name field." >&2
            echo "       Response preview (first 5 lines):" >&2
            printf '%s\n' "$body" | head -5 >&2
            return 1
            ;;
        *)
            echo "ERROR: GitHub response tag_name did not match expected shape:" >&2
            echo "       got: $tag" >&2
            return 1
            ;;
    esac
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
#
# Pre-v1.0.6: when neither sha256sum nor shasum was available this
# function printed a warning and returned 0 — silently downgrading a
# curl-piped bootstrap to "install whatever bytes came down the wire."
# For a pipe-to-shell installer that's a real supply-chain risk: a
# network-level tamperer or a typosquatted mirror gets to install
# arbitrary binaries and the user sees a warning buried in a torrent
# of install output.
#
# v1.0.6: missing checksum tools are a hard error. The user gets a
# clear message telling them exactly which tool to install and the
# install aborts. Can be explicitly opted out via ROBOTICUS_ALLOW_UNVERIFIED=1
# for fully-offline debugging scenarios (documented in the error).
verify_checksum() {
    file="$1"
    expected="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        if [ "${ROBOTICUS_ALLOW_UNVERIFIED:-0}" = "1" ]; then
            echo "ROBOTICUS_ALLOW_UNVERIFIED=1 — skipping checksum verification." >&2
            echo "This is NOT recommended for curl-piped installs." >&2
            return 0
        fi
        cat >&2 <<ERR
ERROR: SHA256 verification tool not found.

Neither 'sha256sum' (GNU coreutils) nor 'shasum' (macOS, Perl) is
available on this system, so the downloaded binary cannot be verified
against the release's SHA256SUMS.txt. Installing unverified binaries
from a curl-piped bootstrap is a supply-chain risk we refuse to take
silently.

To proceed, install one of:
  * Linux:   apt-get install coreutils        (provides sha256sum)
             dnf install coreutils
             apk add coreutils
  * macOS:   shasum is usually present via Perl; if missing, install
             Xcode command-line tools:  xcode-select --install

If you have a specific reason to bypass verification (e.g. you are
working fully offline and have confirmed the bytes out-of-band), set
ROBOTICUS_ALLOW_UNVERIFIED=1 and re-run.
ERR
        exit 1
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
