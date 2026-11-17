# kiln tui shows a span just under a minute as 60s

Found 2026-11-17, by an audit of the terminal UI.

## What happened

`span` rounds a duration before it picks the unit. So 59.6 seconds shows as `60s`, and 59 minutes 45 seconds as
`60m`.

## What should happen

They show as `1m` and `1h00m`: `span` picks the unit from the rounded duration.

## How to see it

Two rows in a test of `span` in `internal/tui`.

## Where to look

`span` in `internal/tui/numbers.go`.
