# A verify session's run that failed stops the build as if it had changed no test

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

A run of the verify session that fails, such as on an API error with no result, counts as a run that answered.
`runOnce` in `internal/kiln/build.go` applies `turn.ends` to each run that ended with no error of kiln's own, so
the run's message is paid. `specJob.heard` reads nothing from a verify session's reply, a failed one included.

After a proof that did not stand, the next `prove` finds the clone as that proof left it. `changedNothing` in
`internal/kiln/work.go` then stops the build: "the verify session changed no test after a proof that did not
stand". `internal/kiln/state.go` says that stop means main's code already does what is asked. The session never
said so. Its run failed before it could answer the proof's report.

## What should happen

kiln reads that the verify session changed nothing only from a run that ended with a result. A run that failed is
told the proof's report again, within the session's rounds. The build never stops as unproved for a run that failed.

## How to see it

Build a spec under `"workflow": "verify-first"` with `"rounds": 3`. The verify session's first run writes a test
that passes on main's code. Its second run ends as an error with no work:
`{"result": {"is_error": true, "result": "API Error: 500 Internal server error"}}`. `kiln fire` prints "no test
fails on main's code alone; it goes back to the verify session, round 1 of 3", then "stopped: the verify session
changed no test after a proof that did not stand". The session's third run, which would write a failing test, never
starts.
