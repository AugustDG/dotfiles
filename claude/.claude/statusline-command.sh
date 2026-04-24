#!/usr/bin/env bash
input=$(cat)

cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd // ""')
model=$(echo "$input" | jq -r '.model.display_name // ""')
remaining=$(echo "$input" | jq -r '.context_window.remaining_percentage // empty')
cost=$(echo "$input" | jq -r '.session.cost // empty')

# Shorten home directory to ~
home="$HOME"
short_cwd="${cwd/#$home/\~}"

# Nerd Font icons via UTF-8 hex (works with bash 3.2)
icon_folder=$(printf '\xEF\x81\xBB')     # U+F07B
icon_branch=$(printf '\xEE\x82\xA0')     # U+E0A0
icon_model=$(printf '\xEF\x8B\x9B')      # U+F2DB  (microchip)
icon_dirty=$(printf '\xEF\x81\xAA')      # U+F06A
icon_staged=$(printf '\xEF\x81\xA7')     # U+F067
icon_clean=$(printf '\xEF\x80\x8C')      # U+F00C
icon_cost=$(printf '\xEF\x85\x95')       # U+F155

# Colors (orange theme via truecolor)
c_reset=$'\033[0m'
c_orange=$'\033[38;2;215;119;87m'        # warm orange
c_orange_light=$'\033[38;2;240;160;120m'  # lighter orange
c_orange_dim=$'\033[38;2;180;100;60m'     # muted orange
c_green=$'\033[38;2;120;180;100m'
c_red=$'\033[38;2;220;80;70m'
c_amber=$'\033[38;2;255;193;7m'
c_dim=$'\033[2m'
c_bold=$'\033[1m'

# Separator
sep="${c_orange_dim} │ ${c_reset}"

# --- Directory ---
dir_part="${c_orange}${icon_folder}  ${short_cwd}${c_reset}"

# --- Git ---
git_part=""
if [ -n "$cwd" ] && git -C "$cwd" rev-parse --git-dir > /dev/null 2>&1; then
  branch=$(git -C "$cwd" -c core.hooksPath=/dev/null symbolic-ref --short HEAD 2>/dev/null || git -C "$cwd" rev-parse --short HEAD 2>/dev/null)

  modified=$(git -C "$cwd" diff --name-only 2>/dev/null | wc -l | tr -d ' ')
  staged=$(git -C "$cwd" diff --cached --name-only 2>/dev/null | wc -l | tr -d ' ')

  status_icons=""
  if [ "$modified" -gt 0 ] 2>/dev/null; then
    status_icons="${c_red}${icon_dirty} ${modified}${c_reset}"
  fi
  if [ "$staged" -gt 0 ] 2>/dev/null; then
    [ -n "$status_icons" ] && status_icons="${status_icons} "
    status_icons="${status_icons}${c_green}${icon_staged} ${staged}${c_reset}"
  fi
  if [ "$modified" -eq 0 ] 2>/dev/null && [ "$staged" -eq 0 ] 2>/dev/null; then
    status_icons="${c_green}${icon_clean}${c_reset}"
  fi

  git_part="${sep}${c_orange_light}${icon_branch}  ${branch}${c_reset} ${status_icons}"
fi

# --- Model ---
model_part=""
if [ -n "$model" ]; then
  model_part="${sep}${c_orange_dim}${icon_model}  ${model}${c_reset}"
fi

# --- Context gauge ---
ctx_part=""
if [ -n "$remaining" ]; then
  remaining_int=$(printf "%.0f" "$remaining")

  if [ "$remaining_int" -gt 50 ] 2>/dev/null; then
    ctx_color="$c_orange_light"
    bar="████░"
  elif [ "$remaining_int" -gt 25 ] 2>/dev/null; then
    ctx_color="$c_amber"
    bar="███░░"
  elif [ "$remaining_int" -gt 10 ] 2>/dev/null; then
    ctx_color="$c_red"
    bar="██░░░"
  else
    ctx_color="${c_red}${c_bold}"
    bar="█░░░░"
  fi

  ctx_part="${sep}${ctx_color}${bar} ${remaining_int}%${c_reset}"
fi

# --- Cost ---
cost_part=""
if [ -n "$cost" ] && [ "$cost" != "null" ]; then
  cost_fmt=$(printf "%.2f" "$cost" 2>/dev/null)
  if [ -n "$cost_fmt" ]; then
    cost_part="${sep}${c_orange_dim}${icon_cost} \$${cost_fmt}${c_reset}"
  fi
fi

printf "%s%s%s%s%s\n" "$dir_part" "$git_part" "$model_part" "$ctx_part" "$cost_part"
