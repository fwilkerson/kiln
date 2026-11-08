#!/bin/zsh
# Spec 1's build: one unattended claude -p session under hand-run-settings.json. The sandbox keeps its writes in this
# checkout and Go's caches. Claude Code refuses a headless write under .claude/ whatever the rules say, so the session
# stages those files under guidance/ for the developer to copy in.
set -eu
cd "${0:A:h}/.."
mkdir -p .hand-run
claude -p "$(<scripts/hand-run-prompt.md)" \
	--permission-mode acceptEdits \
	--settings scripts/hand-run-settings.json --setting-sources project \
	--model opus --effort high \
	--output-format stream-json --verbose \
	</dev/null >.hand-run/stream.jsonl 2>.hand-run/stderr.log
