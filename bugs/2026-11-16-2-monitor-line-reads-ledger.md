# A monitor's line in kiln status shows the ledger, not the book

Found 2026-11-16 in odh.

## What happened

A `kiln accept health` held the run lock for about 40 seconds. A `kiln accept deps` made in that window queued the
deps fix's landing, accepted, for the next `kiln monitor run`. `kiln status` then showed the monitor as waiting:

```text
deps: waiting on the developer, last ran 2026-11-16 21:27, runs next after 2026-11-23 20:57
```

The developer had already accepted the fix, and nothing waited on them.

## What should happen

While the book holds a monitor's fix as accepted with its landing queued, the monitor's line says so. It does not
say "waiting on the developer".

## Where to look

`fixResult` in `internal/kiln/monitor_show.go` takes the label from the fix's last outcome in the ledger, `waits on
its branch`. The book holds what the fix is now.
