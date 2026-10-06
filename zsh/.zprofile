# Runs once per login shell, before .zshrc.
# Toolchain initialization and machine-local overrides.

# --- Homebrew (macOS Apple Silicon/Intel + Linuxbrew) ---
for brew_prefix in /opt/homebrew /usr/local /home/linuxbrew/.linuxbrew "$HOME/.linuxbrew"; do
  if [[ -x "$brew_prefix/bin/brew" ]]; then
    eval "$($brew_prefix/bin/brew shellenv)"
    break
  fi
done

# brew shellenv prepends Homebrew's site-functions to fpath. Its _git is a
# wrapper around the bash completion script and shadows zsh's native _git,
# breaking the git compdefs in .zshrc. Demote it so native completions win;
# Homebrew completions without a native counterpart still resolve.
if [[ -n "${HOMEBREW_PREFIX:-}" ]]; then
  fpath=(${fpath:#$HOMEBREW_PREFIX/share/zsh/site-functions} "$HOMEBREW_PREFIX/share/zsh/site-functions")
fi

# --- Editor ---
export EDITOR=nvim

# --- atuin PATH (the `atuin init zsh` call lives in .zshrc) ---
[[ -r "$HOME/.atuin/bin/env" ]] && . "$HOME/.atuin/bin/env"

# --- mise: shims serve scripts and `zsh -c`; .zshrc's activate takes over at a prompt ---
command -v mise >/dev/null 2>&1 && eval "$(mise activate zsh --shims)"

# --- Machine-local exports and secrets (never tracked) ---
[[ -r "$HOME/.zprofile.local" ]] && source "$HOME/.zprofile.local"

# Added by OrbStack: command-line tools and integration
# This won't be added again if you remove it.
source ~/.orbstack/shell/init.zsh 2>/dev/null || :

# --- ~/.local/bin first: /etc/zprofile's path_helper put .zshenv's entry behind
# the system dirs ---
path=("$HOME/.local/bin" ${path:#$HOME/.local/bin})
