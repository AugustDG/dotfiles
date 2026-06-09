# dotfiles

Personal dotfiles for macOS (Apple Silicon / Intel) and Linux. Configs are laid
out as [GNU stow](https://www.gnu.org/software/stow/) packages managed by a Go
CLI with an interactive TUI.

## Quick install

```bash
curl -sL https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash
```

This downloads the pre-built `dotfiles` CLI, then runs `dotfiles install` which
bootstraps Homebrew, installs the toolchain, clones this repo, and presents an
interactive module picker.

## CLI usage

```bash
dotfiles install              # Interactive TUI — pick modules to install
dotfiles install --all        # Install all OS-compatible modules
dotfiles install nvim tmux    # Install specific modules
dotfiles install --skip-bootstrap  # Skip brew/gh/clone steps

dotfiles uninstall nvim tmux  # Unstow modules from $HOME
dotfiles status               # Show which modules are stowed
dotfiles update               # Pull latest submodules and re-stow
dotfiles update tmux nvim     # Update specific modules

dotfiles sync                 # Commit and push local changes, submodules first
dotfiles sync tmux -m "msg"   # Sync specific modules with a commit message
dotfiles sync --dry-run       # Show what would be committed and pushed
```

## Layout

```
zsh/       → ~/.zshenv, ~/.zprofile, ~/.zshrc
git/       → ~/.gitconfig
claude/    → ~/.claude/{CLAUDE.md, settings.json, skills/}
nvim/      → ~/.config/nvim      (submodule AugustDG/nvim-config)
tmux/      → ~/.config/tmux      (submodule AugustDG/tmux-config)
yazi/      → ~/.config/yazi      (submodule AugustDG/yazi-config)
opencode/  → ~/.config/opencode  (submodule AugustDG/opencode-config)
zed/       → ~/.config/zed       (submodule AugustDG/zed-config)
i3/        → ~/.i3, ~/.config/dunst   (submodule AugustDG/i3-config — Linux only)
```

Each module has a `module.toml` declaring its brew dependencies, OS support,
and post-install hooks.

## Machine-local secrets

`.zprofile` sources `~/.zshrc.local` if it exists. Put machine-specific
exports there (e.g. `CLOUD_PAT`) — the file is never tracked.

## Development

```bash
go build -o dotfiles ./cmd/dotfiles
./dotfiles status
```

Releases are built by GitHub Actions on tag push (`v*`). Binaries for
darwin/arm64, darwin/amd64, and linux/amd64 are attached to each release.
