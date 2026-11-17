# An interrupt during the move of main leaves the checkout half-updated and locked

Found 2026-11-17, by an audit of `internal/git`.

## What happened

`Move` runs `merge --ff-only` in the developer's checkout of main. Ctrl-C cancels kiln's context, and
`exec.CommandContext` then sends SIGKILL to git's process group mid-merge. git has written some of the landed
files into the checkout, has not moved main, and leaves `.git/index.lock`. The developer's next git command
fails. kiln's next landing reads the lock as busy, and never lands until someone deletes the file.

## What should happen

The move finishes or never starts, as `land`'s doc says of an interrupt. Run the fast-forward so that kiln's
cancellation cannot kill it: it takes well under a minute, so kiln waits for it to end.

## How to see it

A test in `internal/git`: a branch that adds 20,000 small files, and `Move` under a context that ends after 20 ms.
Today main is not moved and `.git/index.lock` is left. Afterward, either main is at the landed commit, or the
checkout is as it was, and no `index.lock` exists.

## Where to look

`Move` in `internal/git/land.go`, `OwnGroup` in `internal/proc/proc.go`, and the comment on `run` in
`internal/git/git.go`, which says a process group of its own keeps an interrupt from ending git.
