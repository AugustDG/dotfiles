#!/usr/bin/env zsh
# Stage-2 installer for AugustDG/dotfiles. Called by install.sh.
#
# Installs the full toolchain, builds + installs hopper, creates
# ~/.zshrc.local template, backs up existing rc files, and stows
# every package into $HOME.
#
# Env knobs:
#   STOW_I3=1      Also stow the i3 package (Linux only)
#   SKIP_LAZY=1    Skip `nvim --headless Lazy! sync`

set -euo pipefail

log()  { print -P "%F{blue}==>%f $*"; }
warn() { print -P "%F{yellow}!!%f  $*" >&2; }
die()  { print -P "%F{red}xx%f  $*" >&2; exit 1; }

# --- brew on PATH (in case this is invoked standalone) ---------------------
for brew_prefix in /opt/homebrew /usr/local /home/linuxbrew/.linuxbrew "$HOME/.linuxbrew"; do
  if [[ -x "$brew_prefix/bin/brew" ]]; then
    eval "$($brew_prefix/bin/brew shellenv)"
    break
  fi
done
command -v brew >/dev/null 2>&1 || die "brew not on PATH"

DOTFILES_DIR="${DOTFILES_DIR:-${${(%):-%x}:A:h}}"
[[ -d "$DOTFILES_DIR" ]] || die "DOTFILES_DIR does not exist: $DOTFILES_DIR"
log "Using dotfiles at $DOTFILES_DIR"

# --- 1. Toolchain ----------------------------------------------------------
log "Installing toolchain via brew"
brew install \
  tmux \
  neovim \
  yazi \
  fzf \
  atuin \
  oh-my-posh \
  bun \
  nvm \
  pnpm \
  node \
  ripgrep \
  fd \
  jq

# --- 2. znap ---------------------------------------------------------------
if [[ ! -d "$HOME/.plugins/znap" ]]; then
  log "Cloning znap into ~/.plugins/znap"
  mkdir -p "$HOME/.plugins"
  git clone --depth 1 https://github.com/marlonrichert/zsh-snap.git "$HOME/.plugins/znap"
fi

# --- 3. hopper (AugustDG/hopper) ------------------------------------------
if ! command -v hopper >/dev/null 2>&1; then
  log "Building + installing hopper"
  local hopper_dir="${TMPDIR:-/tmp}/hopper-install-$$"
  git clone --depth 1 https://github.com/AugustDG/hopper.git "$hopper_dir"
  # --no-shell: our tracked .zshrc already contains the hopper integration block.
  "$hopper_dir/scripts/install-common.sh" --shell zsh --no-shell
  rm -rf "$hopper_dir"
else
  log "hopper already on PATH"
fi

# --- 4. ~/.zshrc.local template (machine-local secrets, never tracked) -----
if [[ ! -f "$HOME/.zshrc.local" ]]; then
  log "Creating ~/.zshrc.local template"
  cat > "$HOME/.zshrc.local" <<'EOF'
# Machine-local overrides. Not tracked by dotfiles.
# Fill in whichever are relevant to this machine.

# export CLOUD_API_ENDPOINT=https://api.botpress.cloud
# export CLOUD_PAT=bp_pat_xxxxxxxxxxxxxxxx
# export CLOUD_BOT_ID=xxxxxxxxxxxxxxxxxxx
EOF
  chmod 600 "$HOME/.zshrc.local"
fi

# --- 5. Back up any conflicting rc files ----------------------------------
local backup_dir="$HOME/.dotfiles-backup-$(date +%Y%m%d-%H%M%S)"
local targets=(.zshrc .zshenv .zprofile .gitconfig)
local need_backup=0
for f in $targets; do
  if [[ -e "$HOME/$f" && ! -L "$HOME/$f" ]]; then
    need_backup=1
    break
  fi
done
if (( need_backup )); then
  log "Backing up existing rc files to $backup_dir"
  mkdir -p "$backup_dir"
  for f in $targets; do
    if [[ -e "$HOME/$f" && ! -L "$HOME/$f" ]]; then
      mv "$HOME/$f" "$backup_dir/"
    fi
  done
fi

# --- 6. stow packages into $HOME ------------------------------------------
local pkgs=(zsh git claude nvim tmux yazi opencode zed)
if [[ "${STOW_I3:-0}" == "1" ]]; then
  pkgs+=(i3)
fi
log "Stowing packages: ${pkgs[*]}"
stow -d "$DOTFILES_DIR" -t "$HOME" -R "${pkgs[@]}"

# --- 7. tpm + tmux plugins -------------------------------------------------
if [[ -x "$HOME/.config/tmux/plugins/tpm/bin/install_plugins" ]]; then
  log "Installing tmux plugins via tpm"
  "$HOME/.config/tmux/plugins/tpm/bin/install_plugins" || warn "tpm install returned non-zero; open tmux and press prefix+I to retry"
fi

# --- 8. nvim plugins (Lazy) ------------------------------------------------
if [[ "${SKIP_LAZY:-0}" != "1" ]] && command -v nvim >/dev/null 2>&1; then
  log "Bootstrapping nvim plugins (Lazy)"
  nvim --headless "+Lazy! sync" +qa 2>/dev/null || warn "nvim Lazy sync returned non-zero; run :Lazy sync inside nvim"
fi

# --- 9. Done ---------------------------------------------------------------
cat <<EOF

--------------------------------------------------------------------
Done. Next steps:
  1. Open a new terminal (or \`exec zsh\`) to pick up the new shell.
  2. Edit ~/.zshrc.local with any machine-specific exports (CLOUD_*).
  3. Install a Nerd Font if the prompt shows missing glyphs:
       brew install --cask font-iosevka-nerd-font
  4. Restart Terminal/iTerm/Alacritty so font changes take effect.
--------------------------------------------------------------------
EOF
