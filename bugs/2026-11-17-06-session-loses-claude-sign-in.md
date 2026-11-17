# A session loses the sign-in and the provider set in the environment

Found 2026-11-17, by an audit of the sandbox.

## What happened

kiln withholds every `CLAUDE*` variable from a session. So a session never sees `CLAUDE_CODE_OAUTH_TOKEN`, which
is how `claude setup-token` signs an unattended session in: Claude Code saves that token nowhere. It never sees
`CLAUDE_CONFIG_DIR`, so a developer with two accounts builds under the default one. It never sees
`CLAUDE_CODE_USE_BEDROCK` or `CLAUDE_CODE_USE_VERTEX`, so a developer on either is switched off it, silently.

## What should happen

A session keeps those four variables, and still loses every other `CLAUDE*` variable, which describe the session
kiln itself may run inside.

## How to see it

A row for each in the test of `Session.Env` in `internal/proc`.

## Where to look

`Env` in `internal/proc/env.go`.
