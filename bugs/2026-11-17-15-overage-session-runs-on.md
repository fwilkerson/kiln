# A session that enters billing overage runs on, and kiln then resumes it after it finished

Found 2026-11-17, by an audit of the sandbox.

## What happened

A session's stream holds a usage-limit event whose status is allowed, with overage in use. The session goes on
working, at overage rates, and ends with a result. kiln then prints "paused for a usage limit", waits for the
reset, and resumes the finished session with "The usage limit has reset. Go on where you stopped."

## What should happen

The comment beside the event's handling says overage is billing the developer never chose for an unattended
build. So kiln ends the session when that event arrives, and pauses the build until the limit resets, as it does
for a limit that refuses. A session that ended with a result is never resumed for a limit.

## How to see it

A scenario whose scripted session emits `{"limit": {"status": "allowed", "using_overage": true, "resets_in":
"1s"}}`, then a Bash call that echoes a marker, then a result. The marker must not be in the stream.

## Where to look

Where `internal/claude/claude.go` reads the limit event, and `run` there, which keeps reading. The pause is in
`internal/kiln/build.go`. `uat/testdata/build/usage-limit.txtar` holds today's rule.
