#!/usr/bin/env sh
# ctxed installer — https://github.com/simranjeetc/ctxed
#
#   curl -fsSL https://github.com/simranjeetc/ctxed/releases/latest/download/install.sh | sh
#
# Environment variables:
#   CTXED_VERSION      release tag to install (e.g. "0.1.0" or "latest"; default: latest)
#   CTXED_INSTALL_DIR  target directory for the binary (default: first writable PATH dir, else ~/.local/bin)
#   CTXED_REPO         GitHub repository (default: simranjeetc/ctxed)
#   CTXED_NO_SKILL     set to 1 to skip installing the ctxed-overview skill
set -eu

REPO="${CTXED_REPO:-simranjeetc/ctxed}"
VERSION="${CTXED_VERSION:-latest}"

BOLD="\033[1m"; GREEN="\033[38;5;71m"; AMBER="\033[38;5;208m"; RED="\033[38;5;167m"; RESET="\033[0m"
info()  { printf " ${GREEN}✓${RESET} %b\n" "$1"; }
step()  { printf " ${AMBER}›${RESET} %b\n" "$1"; }
warn()  { printf " ${AMBER}!${RESET} %b\n" "$1"; }
die()   { printf " ${RED}✗${RESET} %b\n" "$1" >&2; exit 1; }

# --- HTTP client ------------------------------------------------------------
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 30 --retry 2 "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- -T 15 -t 3 "$1"
  else
    die "Neither curl nor wget found in PATH. Install one and retry."
  fi
}

fetch_file() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 300 --retry 2 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 30 -t 3 -O "$2" "$1"
  else
    die "Neither curl nor wget found in PATH. Install one and retry."
  fi
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then openssl dgst -sha256 "$1" | awk '{print $NF}'
  else printf ''; fi
}

# --- Platform ---------------------------------------------------------------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux*)  OS="linux" ;;
  darwin*) OS="darwin" ;;
  *) die "Unsupported operating system: $OS (ctxed ships linux and darwin builds)" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) die "Unsupported architecture: $ARCH (ctxed ships amd64 and arm64 builds)" ;;
esac

# --- Resolve version --------------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  step "Resolving the latest release of ${BOLD}${REPO}${RESET}..."
  JSON="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)"
  if [ -n "$JSON" ]; then
    VERSION="$(printf '%s' "$JSON" | grep '"tag_name":' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' | sed 's/^v//')"
  fi
  [ -n "$VERSION" ] && [ "$VERSION" != "latest" ] \
    || die "Could not determine the latest version. Check your network, or pass CTXED_VERSION=x.y.z."
fi

ASSET="ctxed_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/v${VERSION}"

# --- Install directory (cascading; sudo only as a last resort) --------------
USE_SUDO=0
TARGET_DIR=""
is_writable() {
  [ -n "$1" ] || return 1
  mkdir -p "$1" 2>/dev/null || return 1
  test_file="$1/.ctxed_test_$$"
  if ( : > "$test_file" ) 2>/dev/null; then rm -f "$test_file" 2>/dev/null; return 0; fi
  return 1
}

if [ -n "${CTXED_INSTALL_DIR:-}" ]; then
  is_writable "$CTXED_INSTALL_DIR" && TARGET_DIR="$CTXED_INSTALL_DIR"
elif [ -n "${INSTALL_DIR:-}" ]; then
  is_writable "$INSTALL_DIR" && TARGET_DIR="$INSTALL_DIR"
fi

# Prefer a writable directory that is already on PATH.
if [ -z "$TARGET_DIR" ]; then
  for d in "${HOME:-}/.local/bin" "${HOME:-}/bin" "/usr/local/bin"; do
    [ -n "$d" ] || continue
    case ":${PATH:-}:" in
      *":$d:"*) is_writable "$d" && { TARGET_DIR="$d"; break; } ;;
    esac
  done
fi

if [ -z "$TARGET_DIR" ]; then
  if [ -n "${HOME:-}" ] && is_writable "${HOME}/.local/bin"; then TARGET_DIR="${HOME}/.local/bin"
  elif is_writable "/usr/local/bin"; then TARGET_DIR="/usr/local/bin"
  fi
fi

if [ -z "$TARGET_DIR" ]; then
  if command -v sudo >/dev/null 2>&1 && [ -d "/usr/local/bin" ]; then
    USE_SUDO=1; TARGET_DIR="/usr/local/bin"
  else
    die "No writable install directory found. Set CTXED_INSTALL_DIR."
  fi
fi

TARGET_BIN="${TARGET_DIR}/ctxed"

# --- Existing install -------------------------------------------------------
ACTION="Installed"
if [ -f "$TARGET_BIN" ] || [ -L "$TARGET_BIN" ]; then
  CURRENT_VER=""
  [ -x "$TARGET_BIN" ] && CURRENT_VER="$("$TARGET_BIN" version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n 1 || true)"
  if [ -n "$CURRENT_VER" ]; then
    step "Found existing ctxed v${CURRENT_VER} at ${BOLD}${TARGET_BIN}${RESET}"
    ACTION="Updated"
  else
    step "Replacing existing binary at ${BOLD}${TARGET_BIN}${RESET}"
    ACTION="Replaced"
  fi
