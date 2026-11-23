# A kiln killed after a proof that did not stand stops the build, and the verify session is never told

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

`disproved` in `internal/kiln/work.go` records a proof that did not stand in two writes. The first sets the
record's `Unproved` commit. The second, in `goBack`, owes the verify session its round. A kiln killed between them
leaves `Unproved` set and nothing owed.

The next `kiln fire` commits nothing new in the clone. So `changedNothing` finds the tree that `Unproved` held,
and stops the build: "the verify session changed no test after a proof that did not stand". The verify session was
never told that its proof did not stand. `TestKilledAtEachStep` misses it, since no flow in `internal/crash` has a
proof that does not stand.

A second place: `tended` in `internal/kiln/build.go` merges main, which may owe a conflict's round, and clears
the record's `MergeMain` in a later write. A kill between the two merges again, and may owe that round twice. No
test has shown this one.

## What should happen

A kill at any step of a proof that did not stand loses no message. Either the record keeps the round owed with the
unproved commit, in one write, or it keeps neither, and the next kiln proves again. The verify session is told
before kiln reads what it changed.

## How to see it

Drive a `"workflow": "verify-first"` build whose first test passes on main's code through the crash harness of
`internal/crash`. Kill kiln at the write of `state.json` that holds `unproved` while the verify seat's `owed` is
empty. The second `kiln fire` prints "stopped: the verify session changed no test after a proof that did not
stand", with 0 of 3 rounds used. Only the verify session's first run ever started.
