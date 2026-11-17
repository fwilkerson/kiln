# A session runs unsandboxed when Claude Code's sandbox cannot start

Found 2026-11-17, by an audit of the sandbox.

## What happened

The settings kiln writes for a session turn the sandbox on, and do not set `sandbox.failIfUnavailable`. Claude
Code's documentation says that, without that key, it runs commands unsandboxed when the sandbox cannot start.
`claude --help` says that with `-p`, a settings file that fails validation is silently ignored. kiln's file holds
what `hosts` and the `writable` commands gave, unchecked: `"https://proxy.golang.org/"` and a relative path pass
through as written.

## What should happen

The package doc of `internal/claude` says each session's commands run in Claude Code's sandbox. The settings set
`sandbox.failIfUnavailable` to true. kiln refuses, before any session starts, a host in `kiln.json` that is not a
bare host name, and a writable path that is not absolute, each with a message that names the value.

## How to see it

A test of `settings` in `internal/claude` that reads the key. A scenario whose `kiln.json` names the host
`https://proxy.golang.org/`, and whose `kiln queue x` must refuse.

## Where to look

`settings` in `internal/claude/settings.go`, and `sandboxed` in `internal/kiln/sandbox.go`, which runs the
`writable` commands.
