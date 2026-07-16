#!/bin/bash
set -e

# Javinizer Desktop App Installer (macOS / Linux)
# Usage:
#   curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install-app.sh | bash
#   # install the newest release including prereleases:
#   curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install-app.sh | bash -s -- --pre-release
#
# Installs the clickable desktop app:
#   macOS  → /Applications/Javinizer.app (falls back to ~/Applications)
#   Linux  → ~/Applications/javinizer-desktop-<arch>.AppImage
#
# Windows is NOT supported by this script — a bash one-liner can't install a
# GUI .exe into the Start menu. Windows users should use Scoop
# (`scoop install javinizer-app`) or download `javinizer-desktop-windows-amd64.exe`
# directly from the Releases page.

GITHUB_REPO="javinizer/javinizer-go"
PRE_RELEASE=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --pre-release)
            PRE_RELEASE=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}" >&2
            echo "Usage: bash install-app.sh [--pre-release]" >&2
            exit 1
            ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Detect OS and architecture. Desktop assets use bundle-convention arch names:
# Linux AppImages use uname -m (x86_64/aarch64), not Go's amd64/arm64. macOS
# ships a single universal zip (no arch suffix).
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS_NAME="linux"
            ;;
        darwin)
            OS_NAME="darwin"
            ;;
        mingw*|msys*|cygwin*)
            echo -e "${RED}Windows is not supported by this script.${NC}" >&2
            echo -e "${YELLOW}Use Scoop instead:${NC}" >&2
            echo -e "${GREEN}  scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer${NC}" >&2
            echo -e "${GREEN}  scoop install javinizer-app${NC}" >&2
            echo -e "${YELLOW}Or download javinizer-desktop-windows-amd64.exe from:${NC}" >&2
            echo -e "${GREEN}  https://github.com/$GITHUB_REPO/releases/latest${NC}" >&2
            exit 1
            ;;
        *)
            echo -e "${RED}Unsupported OS: $OS${NC}" >&2
            exit 1
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            LINUX_ARCH="x86_64"
            ;;
        aarch64|arm64)
            LINUX_ARCH="aarch64"
            ;;
        *)
            echo -e "${RED}Unsupported architecture: $ARCH${NC}" >&2
            exit 1
            ;;
    esac

    if [ "$OS_NAME" = "darwin" ]; then
        ASSET="javinizer-desktop-macos-universal.zip"
    else
        ASSET="javinizer-desktop-linux-${LINUX_ARCH}.AppImage"
    fi

    echo -e "${GREEN}Detected platform: $OS_NAME ($ARCH)${NC}"
    echo -e "${GREEN}Desktop asset: $ASSET${NC}"
}

calculate_sha256() {
    local file="$1"

    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$file" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$file" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$file" | awk '{print $NF}'
    else
        echo -e "${RED}No SHA256 tool found (sha256sum/shasum/openssl) — refusing to install unverified bundle${NC}" >&2
        exit 1
    fi
}

get_latest_version() {
    echo -e "${YELLOW}Fetching latest release...${NC}"
    if [ "$PRE_RELEASE" = true ]; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases?per_page=1" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -1)
    else
        VERSION=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [ -z "$VERSION" ]; then
            echo -e "${RED}No stable release is available yet.${NC}"
            echo -e "${YELLOW}To install the latest pre-release, re-run with --pre-release:${NC}"
            echo -e "${GREEN}  curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install-app.sh | bash -s -- --pre-release${NC}"
            echo -e "${YELLOW}Or download a specific release from: https://github.com/$GITHUB_REPO/releases${NC}"
            exit 1
        fi
    fi

    if [ -z "$VERSION" ]; then
        echo -e "${RED}Failed to fetch latest version${NC}"
        exit 1
    fi

    if [ "$PRE_RELEASE" = true ] && echo "$VERSION" | grep -q -- '-'; then
        echo -e "${YELLOW}Note: $VERSION is a pre-release.${NC}"
    fi
    echo -e "${GREEN}Latest version: $VERSION${NC}"
}

