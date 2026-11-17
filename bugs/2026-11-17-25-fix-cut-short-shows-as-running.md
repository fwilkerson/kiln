# A monitor whose fix was cut short shows as running

Found 2026-11-17, by audits of the terminal UI and of the monitors.

## What happened

A `kiln monitor run` dies mid-fix. The book still holds the fix as building, under a process ID that is gone.
`kiln tui` shows the monitor as `▶ running`, in yellow, while its "now" line says nothing is running. `kiln
status` says "fix building", and "next: wait for it to end". Both last until the next `kiln monitor run` drops
the fix.

## What should happen

A fix whose kiln is gone shows as halted, as a spec's build does in `kiln tui`. `kiln status` says the same, and
its next step names `kiln monitor run`, which drops the fix and runs the monitor again.

## How to see it

A test of the view in `internal/kiln`: a book that holds a monitor's record, building, under the process ID of a
process that has exited, and a ledger with its monitor, built and start lines. `view.Monitors` must answer the
state `tui.Halted`.

## Where to look

`fixStates` and `standing.state` in `internal/kiln/view.go`, `stateOf` and `alive` there, and `fixStep` in
`internal/kiln/stats.go`.
