# An accepted fix that conflicts with main is told to accept again, forever

Found 2026-11-17, by an audit of the monitors.

## What happened

A fix waits for `kiln accept`, and main gains a commit that changes the same lines. `kiln accept docs` rebases,
meets the conflict and stops. It prints "next: fix main, and run kiln accept docs to land it again". No session
is there to resolve a conflict, so each further accept stops the same way. `kiln status` shows, as its report, a
message written for a session: "Merge main into your branch with git merge main ...". The monitor is held for
good.

## What should happen

The next step works. For an accepted fix whose landing stopped on a conflict, the summary and `kiln status` say
that main changed what the fix changed, and name `kiln reject <name>`: the monitor's next run then fixes anew,
from main. A landing that stopped on the gate keeps today's next step.

## How to see it

A scenario with a monitor whose `command` writes `docs.txt` and whose entry says `"accept": true`. After
`kiln monitor run`, commit another text to `docs.txt` on main, and run `kiln accept docs`. Its output and
`kiln status` must name `kiln reject docs`, and not "kiln accept docs to land it again".

## Where to look

`landFix` in `internal/kiln/fix.go`, `fixStep` in `internal/kiln/stats.go`, and the conflict's report in
`internal/kiln/land.go`.
