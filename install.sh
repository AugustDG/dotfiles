#!/usr/bin/env bash
# Bootstrap installer for AugustDG/dotfiles.
#
# Downloads the pre-built dotfiles CLI binary for this platform from the latest
# GitHub release and runs `dotfiles install`.
#
# Usage:
#   curl -sL https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash

set -euo pipefail

REPO="AugustDG/dotfiles"

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx\033[0m  %s\n' "$*" >&2; exit 1; }

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       die "Unsupported architecture: $ARCH" ;;
esac

BINARY="dotfiles-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/latest/${BINARY}"

INSTALL_DIR="${DOTFILES_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"

log "Downloading $BINARY..."
TMPFILE="$(mktemp)"
if ! curl -fsSL "$URL" -o "$TMPFILE"; then
  rm -f "$TMPFILE"
  die "Failed to download from $URL"
fi
mv "$TMPFILE" "${INSTALL_DIR}/dotfiles"
chmod +x "${INSTALL_DIR}/dotfiles"

export PATH="$INSTALL_DIR:$PATH"

log "Running dotfiles install..."
exec dotfiles install
