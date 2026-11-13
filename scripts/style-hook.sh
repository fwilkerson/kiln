#!/bin/sh
# Claude Code runs this after each Edit or Write, and when a session would stop. It reports the style rules a session
# must fix by hand, and changes nothing: a hook that rewrote the file would leave the session's copy of it stale.
in=$(cat)
cd "$CLAUDE_PROJECT_DIR" || exit 0

# A session that edits through the shell never calls Edit or Write, so the stop checks the whole repository.
# A stop already held back once goes ahead, so a rule the session can't meet never keeps it running. The gate's
# TestStyle still fails it.
if [ "$(printf '%s' "$in" | jq -r '.hook_event_name')" = Stop ]; then
  [ "$(printf '%s' "$in" | jq -r '.stop_hook_active')" = true ] && exit 0
  out=$(go run ./tools/style -l . 2>&1 | grep -v '^exit status')
  [ -z "$out" ] && exit 0
  printf 'Fix these before you end your turn; gofmt fixes the formatting:\n%s\n' "$out" >&2
  exit 2
fi

f=$(printf '%s' "$in" | jq -r '.tool_input.file_path // empty')
# A Go file in another repository answers to that repository's rules, not kiln's.
case "$f" in
"$CLAUDE_PROJECT_DIR"/*.go) ;;
*) exit 0 ;;
esac

# One gofmt at the end fixes the formatting.
out=$(go run ./tools/style -l "$f" 2>&1 | grep -v -e 'not formatted as gofmt' -e '^exit status')
[ -z "$out" ] && exit 0
echo "$out" >&2
exit 2
