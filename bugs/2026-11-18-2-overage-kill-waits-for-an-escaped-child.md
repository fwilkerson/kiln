# A session killed at a usage overage can hold kiln for 10 seconds

Found 2026-11-18, while writing a test that a session is killed at its first overage event.

## What happened

When a session's stream reports overage, `claude.Run` stops and kills the session's process tree. A child the
session forks at that moment can escape the kill, as `tree.end` in `internal/claude/tree.go` stops and kills only
the processes it has seen. The escaped child keeps the session's output open, so `Run` waits the full
`outputGrace`, 10 seconds, before it returns. A test whose fake claude ran `sleep` after the overage event took the
full 10 seconds on some runs, and under a second on others.

## What should happen

`Run` returns promptly once it has killed a session at an overage, whatever the session forked as it died. No
process the session started outlives the kill.

## How to see it

A fake claude that prints an overage event, then forks short-lived children in a loop, such as
`while :; do sleep 1; done`. Run it through `claude.Run` several times: some runs take about 10 seconds, and a
`sleep` may outlive the run.
