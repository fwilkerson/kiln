# The ledger shows fewer rounds than kiln fire prints, when one run is told two messages

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

A run that kiln tells several messages at once gets one start line in the ledger. `turn` in
`internal/kiln/work.go` gives that line the reason of the oldest message alone: `why: owed[0].reason`. A build's
rounds are the highest round among its start lines, as `sums` in `internal/kiln/stats.go` reads them.

So a round that a later message holds is lost. `kiln fire` prints "round 1 of 3" as it sends the work back, while
`kiln ledger --kind build` prints `"rounds":0`, and `kiln tui` shows the same. A run line of
`kiln ledger --kind run` shows the oldest message's `why` and `round` too.

The audit saw it at commit 839db82, where an accepted fix whose landing conflicted with main printed "round 2 of 3",
and the ledger held `"rounds":1`. Commit 44b2c7e made that case one round. Two messages are still owed at once
where the audit's findings wait for the build session, and a join then conflicts with its work. No test has shown
that case on today's code.

## What should happen

`kiln ledger` and `kiln tui` show the rounds `kiln fire` printed. A run told several messages records the highest
round among them, or each message's reason.

## How to see it

In a test of `internal/kiln`, owe the build session two messages: the audit's findings, which hold no round, then
a conflict of round 1. The reason of the run `turn` answers holds round 0, and its why is `findings`.
