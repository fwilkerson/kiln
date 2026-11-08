#!/bin/zsh
# Spec 1's build: one unattended claude -p session. Bypass mode is the developer's choice: the session reads Eidos and
# odh outside this checkout and writes .claude/, which no allow rule or hook unlocks for a headless session.
set -eu
cd "${0:A:h}/.."
mkdir -p .hand-run
claude -p "$(<scripts/hand-run-prompt.md)" \
	--permission-mode bypassPermissions \
	--model opus --effort high \
	--output-format stream-json --verbose \
	>.hand-run/stream.jsonl 2>.hand-run/stderr.log
