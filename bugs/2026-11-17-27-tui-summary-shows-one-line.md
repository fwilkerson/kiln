# kiln tui shows only the first line of a monitor's summary

Found 2026-11-17, by an audit of the terminal UI.

## What happened

A monitor's summary holds two lines, as a failed session's does: "the session failed:" then "API Error: 529
overloaded". The builds panel places the summary as one row, so the column names and the builds are drawn over
its second line.

## What should happen

The package doc of `internal/tui` says the summary heads the panel. Each of its lines has a row, up to three,
and the builds follow.

## How to see it

A test in `internal/tui`: a monitor whose `Summary` is those two lines. The screen must hold "API Error: 529
overloaded".

## Where to look

`recentRows` in `internal/tui/draw.go`.
