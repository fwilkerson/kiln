# kiln reject can remove a landing that kiln accept has just begun

Found 2026-11-22, by the agent that made one function end every record.

## What happened

`Repo.Reject` in `internal/kiln/kiln.go` reads the book, checks that the spec or fix may be rejected, then calls
`end`, which removes its checkouts, writes its outcome, and retires its record. The check and the removal are two
steps, with no lock held across them. A `kiln accept` of the same name that starts between them takes the record,
marks it building and starts its landing, while the reject removes the worktree and the branch under it.

## What should happen

`kiln reject` and `kiln accept` of one name never both act on its record. Whichever starts second refuses, and
says the other is under way, or waits for it, as kiln's other commands do on a lock.

## How to see it

Give a fix that waits on its branch a slow `git worktree remove`, for example with a fake `git` on the `PATH`
that sleeps for that command. Run `kiln reject` on it, and during the sleep run `kiln accept` on the same name.
The accept starts a landing whose checkout the reject then removes.
