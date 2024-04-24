# Setup fzf
# ---------
if [[ ! "$PATH" == */home/augus/.fzf/bin* ]]; then
  PATH="${PATH:+${PATH}:}/home/augus/.fzf/bin"
fi

eval "$(fzf --bash)"
