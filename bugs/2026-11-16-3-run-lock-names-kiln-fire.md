# The run lock names kiln fire when kiln accept holds it

Found 2026-11-16 in odh.

## What happened

While a `kiln accept` lands a build, it holds the run lock. A second `kiln accept` of a spec, made in that window,
is told that "the kiln fire that is building" lands it. No `kiln fire` runs.

A fix's accept is unaffected, since it always names the next `kiln monitor run`.

## What should happen

The run lock names the command that holds it. A second accept's reply names a `kiln fire` only while one runs, and
otherwise says what lands the build, a command that will in fact land it.

## Where to look

`landAccepted` in `internal/kiln/kiln.go` takes the lock through `r.run`, which writes `runHolder`, "kiln fire".
`landsAccepted` reads that holder for its reply.
