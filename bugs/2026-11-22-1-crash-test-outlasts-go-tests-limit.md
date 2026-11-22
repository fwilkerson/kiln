# go test ./... fails on a busy machine, as the crash test outlasts its limit

Found 2026-11-22, while merging the audit review's fixes with three agents at work.

## What happened

`TestKilledAtEachStep` in `internal/crash` took 83 seconds on a quiet machine and 518 seconds at a load of 12. A
plain `go test ./...` gives each package 10 minutes, so on one run the package failed at 611 seconds with a panic
from the timeout, and no test of its own failed. kiln's gate passes `-timeout 30m`, but a session runs a plain
`go test ./...`, as `.kiln/agents/build.md` and each monitor's agent tell it to. A session that sees the failure
may chase a defect that isn't there, or stop.

## What should happen

`go test ./...` passes on a busy machine whenever it passes on a quiet one. The crash test's run time stays well
inside `go test`'s default limit, whatever the load, or the test fails with a message that names its own budget
rather than with `go test`'s timeout.

## How to see it

Run `go test -count=1 ./internal/crash` while two other `go test -count=1 ./...` runs work in other checkouts.
Its time grows several times over, and with enough load it passes 10 minutes.
