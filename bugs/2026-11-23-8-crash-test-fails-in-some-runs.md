# The crash test fails in some runs, on code that did not change

Found 2026-11-23, in the session that went over the audit's last findings, at commit 3116a6c.

## What happened

`TestKilledAtEachStep` in `internal/crash` passes in some runs and fails in others, on the same code. Run alone,
with `go test ./internal/crash -count=1`, it failed in 5 of 9 runs at commit 3116a6c. Each failure is one of these
two lines, or both:

```text
flows_test.go:77: no kiln got a real SIGINT at session end, {"code":{"from":"set","owed":1,"session":"set"},"state":"building","verify":{"session":"set"}}, ["build"], which a flow left to an earlier one
flows_test.go:77: no kiln got a real SIGINT at session start, {"code":{"owed":1,"session":"set"},"state":"building","verify":{"from":"set","owed":1,"session":"set"}}, ["verify"], which a flow left to an earlier one
```

It passed in 5 of 5 runs at commit 868d22e, the commit before 71489ad. That commit added the flow `disprove`, whose
verify session's first proof does not stand. So the failures came in with that flow.

The test is in the gate. A run that fails sends a build's work back for no fault of the work, and spends a round.

## What should happen

The test gives the same result in each run of the same code. A leftover that a flow leaves to an earlier flow gets
its SIGINT in that flow's second run, each time. Or the test no longer needs the two runs of a flow to show the same
leftovers.

## How to see it

Run `go test ./internal/crash -count=1` ten times at commit 3116a6c. Some runs fail with a line above, and the
others pass. The same command at commit 868d22e passes each time.

A guess, which no test has shown: in a flow beside a verify session, the two sessions take their steps in an order
that differs from run to run. So the second run of a flow, which gets the signals, may never show a leftover that its
first run showed.
