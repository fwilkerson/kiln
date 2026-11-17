# A gate command that leaves a child behind holds the gate until the child ends

Found 2026-11-17, by an audit of the sandbox.

## What happened

The gate command `sleep 4 & echo started` holds the gate for 4 seconds, though its shell exits at once: kiln
waits for the output pipe, which the child still holds. A test that leaves a server running holds the gate
forever. Under launchd the run lock is then never released, and every later `kiln monitor run` refuses. A
monitor's check, a gauge, a monitor's command and the `writable` commands run the same way.

## What should happen

Once a command's shell exits, kiln gives what it left behind 10 seconds to close its output, as `Run` in
`internal/claude` gives a session's. Then it kills the command's process group, and reads the output it has.

## How to see it

A test in `internal/kiln` that times `runCheck` on `sleep 30 & echo started`. It must return within 15 seconds,
with the output "started", and leave no `sleep` running.

## Where to look

`runCheck` in `internal/kiln/gate.go`, `runShell` in `internal/kiln/monitor.go`, the gauge's read in
`internal/kiln/gauge.go`, `commandGate` in `internal/kiln/fix.go`, and the `writable` commands in
`internal/kiln/sandbox.go`. `exec.Cmd.WaitDelay` is the mechanism.
