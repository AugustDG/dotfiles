. "$HOME/.cargo/env"

# Core PATH
export PATH="$HOME/bin:$PATH"
export PATH="$PATH:$HOME/go/bin"
export PATH="$PATH:$HOME/.local/bin"

# Homebrew — try both Apple Silicon and Intel prefixes
for brew_prefix in /opt/homebrew /usr/local /home/linuxbrew/.linuxbrew "$HOME/.linuxbrew"; do
  [[ -x "$brew_prefix/bin/brew" ]] && export PATH="$PATH:$brew_prefix/bin" && break
done

# pnpm
if [[ "$OSTYPE" == darwin* ]]; then
  export PNPM_HOME="$HOME/Library/pnpm"
else
  export PNPM_HOME="$HOME/.local/share/pnpm"
fi
[[ ":$PATH:" != *":$PNPM_HOME:"* ]] && export PATH="$PNPM_HOME:$PATH"

# bun
export BUN_INSTALL="$HOME/.bun"
[[ ":$PATH:" != *":$BUN_INSTALL/bin:"* ]] && export PATH="$BUN_INSTALL/bin:$PATH"
