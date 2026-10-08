#!/bin/sh
# Install ctxed without cloning the repo:
#
#   curl -fsSL https://raw.githubusercontent.com/simranjeetc/ctxed/main/install.sh | sh
#
# Options (as env vars):
#   CTXED_VERSION   release tag to install (default: latest)
#   CTXED_BIN_DIR   where to put the binary (default: ~/.local/bin)
#   CTXED_NO_SKILL  set to 1 to skip installing the ctxed-overview skill
set -eu

REPO="simranjeetc/ctxed"
BIN_DIR="${CTXED_BIN_DIR:-$HOME/.local/bin}"
VERSION="${CTXED_VERSION:-latest}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) echo "ctxed: unsupported OS: $os" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "ctxed: unsupported architecture: $arch" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  base="https://github.com/$REPO/releases/latest/download"
  tag="latest"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
  tag="$VERSION"
fi

asset="ctxed_${tag}_${os}_${arch}.tar.gz"
if [ "$tag" = "latest" ]; then
  # Release assets are versioned; resolve the real tag from the redirect.
  resolved=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
  ver=${resolved##*/}
  asset="ctxed_${ver}_${os}_${arch}.tar.gz"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset ..."
curl -fsSL "$base/$asset" -o "$tmp/ctxed.tar.gz"
tar -xzf "$tmp/ctxed.tar.gz" -C "$tmp"

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/ctxed" "$BIN_DIR/ctxed"
echo "Installed $BIN_DIR/ctxed"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "Add $BIN_DIR to PATH, e.g. export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

if [ "${CTXED_NO_SKILL:-0}" != "1" ] && [ -f "$tmp/skills/ctxed-overview/SKILL.md" ]; then
  if [ -d "$HOME/.claude" ]; then
    mkdir -p "$HOME/.claude/skills/ctxed-overview"
    cp "$tmp/skills/ctxed-overview/SKILL.md" "$HOME/.claude/skills/ctxed-overview/SKILL.md"
    echo "Installed Claude Code skill: ~/.claude/skills/ctxed-overview"
  fi
  if [ -d "$HOME/.config/opencode" ]; then
    mkdir -p "$HOME/.config/opencode/skills/ctxed-overview"
    cp "$tmp/skills/ctxed-overview/SKILL.md" "$HOME/.config/opencode/skills/ctxed-overview/SKILL.md"
    echo "Installed OpenCode skill: ~/.config/opencode/skills/ctxed-overview"
  fi
fi

echo "Run: ctxed overview"
