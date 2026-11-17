# A landing ignores an identity set in the environment, and guesses one

Found 2026-11-17, by an audit of `internal/git`.

## What happened

A developer sets their git identity only through `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and
`GIT_COMMITTER_EMAIL`, as direnv or a CI job does. `git commit` in their shell is by `Dev <dev@example.com>`. kiln
strips every `GIT_*` variable, so git guesses, and kiln's commit is by `<account name> <user@host.local>`, with no
warning. A developer with no identity at all gets the same guess on main.

## What should happen

kiln's commits carry the identity `git commit` would use in the developer's shell. kiln's git keeps those four
variables, and no other `GIT_*` variable. With no identity anywhere, git refuses the commit rather than guess: pin
`user.useConfigOnly=true`, so the error names `git config user.name`.

## How to see it

A test in `internal/git`: a repository whose config holds no `user.name` or `user.email`, under a scratch `HOME`,
with the four variables set. `Checkout`, write a file, `Commit`, and read the commit's author and committer.

## Where to look

`Env` in `internal/proc/env.go`, which filters the environment for git, and the `pinned` list in
`internal/git/git.go`.
