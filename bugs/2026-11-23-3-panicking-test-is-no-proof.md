# A test that panics on main's code is no proof, and the report says no test was named

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

An acceptance test may panic on main's code, such as one that indexes into the output of a command main does not
have yet. `go test` prints `--- FAIL: TestAT1` and the package's `FAIL` line, but the test binary ends before it
reports. `failures.read` in `internal/kiln/gate.go` marks such a package cut short, and the check counts as broken.
`failsOnTests` is then false, so the proof does not stand.

`disproved` in `internal/kiln/work.go` sends the verify session `testBroke`: "a command failed with no test
named, or a path is refused". The test was named, and no path was refused. A session that rightly changes nothing
then stops the build: "the verify session changed no test after a proof that did not stand". That stop is for a spec
main already meets.

## What should happen

In a proof, a package cut short counts each test its output names with `--- FAIL` as failed. The proof stands when
each cut-short package names such a test and nothing else failed: no command without a named test, and no refused
path. A package cut short that names no failed test is still no proof, as a test that does not compile is. A gate of
the build's work reads a cut-short package as it does today.

## How to see it

Build a spec under `"workflow": "verify-first"`. Its verify session writes a test that runs `cat x.txt`, splits the
output with `strings.Fields`, and reads element 0, which panics while main has no `x.txt`. `kiln fire` prints "no
test fails on main's code alone; it goes back to the verify session, round 1 of 3". The verify session's second run
changes nothing, and the build stops as unproved.
