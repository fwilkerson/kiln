# A build whose kiln died shows as building, and kiln queue says to wait for it

Found 2026-11-17, by an audit of building and landing.

## What happened

After `kiln fire` is killed, the record stays building, under a process ID that is gone. `kiln status` prints
`x: building`, though no kiln runs. `kiln queue x` refuses with "it is building: queue it again once its build
ends", and that build never ends by itself. `kiln tui` already shows such a build as halted.

## What should happen

`kiln status` shows a build whose kiln is gone as halted, with a next line that names `kiln fire`, which takes it
up again. `kiln queue`'s refusal says the same.

## How to see it

A scenario whose scripted session kills its `kiln fire`, then runs `kiln status` and `kiln queue x`.

## Where to look

`record.status` in `internal/kiln/kiln.go`, `queueRefusal` in `internal/kiln/state.go`, and `stateOf` and `alive`,
which `kiln tui` reads, in `internal/kiln/view.go`.
