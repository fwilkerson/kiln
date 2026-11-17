# kiln tui's key list runs off a short terminal

Found 2026-11-17, by an audit of the terminal UI.

## What happened

On a run's screen in a terminal of 100 columns and 7 rows, `?` lists the keys in columns that fit the height, 115
columns wide. The last column, with `? keys` and `q quit`, is cut off.

## What should happen

The package doc of `internal/tui` says `?` lists every key a screen takes. A list too wide for the terminal
scrolls: j and k move it by a row, and its last row says that more follow.

## How to see it

A test in `internal/tui`: open a run at 100 by 7, press `?`, and cut each row at the terminal's width. Between
that screen and the ones j reaches, "quit" must show.

## Where to look

Where `internal/tui/keys.go` lays the list out in columns.
