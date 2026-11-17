# An interrupted kiln accept leaves a spec no command can clear

Found 2026-11-17, by an audit of building and landing.

## What happened

`kiln accept x`, for a spec waiting for UAT, moves main to `docs(x): retire the accepted spec`. Then it removes the
worktree, and only then retires the record. Ctrl-C during the removal ends accept early. main has deleted the spec
and gained the line in `PRODUCT.md`, and the record still waits for UAT. From then on:

```text
kiln accept x   open .../x/.kiln/specs/x.md: no such file or directory
kiln reject x   refused: x: it is waiting for UAT, and its work is on main: change the spec, commit it, and queue it
kiln queue x    refused: main holds no .kiln/specs/x.md: commit it there first
```

## What should happen

`kiln accept x` run again finishes the job: when main already holds the retiring commit, it retires the record and
removes the checkouts, and writes nothing more to main.

## How to see it

A scenario with a `git` wrapper first on the PATH. After a successful `merge --ff-only`, the wrapper's next
`worktree remove` sends SIGINT to its parent and sleeps 3 seconds before it runs the real git. The first
`kiln accept x` fails. The second must exit 0, and `kiln status` must not list x.

## Where to look

`acceptLanded` in `internal/kiln/kiln.go`, and `retire` and `title` in `internal/kiln/land.go`.
