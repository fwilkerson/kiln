# A kill just after main moved leaves the landed build stopped, and unacceptable

Found 2026-11-17, by an audit of building and landing.

## What happened

`land` moves main, and only then clears the record's `Before`, drops the checkouts and sets the state to landed.
kiln dies, or is interrupted, in between: a SIGKILL after `merge --ff-only`, or Ctrl-C during `git worktree
remove`. The record stays building, and main holds `feat(x): land the approved spec`. The next `kiln fire` resets
the branch and resumes the session. Its rebase onto main finds no change, so the session is told "Your work changes
nothing on main". Past its rounds the build stops, with "the gate still failed". `kiln accept x` refuses a stopped
build, and `kiln queue x` builds work that main already holds.

## What should happen

A build whose landing moved main ends waiting for UAT, however kiln ended. The record holds the commit being landed
before main moves. When kiln takes a build up again and main already holds that commit, it records the landing,
reads the gauges and drops the checkouts, and starts no session.

## How to see it

A scenario that puts a `git` wrapper first on the PATH. The wrapper runs the real git, and after a successful
`merge --ff-only` it sends SIGKILL to its parent. `kiln fire` dies, a second `kiln fire` follows, and `kiln status`
must say `x: waiting for UAT`.

## A second place to check

`kiln accept` of a spec with amendments, killed at the same point. Today the next `kiln fire` stops on a conflict
in `.kiln/specs/x.md`, and `kiln accept x` repeats it. It should end retired.

## Where to look

`landWork` and `onMain` in `internal/kiln/build.go`, `land` and `unland` in `internal/kiln/land.go`, and `goOn` in
`build.go`.
