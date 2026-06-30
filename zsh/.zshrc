# Znap
[[ -r ~/.plugins/znap/znap.zsh ]] ||
    git clone --depth 1 -- \
        https://github.com/marlonrichert/zsh-snap.git ~/.plugins/znap
source ~/.plugins/znap/znap.zsh  # Start Znap

# Custom completion functions (treehouse, no-mistakes, …) live here. Znap defers
# compinit to the first prompt, so this dir just needs to be on fpath before then.
fpath=(~/.zsh/completions(N) $fpath)

# macOS defaults to 256 open files which is too low for tmux + plugins
ulimit -n 10240 2>/dev/null


# Oh-my-posh
eval "$(oh-my-posh init zsh --config "https://raw.githubusercontent.com/JanDeDobbeleer/oh-my-posh/main/themes/negligible.omp.json")"

# Machine-local secrets — see ~/.zshrc.local (created during bootstrap)


# History — shared across all tmux panes/windows
HISTFILE="$HOME/.zsh_history"
HISTSIZE=100000
SAVEHIST=100000
setopt SHARE_HISTORY          # read/write history in real time across all shells
setopt HIST_IGNORE_ALL_DUPS   # remove older duplicate when a new one is added
setopt HIST_REDUCE_BLANKS     # trim whitespace
setopt HIST_IGNORE_SPACE      # prefix with space to keep a command out of history

# Editor
export EDITOR=nvim

# Aliases
gcp() {
  if [[ -z "$1" ]]; then
    echo "usage: gcp <branch>"
    return 1
  fi
  git checkout "$1" && git pull
}

alias cd='z'
alias th='treehouse'
alias no='no-mistakes'
alias gp='git pull'
alias gs='git status'
alias cdr='cd "$(git rev-parse --show-toplevel)"'
alias codex='command codex --dangerously-bypass-approvals-and-sandbox'
alias claude='claude --model "claude-opus-4-6[1m]" --effort xhigh'

cdw() {
  if [[ -z "$1" ]]; then
    git worktree list
    return
  fi
  local dir
  dir="$(git worktree list --porcelain 2>/dev/null \
    | awk -v name="$1" '/^worktree / { path=$2 } /^branch / { sub(/.*\//, "", $2); if ($2 == name) { print path; exit } }')"
  if [[ -z "$dir" ]]; then
    echo "cdw: worktree '$1' not found"
    return 1
  fi
  cd "$dir"
}

_cdw() {
  local -a wts
  wts=( $(git worktree list --porcelain 2>/dev/null \
    | awk '/^branch / { sub(/.*\//, ""); print }') )
  _describe 'worktree' wts
}
compdef _cdw cdw

gb() {
  if [[ -z "$1" ]]; then
    echo "usage: gb <name>"
    return 1
  fi
  git checkout -b "$1"
}

gc() {
  if [[ -z "$1" ]]; then
    echo "usage: gc <name>"
    return 1
  fi
  git checkout "$1"
}

gagc() {
  if [[ $# -eq 0 ]]; then
    echo "usage: gagc <paths...> [-m <msg>]"
    return 1
  fi
  local -a paths
  local msg=""
  while (( $# )); do
    case "$1" in
      -m)
        msg="$2"
        shift 2
        ;;
      *)
        paths+=("$1")
        shift
        ;;
    esac
  done
  if (( ${#paths[@]} == 0 )); then
    echo "usage: gagc <paths...> [-m <msg>]"
    return 1
  fi
  git add "${paths[@]}" || return
  if [[ -n "$msg" ]]; then
    git commit -m "$msg"
  else
    gho commit
  fi
}

# Git completion for the wrappers above (reuses zsh's built-in _git).
# compinit is loaded by znap; if you move this earlier, ensure compinit ran first.
if (( $+functions[compdef] )); then
  compdef _git gcp=git-checkout
  compdef _git gc=git-checkout
  compdef _git gb=git-checkout
  compdef _git gp=git-pull
  compdef _git gs=git-status
  compdef _git gagc=git-add
  compdef _treehouse th
  compdef _no-mistakes no
fi


# nvm
export NVM_DIR="$HOME/.nvm"
[[ -n "${HOMEBREW_PREFIX:-}" && -s "$HOMEBREW_PREFIX/opt/nvm/nvm.sh" ]] && source "$HOMEBREW_PREFIX/opt/nvm/nvm.sh"
# nvm end

# bun completions
[ -s "$HOME/.bun/_bun" ] && source "$HOME/.bun/_bun"

# Yazi
function y() { # press y to open yazi
	local tmp="$(mktemp -t "yazi-cwd.XXXXXX")" cwd
	command yazi "$@" --cwd-file="$tmp"
	IFS= read -r -d '' cwd < "$tmp"
	[ "$cwd" != "$PWD" ] && [ -d "$cwd" ] && builtin cd -- "$cwd"
	rm -f -- "$tmp"
}
# yazi end

# commands
use() {
  case "$1" in
    aws)
      aws sso login --profile shared
      aws sso login --profile personal
      export AWS_PROFILE=shared
      echo "AWS ready (shared + personal). Active: shared"
      ;;
    *)
      echo "use: unknown target '$1'"
      ;;
  esac
}


. "$HOME/.atuin/bin/env"

eval "$(atuin init zsh)"

# zoxide — smarter cd (provides `z` and `zi`)
if command -v zoxide >/dev/null 2>&1; then
  eval "$(zoxide init zsh)"
fi

[[ -r "$HOME/.local/bin/env" ]] && . "$HOME/.local/bin/env"

# >>> hopper >>>
# hopper zsh integration
_h_cd_pick() {
  local target
  if (( $# > 0 )); then
    target="$(command hopper query "$*" </dev/tty 2>/dev/tty)" || return
  else
    target="$(command hopper pick </dev/tty 2>/dev/tty)" || return
  fi
  target="${target//$'\r'/}"
  target="${target##*$'\n'}"
  [[ "$target" == /* ]] || return
  [[ -n "$target" ]] && cd "$target"
}

h() {
  case "$1" in
    add|remove|list|query|recent|index|init|pick|help|-h|--help)
      command hopper "$@"
      ;;
    "")
      _h_cd_pick
      ;;
    *)
      # If args do not match subcommands, treat them as pick filters.
      _h_cd_pick "$@"
      ;;
  esac
}

ha() {
  command hopper add "$@"
}

hr() {
  command hopper remove "$@"
}

h_widget() {
  local target
  target="$(command hopper pick </dev/tty 2>/dev/tty)" || return
  target="${target//$'\r'/}"
  target="${target##*$'\n'}"
  [[ "$target" == /* ]] || return
  zle reset-prompt
  [[ -n "$target" ]] || return
  BUFFER="cd ${(q)target}"
  zle accept-line
}
zle -N h_widget
bindkey '^G' h_widget
# <<< hopper <<<
