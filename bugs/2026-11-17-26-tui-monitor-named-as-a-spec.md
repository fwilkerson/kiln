# kiln tui opens a spec's builds from a monitor of the same name

Found 2026-11-17, by an audit of the terminal UI.

## What happened

A spec `deps` was built and accepted, and `kiln.json` later names a monitor `deps`. In `kiln tui`, the developer
selects the monitor's row. The next refresh, a second later, moves the selection to the spec's row. Enter on the
monitor's row opens a blank screen, or one of the spec's builds.

## What should happen

The row selected stays selected across a refresh, and Enter on a monitor opens its newest fix. The model tells a
monitor from a spec of the same name wherever it looks one up.

## How to see it

A test in `internal/tui`, as `tui_test.go` drives the model: a source with a spec and a monitor both named
`deps`, the keys j, j, then a tick. The builds panel's title must still start "fixes of deps".

## Where to look

`spec`, `build`, `outcome`, `keep` and `pick` in `internal/tui/model.go`, which each find a row by its name.
