# kiln tui slows down as a gauge's readings pile up

Found 2026-11-17, by an audit of the terminal UI.

## What happened

For a gauge's row, `newestFitting` joins the readings from each start in turn, oldest first, until the rest fit
the width. Its cost grows with the square of the count, and it runs on every frame. One row of 1,000 readings
takes 32 ms, and 2,000 take 110 ms. A monitor that reads a gauge each hour writes 8,760 a year.

## What should happen

A row's cost does not grow with the readings that cannot fit: build the row from the newest reading backward,
and stop at the width.

## How to see it

A test in `internal/tui` that draws `gaugeRows` at 150 columns for 500 readings and for 20,000. The second must
take under 5 times the first, and show the same newest readings.

## Where to look

`newestFitting` and `gaugeRows` in `internal/tui/gauges.go`.
