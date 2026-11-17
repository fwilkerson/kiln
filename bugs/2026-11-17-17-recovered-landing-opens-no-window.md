# A landing that kiln monitor run takes up opens no window, even when it stops

Found 2026-11-17, by an audit of the monitors.

## What happened

The developer runs `kiln accept docs` while another kiln holds the run lock, and is told "the next kiln monitor
run lands it". That run, under launchd, lands the fix or stops it as it starts. Either way no window opens: the
summary goes only to `.git/kiln/monitor.log`. A stopped landing then holds the monitor until the developer
happens to read `kiln status`.

## What should happen

The package doc of `internal/kiln` says a fix that lands, stops or waits opens a window. A fix's landing that
`kiln monitor run` takes up as it starts opens one too, by the rule `notifies` holds.

## How to see it

A scenario with `KILN_OPEN` set to a command that appends a line to a file. A fix waits. A process takes
`.git/kiln/run.lock` with flock while `kiln accept docs` runs, then lets go. A commit on main breaks the gate.
`kiln monitor run` follows, and the file must hold a second line.

## Where to look

`recover` in `internal/kiln/monitor_run.go`, which lands through `buildEach`, and `fix` there, the only caller
of `open`.
