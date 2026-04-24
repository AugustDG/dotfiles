# Runs for every shell invocation — keep minimal.
# Prefer .zprofile for login-shell env and .zshrc for interactive setup.

[[ -r "$HOME/.cargo/env" ]] && . "$HOME/.cargo/env"