download_bundle() {
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/$ASSET"
    CHECKSUM_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/checksums.txt"

    echo -e "${YELLOW}Downloading $ASSET from $DOWNLOAD_URL${NC}"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="$TMP_DIR/$ASSET"

    if ! curl -L -o "$TMP_FILE" "$DOWNLOAD_URL"; then
        echo -e "${RED}Failed to download bundle${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    # Verify checksum — same fail-closed policy as install.sh / javinizer upgrade.
    echo -e "${YELLOW}Verifying checksum...${NC}"
    if ! curl -fsSL "$CHECKSUM_URL" -o "$TMP_DIR/checksums.txt"; then
        echo -e "${RED}Could not download checksums.txt — refusing to install unverified bundle${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    EXPECTED_CHECKSUM=$(awk -v name="$ASSET" '$2==name || $2=="*"name {print $1; exit}' "$TMP_DIR/checksums.txt")
    ACTUAL_CHECKSUM=$(calculate_sha256 "$TMP_FILE")

    if [ -z "$EXPECTED_CHECKSUM" ]; then
        echo -e "${RED}Checksum for $ASSET not found in checksums.txt — refusing to install${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi
    if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
        echo -e "${RED}Checksum verification failed!${NC}"
        echo -e "${RED}Expected: $EXPECTED_CHECKSUM${NC}"
        echo -e "${RED}Actual:   $ACTUAL_CHECKSUM${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi
    echo -e "${GREEN}Checksum verified!${NC}"
}

install_macos() {
    # The zip was built with `ditto -c -k --keepParent`, so unzipping yields
    # Javinizer.app/ at the top level.
    STAGE_DIR="$TMP_DIR/stage"
    mkdir -p "$STAGE_DIR"
    unzip -q "$TMP_FILE" -d "$STAGE_DIR"

    if [ ! -d "$STAGE_DIR/Javinizer.app" ]; then
        echo -e "${RED}Downloaded zip does not contain Javinizer.app — refusing to install${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    # Prefer /Applications; fall back to ~/Applications without sudo, mirroring
    # install.sh's PATH fallback. Don't run sudo in a curl|bash pipe (no tty)
    # unless passwordless sudo is available.
    local dest
    if [ -w "/Applications" ]; then
        dest="/Applications"
        echo -e "${YELLOW}Installing to $dest...${NC}"
        rm -rf "$dest/Javinizer.app"
        mv "$STAGE_DIR/Javinizer.app" "$dest/Javinizer.app"
    elif sudo -n true 2>/dev/null; then
        dest="/Applications"
        echo -e "${YELLOW}Installing to $dest (requires sudo)...${NC}"
        sudo rm -rf "$dest/Javinizer.app"
        sudo mv "$STAGE_DIR/Javinizer.app" "$dest/Javinizer.app"
    else
        dest="$HOME/Applications"
        echo -e "${YELLOW}Installing to $dest (user-local; /Applications not writable)...${NC}"
        mkdir -p "$dest"
        rm -rf "$dest/Javinizer.app"
        mv "$STAGE_DIR/Javinizer.app" "$dest/Javinizer.app"
    fi

    # Best-effort quarantine strip so Gatekeeper doesn't re-prompt on the first
    # launch of the unsigned app. Don't fail if xattr is unavailable (older macOS
    # or non-HFS volumes).
    if command -v xattr >/dev/null 2>&1; then
        xattr -dr com.apple.quarantine "$dest/Javinizer.app" 2>/dev/null || true
    fi

    echo -e "${GREEN}Installed Javinizer.app to $dest${NC}"
    echo -e "${YELLOW}First launch: the app is unsigned. If macOS shows \"Javinizer cannot be opened"
    echo -e "${YELLOW}because the developer cannot be verified\", right-click → Open → confirm (one-time).${NC}"
    rm -rf "$TMP_DIR"
}

install_linux() {
    local dest_dir="$HOME/Applications"
    local dest="$dest_dir/javinizer-desktop-$LINUX_ARCH.AppImage"
    mkdir -p "$dest_dir"
    rm -f "$dest"
    mv "$TMP_FILE" "$dest"
    chmod +x "$dest"
    echo -e "${GREEN}Installed $dest${NC}"
    echo -e "${YELLOW}Launch it with: $dest${NC}"
    echo -e "${YELLOW}Tip: integrate with your app launcher via AppImageLauncher or a .desktop file.${NC}"
    rm -rf "$TMP_DIR"
}

main() {
    echo -e "${GREEN}=== Javinizer Desktop App Installer ===${NC}"
    detect_platform
    get_latest_version
    download_bundle
    case "$OS_NAME" in
        darwin) install_macos ;;
        linux)  install_linux ;;
    esac
    echo -e "${GREEN}=== Installation Complete ===${NC}"
}

main
