#!/bin/sh
# seek installer script for Linux and macOS
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ozgurulukir/seek/main/install.sh | sh

set -e

REPO="ozgurulukir/seek"
BINARY_NAME="seek"

# --- Styling & Output Helpers ---
setup_colors() {
  if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    BLUE='\033[0;34m'
    YELLOW='\033[1;33m'
    BOLD='\033[1m'
    RESET='\033[0m'
  else
    RED=''
    GREEN=''
    BLUE=''
    YELLOW=''
    BOLD=''
    RESET=''
  fi
}

info() {
  printf "${BLUE}${BOLD}==>${RESET} ${BOLD}%s${RESET}\n" "$1"
}

success() {
  printf "${GREEN}${BOLD}✓${RESET} %s\n" "$1"
}

warn() {
  printf "${YELLOW}${BOLD}!${RESET} %s\n" "$1"
}

abort() {
  printf "${RED}${BOLD}error:${RESET} %s\n" "$1" >&2
  exit 1
}

# --- Platform Detection ---
detect_platform() {
  OS="$(uname -s)"
  case "$OS" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)      abort "Unsupported operating system: $OS. seek supports Linux, macOS, and Windows." ;;
  esac

  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             abort "Unsupported architecture: $ARCH" ;;
  esac

  if [ "$OS" = "darwin" ] && [ "$ARCH" = "amd64" ]; then
    abort "Prebuilt macOS binaries are currently built for Apple Silicon (arm64). For Intel Macs, build from source using: make build"
  fi
}

# --- Tool Checks ---
check_tools() {
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    abort "curl or wget is required to download seek."
  fi
  if ! command -v tar >/dev/null 2>&1; then
    abort "tar is required to extract seek."
  fi
}

download_file() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --progress-bar "$url" -o "$dest"
  else
    wget -q --show-progress "$url" -O "$dest"
  fi
}

# --- Version Resolution ---
resolve_version() {
  if [ -n "$SEEK_VERSION" ]; then
    VERSION="$SEEK_VERSION"
    return
  fi

  info "Checking for latest release..."
  # Resolve redirect location (fast, avoids GitHub API rate limits)
  if command -v curl >/dev/null 2>&1; then
    VERSION=$(curl -sIL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" 2>/dev/null | sed -E 's|.*/tag/||')
  fi

  # Fallback to API if redirect check failed
  if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
    if command -v curl >/dev/null 2>&1; then
      VERSION=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
      VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    fi
  fi

  if [ -z "$VERSION" ]; then
    abort "Could not determine latest version from GitHub. Specify with SEEK_VERSION=vX.Y.Z"
  fi
}

# --- Checksum Verification ---
verify_checksum() {
  archive_file="$1"
  checksum_file="$2"
  archive_name="$(basename "$archive_file")"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$archive_file")" && grep "  ${archive_name}\$" "$(basename "$checksum_file")" 2>/dev/null | sha256sum -c - >/dev/null 2>&1) || \
    (cd "$(dirname "$archive_file")" && grep "${archive_name}" "$(basename "$checksum_file")" 2>/dev/null | sha256sum -c - >/dev/null 2>&1)
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$(dirname "$archive_file")" && grep "${archive_name}" "$(basename "$checksum_file")" 2>/dev/null | shasum -a 256 -c - >/dev/null 2>&1)
  else
    warn "Neither sha256sum nor shasum found; skipping checksum verification."
    return 0
  fi
}

# --- Installation Directory ---
determine_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    TARGET_DIR="$INSTALL_DIR"
  elif [ -w "/usr/local/bin" ]; then
    TARGET_DIR="/usr/local/bin"
  elif [ "$(id -u)" -eq 0 ]; then
    TARGET_DIR="/usr/local/bin"
  else
    TARGET_DIR="${HOME}/.local/bin"
  fi
  mkdir -p "$TARGET_DIR"
}

# --- Main Flow ---
main() {
  setup_colors
  detect_platform
  check_tools
  resolve_version
  determine_install_dir

  ARCHIVE_NAME="seek_${VERSION}_${OS}-${ARCH}.tar.gz"
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
  CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/SHA256SUMS.txt"

  info "Installing seek ${VERSION} (${OS}/${ARCH})..."

  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

  info "Downloading ${DOWNLOAD_URL}..."
  download_file "$DOWNLOAD_URL" "${TMP_DIR}/${ARCHIVE_NAME}"
  download_file "$CHECKSUM_URL" "${TMP_DIR}/SHA256SUMS.txt" || true

  if [ -f "${TMP_DIR}/SHA256SUMS.txt" ]; then
    info "Verifying checksum..."
    if ! verify_checksum "${TMP_DIR}/${ARCHIVE_NAME}" "${TMP_DIR}/SHA256SUMS.txt"; then
      abort "SHA256 checksum verification failed! Aborting."
    fi
    success "Checksum verified."
  fi

  info "Extracting..."
  tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "$TMP_DIR"

  # Find the seek binary inside the extracted tree
  BIN_PATH=$(find "$TMP_DIR" -name "$BINARY_NAME" -type f | head -n 1)
  if [ -z "$BIN_PATH" ] || [ ! -f "$BIN_PATH" ]; then
    abort "Could not find '${BINARY_NAME}' binary in downloaded archive."
  fi

  chmod +x "$BIN_PATH"
  mv "$BIN_PATH" "${TARGET_DIR}/${BINARY_NAME}"
  success "Installed seek to ${TARGET_DIR}/${BINARY_NAME}"

  # Verify executable
  INSTALLED_VER=$("${TARGET_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo "$VERSION")
  printf "\n${GREEN}${BOLD}seek ${INSTALLED_VER} installed successfully!${RESET}\n\n"

  # PATH check and guidance
  case ":$PATH:" in
    *":$TARGET_DIR:"*) ;;
    *)
      warn "${TARGET_DIR} is not in your PATH environment variable."
      printf "Add it by running the following command for your shell:\n\n"
      case "$SHELL" in
        */zsh)
          printf "  echo 'export PATH=\"%s:\$PATH\"' >> ~/.zshrc\n" "$TARGET_DIR"
          printf "  source ~/.zshrc\n\n"
          ;;
        */fish)
          printf "  fish_add_path %s\n\n" "$TARGET_DIR"
          ;;
        *)
          printf "  echo 'export PATH=\"%s:\$PATH\"' >> ~/.bashrc\n" "$TARGET_DIR"
          printf "  source ~/.bashrc\n\n"
          ;;
      esac
      ;;
  esac

  printf "Run '${BOLD}seek --help${RESET}' to get started!\n"
}

main "$@"
