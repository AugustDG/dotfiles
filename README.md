# dotfiles

Personal dotfiles for macOS (Apple Silicon / Intel) and Linux. Configs are laid
out as [GNU stow](https://www.gnu.org/software/stow/) packages managed by a
single Go CLI — `dotfiles` is the one entrypoint for installing, updating,
diagnosing, and authoring your dotfiles.

## Quick install

```bash
curl -sL https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash
```

This downloads the pre-built `dotfiles` CLI, then runs `dotfiles install` which
bootstraps Homebrew, installs the toolchain, clones this repo, and presents an
interactive module picker.

## Commands

Run `dotfiles <command> --help` for full flags. `-v/--verbose` shows the
underlying command output for any command.

### Install & manage

```bash
dotfiles install                   # Interactive TUI — pick modules to install
dotfiles install --all             # Install all OS-compatible modules
dotfiles install nvim tmux         # Install specific modules
dotfiles install --skip-bootstrap  # Skip brew/gh/clone bootstrap steps

dotfiles uninstall nvim tmux       # Unstow modules from $HOME
dotfiles uninstall --all           # Unstow everything

dotfiles deps                      # Install missing deps for all modules
dotfiles deps nvim                 # …for specific modules
```

### Stay in sync

```bash
dotfiles status                    # Repo state + per-module stow/submodule/deps table
dotfiles status --check            # Exit non-zero if dirty/unpushed or links broken (for prompts/CI)

dotfiles pull                      # git pull + sync submodules + re-stow installed modules
dotfiles pull nvim tmux            # …limited to specific modules

dotfiles update                    # Bump submodules to their upstream latest, re-stow
dotfiles update tmux               # …for specific modules

dotfiles sync                      # Commit & push local changes, submodules first
dotfiles sync tmux -m "msg"        # Sync specific modules with a commit message
dotfiles sync --dry-run            # Show what would be committed and pushed

dotfiles self-update               # Download & install the latest CLI binary in place
```

`pull` fetches the repo from origin and brings submodules to the recorded
commits; `update` advances submodules to their own upstream HEAD. `pull`
re-stows currently-installed modules so new files get linked.

### Diagnose & repair

```bash
dotfiles doctor                    # Health check: tools, repo, gh auth, shell, PATH,
                                   # module deps, submodules, dangling links (exit 1 on failure)
dotfiles clean                     # Remove dangling symlinks left by removed dotfiles
dotfiles clean --dry-run           # Preview what would be removed
```

### Author modules

```bash
dotfiles add fish --desc "Fish shell"   # Scaffold a new module directory + module.toml
dotfiles adopt fish ~/.config/fish      # Move existing $HOME config into the module and stow it
dotfiles edit                           # Open the dotfiles repo in $EDITOR
dotfiles edit nvim                      # Open a specific module
```

`adopt` moves each given path (which must live under `$HOME`) into the module at
its `$HOME`-relative location, then stows it so the original path becomes a
symlink — the safe way to bring an existing config under management.

### Shell completion

```bash
dotfiles completion zsh > "${fpath[1]}/_dotfiles"   # zsh (then restart your shell)
dotfiles completion bash | sudo tee /etc/bash_completion.d/dotfiles
```

Module-name arguments (`install`, `uninstall`, `update`, `pull`, `sync`,
`deps`, `adopt`, `edit`) complete dynamically from the modules in the repo.

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
ghostty/   → ~/Library/Application Support/com.mitchellh.ghostty   (macOS only)
i3/        → ~/.i3, ~/.config/dunst   (submodule AugustDG/i3-config — Linux only)
```

## Module manifest (`module.toml`)

Each module directory has a `module.toml`:

```toml
name = "nvim"
description = "Neovim config"
os = ["darwin", "linux"]          # omit to support all

[deps]
brew = ["neovim"]                  # Homebrew formulae (macOS + Linuxbrew)
cask = ["font-hack-nerd-font"]     # Homebrew casks (macOS only)
apt  = ["build-essential"]         # apt packages (Debian/Ubuntu)
dnf  = ["gcc"]                     # dnf/yum packages (Fedora/RHEL)

[hooks]
post_install = "nvim --headless \"+Lazy! sync\" +qa"
```

Dependencies install with the package manager appropriate to the current OS,
skipping anything already present.

## Toolchain manifest (`dotfiles.toml`)

The top-level `dotfiles.toml` declares what the bootstrap phase installs (core
and global brew packages, and which `$HOME` files to back up before stowing).
It's optional — the CLI ships the same values as built-in defaults, so a fresh
bootstrap works before the repo is even cloned. Edit it to change the toolchain.

## Machine-local secrets

`.zprofile` sources `~/.zshrc.local` if it exists. Put machine-specific exports
there (e.g. `CLOUD_PAT`) — the file is never tracked. `dotfiles doctor` warns if
it's missing.

## Development

```bash
go build -o dotfiles ./cmd/dotfiles
go test ./...
./dotfiles status
```

Releases are built by GitHub Actions on push to `master` (a rolling `latest`
prerelease) and on tag push (`v*`). Binaries for darwin/arm64, darwin/amd64,
linux/amd64, and linux/arm64 are attached to each release.
