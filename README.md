# dotfiles

Personal dotfiles for macOS (Apple Silicon / Intel) and Linux. Configs are laid
out as [GNU stow](https://www.gnu.org/software/stow/) packages; per-tool configs
(nvim, tmux, yazi, opencode, zed, i3) are git submodules so they can be
maintained independently.

## Quick install

```bash
curl -s https://raw.githubusercontent.com/AugustDG/dotfiles/master/install.sh | bash
```

This bootstraps Homebrew, installs zsh + git + gh + stow + go, runs
`gh auth login` (SSH) so private repos and submodules resolve, clones this
repo, then hands off to `install.zsh` which installs the rest of the toolchain
and stows configs into `$HOME`.

## Manual install

```bash
git clone --recurse-submodules git@github.com:AugustDG/dotfiles.git ~/projects/dotfiles
cd ~/projects/dotfiles
./install.sh
```

## Layout

```
zsh/       → ~/.zshenv, ~/.zprofile, ~/.zshrc
git/       → ~/.gitconfig
claude/    → ~/.claude/{CLAUDE.md, settings.json, statusline-command.sh, skills/}
             (greptileai/skills vendored as a submodule under skills/greptile)
nvim/      → ~/.config/nvim      (submodule AugustDG/nvim-config)
tmux/      → ~/.config/tmux      (submodule AugustDG/tmux-config)
yazi/      → ~/.config/yazi      (submodule AugustDG/yazi-config)
opencode/  → ~/.config/opencode  (submodule AugustDG/opencode-config)
zed/       → ~/.config/zed       (submodule AugustDG/zed-config)
i3/        → ~/.i3, ~/.config/dunst   (submodule AugustDG/i3-config — Linux only)
```

To lay everything into `$HOME`:

```bash
cd ~/projects/dotfiles
stow -t "$HOME" zsh git claude nvim tmux yazi opencode zed
# Linux with i3:
stow -t "$HOME" i3
```

## Machine-local secrets

`.zprofile` sources `~/.zshrc.local` if it exists. Put machine-specific
exports there (e.g. `CLOUD_PAT`) — the file is never tracked.

```bash
# ~/.zshrc.local
export CLOUD_PAT=bp_pat_xxxxxxxxxxxx
```
