#!/bin/sh
# Claude Code runs this before and after each tool call that can change a file, and when a session would stop. It
# reports the style rules a session must fix by hand, and changes nothing: a hook that rewrote a file would leave the
# session's copy of it stale.
#
# Before a call, it saves a checksum of each Go file that differs from HEAD, in each kiln checkout the call can reach.
# After the call, it checks each Go file whose checksum is new. So it checks an edit made through the shell too.
# A call reaches the checkout of the file it names, of its working directory, and of each directory its command
# enters with cd or -C. A file the call commits still counts, since the comparison uses HEAD from before the call.
set -u
in=$(cat)
field() { printf '%s' "$in" | jq -r "$1"; }
cd "$CLAUDE_PROJECT_DIR" || exit 0

# A session that edits a checkout no call names is still checked here, across the whole repository.
# A stop already held back once goes ahead, so a rule the session can't meet never keeps it running. The gate's
# TestStyle still fails it.
if [ "$(field .hook_event_name)" = Stop ]; then
  [ "$(field .stop_hook_active)" = true ] && exit 0
  out=$(go tool style -l . 2>&1 | grep -v '^exit status')
  [ -z "$out" ] && exit 0
  printf 'Fix these before you end your turn; gofmt fixes the formatting:\n%s\n' "$out" >&2
  exit 2
fi

# kiln_top prints the root of the kiln checkout that holds directory $1, and nothing for another repository.
kiln_top() {
  top=$(git -C "$1" rev-parse --show-toplevel 2>/dev/null) || return 0
  [ "$(head -1 "$top/go.mod" 2>/dev/null)" = "module github.com/fwilkerson/kiln" ] && echo "$top"
}

# reach prints each directory the call can change, one to a line.
reach() {
  cwd=$(field '.cwd // empty')
  dir=$(field '.tool_input.file_path // empty')
  if [ -n "$dir" ]; then
    # A Write call may name a file in a directory that does not exist yet.
    dir=$(dirname "$dir")
    while [ ! -d "$dir" ]; do dir=$(dirname "$dir"); done
    echo "$dir"
  fi
  [ -n "$cwd" ] && echo "$cwd"
  field '.tool_input.command // .tool_input.code // empty' |
    grep -oE '(^|[^[:alnum:]_-])(cd|-C)[[:space:]]+[^[:space:];&|)]+' |
    sed -E 's/^.*(cd|-C)[[:space:]]+//' | tr -d "'\"" |
    while IFS= read -r d; do
      case "$d" in
      "~"*) d="$HOME${d#"~"}" ;;
      /*) ;;
      *) d="$cwd/$d" ;;
      esac
      [ -d "$d" ] && echo "$d"
    done
}

# sums prints a line for each Go file in checkout $1 that differs from commit $2, or is untracked: its checksum,
# size and absolute path. cksum fails on a deleted file, so sums prints no line for it.
sums() {
  {
    git -C "$1" diff -z --name-only "$2" -- '*.go' 2>/dev/null
    git -C "$1" ls-files -z -o --exclude-standard -- '*.go' 2>/dev/null
  } | (cd "$1" && xargs -0 cksum 2>/dev/null) |
    awk -v top="$1" 'NF >= 3 { c = $1; s = $2; sub(/^[^ ]+ [^ ]+ /, ""); print c, s, top "/" $0 }'
}

state="${TMPDIR:-/tmp}/kiln-style.$(field '.tool_use_id // .session_id // "none"')"
if [ "$(field .hook_event_name)" = PreToolUse ]; then
  rm -rf "$state" && mkdir -p "$state" || exit 0
  reach | while IFS= read -r d; do kiln_top "$d"; done | sort -u |
    while IFS= read -r top; do
      head=$(git -C "$top" rev-parse HEAD 2>/dev/null) || continue
      echo "$head $top" >>"$state/heads"
      sums "$top" "$head" >>"$state/sums"
    done
  exit 0
fi

[ -f "$state/heads" ] || { rm -rf "$state"; exit 0; }
touch "$state/sums"
changed=$(while read -r head top; do sums "$top" "$head"; done <"$state/heads" |
  grep -vxFf "$state/sums" | cut -d' ' -f3- | sort -u)
rm -rf "$state"
[ -z "$changed" ] && exit 0

# One gofmt at the end fixes the formatting, so the check after a call leaves it to the stop.
out=$(printf '%s\n' "$changed" | while IFS= read -r f; do
  (cd "$(dirname "$f")" && go tool style -l "$f" 2>&1)
done | grep -v -e 'not formatted as gofmt' -e '^exit status')
[ -z "$out" ] && exit 0
echo "$out" >&2
exit 2
