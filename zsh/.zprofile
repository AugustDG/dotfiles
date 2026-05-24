# Runs once per login shell, before .zshrc.
# Toolchain initialization and machine-local overrides.

# --- Homebrew (macOS Apple Silicon/Intel + Linuxbrew) ---
for brew_prefix in /opt/homebrew /usr/local /home/linuxbrew/.linuxbrew "$HOME/.linuxbrew"; do
  if [[ -x "$brew_prefix/bin/brew" ]]; then
    eval "$($brew_prefix/bin/brew shellenv)"
    break
  fi
done

# --- Editor ---
export EDITOR=nvim

# --- nvm (installed via Homebrew) ---
export NVM_DIR="$HOME/.nvm"
if [[ -n "${HOMEBREW_PREFIX:-}" && -s "$HOMEBREW_PREFIX/opt/nvm/nvm.sh" ]]; then
  source "$HOMEBREW_PREFIX/opt/nvm/nvm.sh"
fi

# --- atuin PATH (the `atuin init zsh` call lives in .zshrc) ---
[[ -r "$HOME/.atuin/bin/env" ]] && . "$HOME/.atuin/bin/env"

# --- Misc user-local env (e.g. uv-installed shims) ---
[[ -r "$HOME/.local/bin/env" ]] && . "$HOME/.local/bin/env"

# --- Machine-local overrides / secrets (never tracked) ---
[[ -r "$HOME/.zshrc.local" ]] && source "$HOME/.zshrc.local"