fi

# --- Download + verify ------------------------------------------------------
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t ctxedinstall)"
trap 'rm -rf "$TMP_DIR"' EXIT

step "Downloading ${ASSET}..."
fetch_file "${BASE}/${ASSET}" "${TMP_DIR}/${ASSET}" 2>/dev/null \
  || die "Failed to download ${BASE}/${ASSET}"

if fetch_file "${BASE}/checksums.txt" "${TMP_DIR}/checksums.txt" 2>/dev/null; then
  WANT="$(grep " ${ASSET}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}' | head -n 1)"
  GOT="$(sha256_of "${TMP_DIR}/${ASSET}")"
  if [ -n "$WANT" ] && [ -n "$GOT" ]; then
    [ "$WANT" = "$GOT" ] || die "Checksum mismatch for ${ASSET} (want ${WANT}, got ${GOT})"
    info "Checksum verified"
  else
    warn "Skipping checksum verification (no sha256 tool or no entry)"
  fi
fi

tar -xzf "${TMP_DIR}/${ASSET}" -C "${TMP_DIR}"
[ -f "${TMP_DIR}/ctxed" ] || die "Archive did not contain a ctxed binary"

# --- Install ----------------------------------------------------------------
step "Installing to ${BOLD}${TARGET_BIN}${RESET}"
if [ "$USE_SUDO" = "1" ]; then
  sudo mkdir -p "$TARGET_DIR"
  sudo cp "${TMP_DIR}/ctxed" "$TARGET_BIN"
  sudo chmod 0755 "$TARGET_BIN"
else
  mkdir -p "$TARGET_DIR"
  cp "${TMP_DIR}/ctxed" "$TARGET_BIN"
  chmod 0755 "$TARGET_BIN"
fi

if [ "$ACTION" = "Updated" ]; then
  info "ctxed updated (v${CURRENT_VER} -> v${VERSION})"
else
  info "ctxed v${VERSION} installed"
fi

# --- PATH -------------------------------------------------------------------
PATH_CONFIGURED=0
case ":${PATH:-}:" in *":$TARGET_DIR:"*) PATH_CONFIGURED=1 ;; esac

detect_shell_rc() {
  case "$(basename "${SHELL:-}")" in
    zsh) echo "${HOME}/.zshrc" ;;
    bash) if [ -f "${HOME}/.bashrc" ]; then echo "${HOME}/.bashrc"; else echo "${HOME}/.bash_profile"; fi ;;
    fish) echo "${HOME}/.config/fish/config.fish" ;;
    *) if [ -f "${HOME}/.zshrc" ]; then echo "${HOME}/.zshrc"; elif [ -f "${HOME}/.bashrc" ]; then echo "${HOME}/.bashrc"; else echo "${HOME}/.profile"; fi ;;
  esac
}

if [ "$PATH_CONFIGURED" = "0" ]; then
  warn "${TARGET_DIR} is not on your PATH."
  RC="$(detect_shell_rc)"
  if [ -n "$RC" ]; then
    if grep -Fq "$TARGET_DIR" "$RC" 2>/dev/null; then
      info "PATH entry already present in ${RC}"
    else
      printf '\n# Added by the ctxed installer\nexport PATH="%s:$PATH"\n' "$TARGET_DIR" >> "$RC"
      info "Added ${TARGET_DIR} to PATH in ${RC} (restart your shell)"
    fi
  else
    printf '   Add it yourself: export PATH="%s:$PATH"\n' "$TARGET_DIR"
  fi
fi

# --- Skill ------------------------------------------------------------------
if [ "${CTXED_NO_SKILL:-0}" != "1" ] && [ -f "${TMP_DIR}/skills/ctxed-overview/SKILL.md" ]; then
  if [ -d "${HOME}/.claude" ]; then
    mkdir -p "${HOME}/.claude/skills/ctxed-overview"
    cp "${TMP_DIR}/skills/ctxed-overview/SKILL.md" "${HOME}/.claude/skills/ctxed-overview/SKILL.md"
    info "Installed Claude Code skill: ~/.claude/skills/ctxed-overview"
  fi
  if [ -d "${HOME}/.config/opencode" ]; then
    mkdir -p "${HOME}/.config/opencode/skills/ctxed-overview"
    cp "${TMP_DIR}/skills/ctxed-overview/SKILL.md" "${HOME}/.config/opencode/skills/ctxed-overview/SKILL.md"
    info "Installed OpenCode skill: ~/.config/opencode/skills/ctxed-overview (restart OpenCode)"
  fi
fi

printf '\nRun %b to see what your session'"'"'s context is made of:\n' "${BOLD}ctxed${RESET}"
printf '  %bctxed overview%b   # or ask your agent "what'"'"'s in my context?"\n\n' "$AMBER" "$RESET"
