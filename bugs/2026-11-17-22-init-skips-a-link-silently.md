# kiln init says nothing when it cannot link a reviewer

Found 2026-11-17, by an audit of `kiln init`.

## What happened

A project already holds `.claude/agents/critic.md`, an agent of its own. `kiln init` prints "linked" for
`zoom-out` and `comprehension`, and nothing for `critic`. The developer's file stays, which is right. `/shape`
then runs the developer's unrelated `critic` as a reviewer of each spec.

## What should happen

For each link it skips, init prints a line that names the path, says something else is there, and says which file
under `.kiln/` it would have linked, so the developer can rename one.

## How to see it

A scenario that brings `.claude/agents/critic.md`, then runs `kiln init`.

## Where to look

The links in `internal/kiln/init.go`, near its end.
