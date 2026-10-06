# Znap
[[ -r ~/.plugins/znap/znap.zsh ]] ||
    git clone --depth 1 -- \
        https://github.com/marlonrichert/zsh-snap.git ~/.plugins/znap
source ~/.plugins/znap/znap.zsh  # Start Znap

# Custom completion functions (treehouse, …) live here. Znap defers
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

# Aliases
alias cd='z'
alias th='treehouse'
alias gp='git pull'
alias gs='git status'
alias cdr='cd "$(git rev-parse --show-toplevel)"'
alias codex='codex --dangerously-bypass-approvals-and-sandbox'
alias claude='_ZO_DOCTOR=0 claude'
# pi runs on node 22 even in projects where mise pins another version
alias po='mise exec node@22.22.0 -- pi'

gpm() {
  (git checkout master || git checkout main) && git pull
}

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
fi



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

# atuin
eval "$(atuin init zsh)"

# zoxide — smarter cd (provides `z` and `zi`)
if command -v zoxide >/dev/null 2>&1; then
  eval "$(zoxide init zsh)"
fi

# mise: per-directory tool versions, refreshed before each prompt
command -v mise >/dev/null 2>&1 && eval "$(mise activate zsh)"

# Machine-local aliases and functions (never tracked); exports live in
# ~/.zprofile.local
[[ -r "$HOME/.zshrc.local" ]] && source "$HOME/.zshrc.local"

# ws
eval "$(ws shell-init zsh)"
