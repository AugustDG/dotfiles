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

command -v curl >/dev/null 2>&1 || die "curl is required"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *)            die "Unsupported OS: $OS" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)             die "Unsupported architecture: $ARCH" ;;
esac

BINARY="dotfiles-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/latest/${BINARY}"

INSTALL_DIR="${DOTFILES_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"

log "Downloading $BINARY..."
# Temp file lives in INSTALL_DIR so the final mv is an atomic same-filesystem
# rename.
TMPFILE="$(mktemp "${INSTALL_DIR}/.dotfiles.XXXXXX")"
trap 'rm -f "$TMPFILE"' EXIT
if ! curl -fsSL --retry 3 --connect-timeout 10 "$URL" -o "$TMPFILE"; then
  die "Failed to download from $URL"
fi
chmod +x "$TMPFILE"
mv "$TMPFILE" "${INSTALL_DIR}/dotfiles"

export PATH="$INSTALL_DIR:$PATH"

log "Running dotfiles install..."
# Under `curl | bash`, stdin is the script pipe; reattach the terminal so the
# interactive module picker can run.
if ( : </dev/tty ) 2>/dev/null; then
  exec dotfiles install </dev/tty
else
  exec dotfiles install
fi
