#!/bin/sh
# seek installer script for Linux and macOS
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ozgurulukir/seek/main/install.sh | sh
#
# Fail-closed guarantees:
#   - checksum file is mandatory; a missing/unverifiable checksum aborts
#   - archive contents are validated (paths, symlinks, single binary) before
#     anything is extracted
#   - the installed binary is smoke-tested; failure restores the previous
#     binary and aborts — the installer never reports success for a broken
#     install
#
# Environment overrides:
#   SEEK_VERSION=vX.Y.Z     install a specific release
#   SEEK_DOWNLOAD_BASE=URL  alternate release root (e.g. an internal mirror;
#                           release assets live directly under this URL)
#   INSTALL_DIR=/path       installation directory

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
  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    abort "sha256sum or shasum is required to verify the download. Install one and retry — checksums are mandatory."
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

# --- Checksum Verification (mandatory, fail-closed) ---
verify_checksum() {
  archive_file="$1"
  checksum_file="$2"
  archive_name="$(basename "$archive_file")"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$archive_file")" && grep "  ${archive_name}\$" "$(basename "$checksum_file")" | sha256sum -c - >/dev/null)
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$(dirname "$archive_file")" && grep "  ${archive_name}\$" "$(basename "$checksum_file")" | shasum -a 256 -c - >/dev/null)
  fi
}

# --- Archive Pre-Validation (before extraction) ---
# Rejects absolute paths, path traversal, symlinks, hardlinks, devices, and
# archives that do not contain exactly one regular file named "seek".
validate_archive() {
  archive_file="$1"

  if ! tar -tzf "$archive_file" > "${TMP_DIR}/.entries" 2>/dev/null; then
    abort "Downloaded file is not a valid gzip tar archive."
  fi

  if ! tar -tvzf "$archive_file" > "${TMP_DIR}/.listing" 2>/dev/null; then
    abort "Cannot inspect archive contents."
  fi

  if grep -qE '^l' "${TMP_DIR}/.listing"; then
    abort "Archive contains symlinks; refusing to extract."
  fi
  if grep -qE '^h' "${TMP_DIR}/.listing"; then
    abort "Archive contains hardlinks; refusing to extract."
  fi
  # Non-file, non-directory entry types (block/char devices, fifos, sockets)
  if grep -qvE '^[-d]' "${TMP_DIR}/.listing"; then
    abort "Archive contains unexpected entry types; refusing to extract."
  fi

  if grep -E '(^/|(^|/)\.\.(/|$))' "${TMP_DIR}/.entries" >/dev/null; then
    abort "Archive contains absolute or traversal paths; refusing to extract."
  fi

  bin_entries=$(grep -E '(^|/)seek$' "${TMP_DIR}/.entries" | grep -v '/$' || true)
  bin_count=$(printf '%s' "$bin_entries" | grep -c . || true)
  if [ "$bin_count" -ne 1 ]; then
    abort "Expected exactly one 'seek' binary in the archive, found ${bin_count:-0}."
  fi
  BIN_REL="$bin_entries"
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
  if [ -n "$SEEK_DOWNLOAD_BASE" ]; then
    DL_BASE="${SEEK_DOWNLOAD_BASE%/}"
  else
    DL_BASE="https://github.com/${REPO}/releases/download/${VERSION}"
  fi
  DOWNLOAD_URL="${DL_BASE}/${ARCHIVE_NAME}"
  CHECKSUM_URL="${DL_BASE}/SHA256SUMS.txt"

  info "Installing seek ${VERSION} (${OS}/${ARCH})..."

  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

  info "Downloading ${DOWNLOAD_URL}..."
  download_file "$DOWNLOAD_URL" "${TMP_DIR}/${ARCHIVE_NAME}"
  info "Downloading ${CHECKSUM_URL}..."
  # Fail-closed: no checksum file means no install.
  if ! download_file "$CHECKSUM_URL" "${TMP_DIR}/SHA256SUMS.txt"; then
    abort "Could not download SHA256SUMS.txt. Refusing to install without checksum verification."
  fi

  info "Verifying checksum..."
  if ! verify_checksum "${TMP_DIR}/${ARCHIVE_NAME}" "${TMP_DIR}/SHA256SUMS.txt"; then
    abort "SHA256 checksum verification failed! Aborting."
  fi
  success "Checksum verified."

  info "Validating archive contents..."
  validate_archive "${TMP_DIR}/${ARCHIVE_NAME}"
  success "Archive validated (no symlinks/traversal, single binary)."

  info "Extracting..."
  tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "$TMP_DIR"

  BIN_PATH="${TMP_DIR}/${BIN_REL}"
  if [ ! -f "$BIN_PATH" ]; then
    abort "Binary disappeared after extraction: ${BIN_REL}"
  fi

  # Keep the previous binary for rollback until the new one passes a smoke test.
  TARGET_BIN="${TARGET_DIR}/${BINARY_NAME}"
  BACKUP_BIN="${TARGET_BIN}.bak.$$"
  if [ -e "$TARGET_BIN" ]; then
    if ! cp "$TARGET_BIN" "$BACKUP_BIN"; then
      abort "Could not back up existing binary at ${TARGET_BIN}."
    fi
  fi

  chmod +x "$BIN_PATH"
  if ! mv "$BIN_PATH" "$TARGET_BIN"; then
    rm -f "$BACKUP_BIN"
    abort "Could not move binary into ${TARGET_DIR}."
  fi

  # Smoke test: the binary must actually run. A wrong/absent version string is
  # a warning; a binary that cannot execute is a hard failure with rollback.
  if INSTALLED_VER=$("$TARGET_BIN" --version 2>/dev/null); then
    case "$INSTALLED_VER" in
      *"$VERSION"*) : ;;
      *) warn "Installed binary reports '${INSTALLED_VER}' but ${VERSION} was requested." ;;
    esac
    rm -f "$BACKUP_BIN"
  else
    if [ -e "$BACKUP_BIN" ]; then
      if mv "$BACKUP_BIN" "$TARGET_BIN"; then
        warn "Previous binary restored."
      fi
    else
      rm -f "$TARGET_BIN"
    fi
    abort "Installed binary failed to run (--version); installation rolled back. The release artifact is broken — report it at https://github.com/${REPO}/issues"
  fi

  success "Installed seek to ${TARGET_BIN}"

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
