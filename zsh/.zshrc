# Interactive-shell config. Env/PATH setup lives in .zprofile.

# --- Znap (plugin manager) ---
[[ -r ~/.plugins/znap/znap.zsh ]] ||
    git clone --depth 1 -- \
        https://github.com/marlonrichert/zsh-snap.git ~/.plugins/znap
source ~/.plugins/znap/znap.zsh

# macOS defaults to 256 open files; tmux + plugins want more
ulimit -n 10240 2>/dev/null

# --- Prompt ---
eval "$(oh-my-posh init zsh --config "https://raw.githubusercontent.com/JanDeDobbeleer/oh-my-posh/main/themes/negligible.omp.json")"

# --- History (shared across tmux panes/windows) ---
HISTFILE="$HOME/.zsh_history"
HISTSIZE=100000
SAVEHIST=100000
setopt SHARE_HISTORY          # read/write history in real time across all shells
setopt HIST_IGNORE_ALL_DUPS   # remove older duplicate when a new one is added
setopt HIST_REDUCE_BLANKS     # trim whitespace
setopt HIST_IGNORE_SPACE      # prefix with space to keep a command out of history

# --- Git aliases / wrappers ---
alias gp='git pull'
alias gs='git status'
alias cdr='cd "$(git rev-parse --show-toplevel)"'

gcp() {
  if [[ -z "$1" ]]; then
    echo "usage: gcp <branch>"
    return 1
  fi
  git checkout "$1" && git pull
}

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
fi

# --- Claude CLI default flags ---
alias claude='claude --model "claude-opus-4-6[1m]" --effort high'

# --- bun completions ---
[ -s "$HOME/.bun/_bun" ] && source "$HOME/.bun/_bun"

# --- yazi (press y to open, cd's into final dir) ---
y() {
  local tmp="$(mktemp -t "yazi-cwd.XXXXXX")" cwd
  command yazi "$@" --cwd-file="$tmp"
  IFS= read -r -d '' cwd < "$tmp"
  [ "$cwd" != "$PWD" ] && [ -d "$cwd" ] && builtin cd -- "$cwd"
  rm -f -- "$tmp"
}

# --- `use <target>` helper ---
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

# --- atuin (shell-history + ZLE bindings) ---
command -v atuin >/dev/null 2>&1 && eval "$(atuin init zsh)"

# --- hopper (project jumper) ---
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
