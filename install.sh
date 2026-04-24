#!/usr/bin/env bash
# Bootstrap installer for AugustDG/dotfiles.
#
# macOS + Linux via Homebrew. Installs brew, zsh, git, gh, stow, go,
# authenticates gh (SSH), clones the dotfiles repo, then hands off to install.zsh.
#
# Usage:
#   curl -s https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash
#   # or, from a checkout:
#   ./install.sh
#
# Env knobs:
#   DOTFILES_DIR             Override clone destination (default: ~/projects/dotfiles)
#   DOTFILES_SKIP_GH_AUTH=1  Skip gh auth login (for CI / re-runs)
#   DOTFILES_SKIP_CHSH=1     Skip changing the login shell

set -euo pipefail

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m  %s\n' "$*" >&2; }
die()  { printf '\033[1;31mxx\033[0m  %s\n' "$*" >&2; exit 1; }

DOTFILES_DIR="${DOTFILES_DIR:-$HOME/projects/dotfiles}"
DOTFILES_REPO_SSH="git@github.com:AugustDG/dotfiles.git"
DOTFILES_REPO_HTTPS="https://github.com/AugustDG/dotfiles.git"

# --- 1. Detect OS ----------------------------------------------------------
case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux)  OS=linux ;;
  *)      die "Unsupported OS: $(uname -s)" ;;
esac
log "Detected OS: $OS"

# --- 2. Install Homebrew ---------------------------------------------------
if ! command -v brew >/dev/null 2>&1; then
  log "Installing Homebrew"
  if [[ "$OS" == linux ]]; then
    # Linuxbrew needs build-essential/procps/curl/file/git on Debian-ish systems.
    if command -v apt-get >/dev/null 2>&1; then
      sudo apt-get update
      sudo apt-get install -y build-essential procps curl file git
    fi
  fi
  NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi

# Pick up brew on PATH for the rest of this script
for brew_prefix in /opt/homebrew /usr/local /home/linuxbrew/.linuxbrew "$HOME/.linuxbrew"; do
  if [[ -x "$brew_prefix/bin/brew" ]]; then
    eval "$($brew_prefix/bin/brew shellenv)"
    break
  fi
done
command -v brew >/dev/null 2>&1 || die "brew not on PATH after install"

# --- 3. Install core toolchain --------------------------------------------
# Only install packages that aren't already present.
core_packages=(zsh git gh stow go)
to_install=()
for pkg in "${core_packages[@]}"; do
  if brew list --formula "$pkg" >/dev/null 2>&1; then
    log "already installed: $pkg"
  else
    to_install+=("$pkg")
  fi
done
if (( ${#to_install[@]} > 0 )); then
  log "Installing: ${to_install[*]}"
  brew install "${to_install[@]}"
else
  log "All core packages already installed"
fi

# --- 4. GitHub auth (needed before cloning because submodules use SSH) -----
if [[ "${DOTFILES_SKIP_GH_AUTH:-0}" != "1" ]]; then
  if gh auth status --hostname github.com >/dev/null 2>&1; then
    log "gh already authenticated"
  else
    log "Authenticating gh (SSH). A browser window will open."
    gh auth login --hostname github.com --git-protocol ssh --web
  fi
  gh auth setup-git
else
  warn "Skipping gh auth (DOTFILES_SKIP_GH_AUTH=1)"
fi

# --- 5. Clone / update dotfiles repo --------------------------------------
if [[ -d "$DOTFILES_DIR/.git" ]]; then
  log "Dotfiles already at $DOTFILES_DIR — pulling + updating submodules"
  git -C "$DOTFILES_DIR" pull --ff-only || warn "git pull failed; continuing"
  git -C "$DOTFILES_DIR" submodule update --init --recursive
else
  mkdir -p "$(dirname "$DOTFILES_DIR")"
  log "Cloning dotfiles into $DOTFILES_DIR"
  if ! git clone --recurse-submodules "$DOTFILES_REPO_SSH" "$DOTFILES_DIR" 2>/dev/null; then
    warn "SSH clone failed, falling back to HTTPS (submodules may still need SSH auth)"
    git clone --recurse-submodules "$DOTFILES_REPO_HTTPS" "$DOTFILES_DIR"
  fi
fi

# --- 6. Resolve zsh + chsh -------------------------------------------------
# Prefer brew's zsh (usually newer than the system one) but fall back to
# anything on PATH so macOS /bin/zsh, distro-packaged zsh, etc. all work.
ZSH_BIN=""
for candidate in "$(brew --prefix)/bin/zsh" "$(command -v zsh || true)" /bin/zsh /usr/bin/zsh; do
  if [[ -n "$candidate" && -x "$candidate" ]]; then
    ZSH_BIN="$candidate"
    break
  fi
done
[[ -n "$ZSH_BIN" ]] || die "no usable zsh found on PATH after install"
log "Using zsh at $ZSH_BIN"

if [[ "${DOTFILES_SKIP_CHSH:-0}" != "1" ]]; then
  if ! grep -Fxq "$ZSH_BIN" /etc/shells 2>/dev/null; then
    log "Adding $ZSH_BIN to /etc/shells (sudo)"
    echo "$ZSH_BIN" | sudo tee -a /etc/shells >/dev/null
  fi
  if [[ "$SHELL" != "$ZSH_BIN" ]]; then
    log "Changing login shell to $ZSH_BIN"
    chsh -s "$ZSH_BIN" || warn "chsh failed; set your shell manually"
  fi
fi

# --- 7. Hand off to stage 2 ------------------------------------------------
log "Handing off to install.zsh"
exec "$ZSH_BIN" "$DOTFILES_DIR/install.zsh" "$@"
