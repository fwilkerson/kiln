# kiln tui leaves a failed fix's outcome plain

Found 2026-11-17, by an audit of the terminal UI.

## What happened

In the builds panel, a fix whose outcome is "session failed" shows plain, with no glyph. The specs panel marks
the same monitor `✗ failed`, in red. "couldn't run" and "usage limit" show plain too.

## What should happen

The package doc of `internal/tui` says red marks what failed, with ✗ beside it. A fix that ended "session
failed" or "couldn't run" shows red, with ✗. One that ended "usage limit" shows yellow, with ▶, as a paused build
does.

## How to see it

A test in `internal/tui`: a monitor with one fix whose `Outcome` is "session failed". The screen must hold
"✗ session failed".

## Where to look

`outcome` in `internal/tui/draw.go`.
