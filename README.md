# dotfiles

Personal dotfiles for macOS (Apple Silicon / Intel) and Linux. Configs are laid
out as [GNU stow](https://www.gnu.org/software/stow/) packages managed by a
single Go CLI — `dotfiles` is the one entrypoint for installing, updating,
diagnosing, and authoring your dotfiles.

## Quick install

```bash
curl -sL https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash
```

This downloads the pre-built `dotfiles` CLI, then runs `dotfiles init`, which
bootstraps Homebrew, installs the toolchain, clones this repo, and presents an
interactive module picker.

## Commands

Run `dotfiles <command> --help` for full flags. `-v/--verbose` shows the
underlying command output for any command. `dotfiles --help` lists commands in
the same groups as below.

### Set up this machine

```bash
dotfiles init                      # Bootstrap brew/gh/repo/toolchain, then pick modules to link
dotfiles init --adopt              # …absorbing conflicting files into the repo instead of failing
dotfiles doctor                    # Health check: tools, repo, gh auth, shell, PATH,
                                   # module deps, submodules, dangling links (exit 1 on failure)
dotfiles self-update               # Download & install the latest CLI binary in place
```

`init` is safe to rerun. Without a terminal it links every OS-compatible module.

### Link modules into $HOME

```bash
dotfiles link                      # Interactive picker
dotfiles link nvim tmux            # Stow modules, install their deps, run their hooks
dotfiles link --all                # Every OS-compatible module
dotfiles unlink nvim tmux          # Remove their symlinks; the modules stay in the repo
dotfiles unlink --all              # Unlink everything

dotfiles status                    # Repo state + per-module stow/submodule/deps table
dotfiles status --check            # Exit non-zero if dirty/unpushed or links broken (for prompts/CI)
dotfiles deps                      # Install missing deps for all modules
dotfiles deps nvim                 # …for specific modules
dotfiles clean                     # Remove dangling symlinks left by removed dotfiles
dotfiles clean --dry-run           # Preview what would be removed
```

### Sync the repo

```bash
dotfiles pull                      # git pull + sync submodules + re-stow linked modules
dotfiles pull nvim tmux            # …limited to specific modules
dotfiles update                    # Bump submodules to their upstream latest, re-stow
dotfiles update tmux               # …for specific modules
dotfiles sync                      # Commit & push local changes, submodules first
dotfiles sync tmux -m "msg"        # Sync specific modules with a commit message
dotfiles sync --dry-run            # Show what would be committed and pushed
```

`pull` fetches the repo from origin and brings submodules to the recorded
commits; `update` advances submodules to their own upstream HEAD. Both re-stow
linked modules so new files get linked.

### Author modules

```bash
dotfiles add fish --desc "Fish shell"   # Scaffold a new module directory + module.toml
dotfiles adopt fish ~/.config/fish      # Move existing $HOME config into the module and stow it
dotfiles eject atuin                    # Stop managing; keep its files in $HOME (reverse of adopt)
dotfiles eject --delete-files atuin     # Also drop its files from $HOME
dotfiles eject --with-submodules nvim   # Also unregister nested Git submodules
dotfiles edit                           # Open the dotfiles repo in $EDITOR
dotfiles edit nvim                      # Open a specific module
```

`adopt` moves each given path (which must live under `$HOME`) into the module at
its `$HOME`-relative location, then stows it so the original path becomes a
symlink. It's the safe way to bring an existing config under management, and
`eject` undoes it.

### Shell completion

```bash
dotfiles completion zsh > "${fpath[1]}/_dotfiles"   # zsh (then restart your shell)
dotfiles completion bash | sudo tee /etc/bash_completion.d/dotfiles
```

Module-name arguments (`link`, `unlink`, `eject`, `update`, `pull`,
`sync`, `deps`, `adopt`, `edit`) complete dynamically from the modules in the
repo. znap caches zsh's completion list in `~/.cache/zsh/compdump`, so after
adding a new completion file run `rm ~/.cache/zsh/compdump*` before restarting
the shell.

## Layout

```
zsh/       → ~/.zshenv, ~/.zprofile, ~/.zshrc
git/       → ~/.gitconfig
pi/        → ~/.pi/agent/{settings, keybindings, themes, mcp config, extensions}
nvim/      → ~/.config/nvim      (submodule AugustDG/nvim-config)
tmux/      → ~/.config/tmux      (submodule AugustDG/tmux-config)
yazi/      → ~/.config/yazi      (submodule AugustDG/yazi-config)
zed/       → ~/.config/zed       (submodule AugustDG/zed-config)
ws/        → ~/.config/ws        (installs the ws binary from AugustDG/ws; projects/ is private submodule AugustDG/ws-projects)
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

Two untracked files hold what differs per machine:

- `~/.zprofile.local`, sourced from `.zprofile`: exports and secrets (e.g.
  `CLOUD_PAT`). `dotfiles doctor` warns if it's missing.
- `~/.zshrc.local`, sourced from `.zshrc`: aliases and functions.

`dotfiles init` creates both if they don't exist.

The `pi` module tracks portable settings and extension sources, but intentionally
excludes `auth.json`, sessions, session-message runtime data, model caches, and
extension `node_modules`.

## Development

```bash
go build -o dotfiles ./cmd/dotfiles
go test ./...
./dotfiles status
```

Releases are built by GitHub Actions on push to `master` (a rolling `latest`
prerelease) and on tag push (`v*`). Binaries for darwin/arm64, darwin/amd64,
linux/amd64, and linux/arm64 are attached to each release.
