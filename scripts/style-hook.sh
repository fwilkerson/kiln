#!/bin/sh
# Claude Code runs this after each Edit or Write. It reports the style rules a session must fix by hand, and changes
# nothing: a hook that rewrote the file would leave the session's copy of it stale. One gofmt fixes the formatting.
f=$(jq -r '.tool_input.file_path // empty')
# A Go file in another repository answers to that repository's rules, not kiln's.
case "$f" in
"$CLAUDE_PROJECT_DIR"/*.go) ;;
*) exit 0 ;;
esac

cd "$CLAUDE_PROJECT_DIR" || exit 0
out=$(go run ./tools/style -l "$f" 2>&1 | grep -v -e 'not formatted as gofmt' -e '^exit status')
[ -z "$out" ] && exit 0
echo "$out" >&2
exit 2
