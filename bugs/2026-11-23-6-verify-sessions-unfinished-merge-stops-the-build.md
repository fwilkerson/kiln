# The verify session's unfinished merge stops the build as an error of kiln's

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

When main moves under a spec's build beside a verify session, kiln merges main into that session's clone. A
conflict there goes to the verify session as a round, which asks it to merge main and resolve each file. A run that
ends with the merge unfinished leaves the conflict in the clone.

`verifyWork` in `internal/kiln/work.go` then gets a `*git.Unmerged` from `Commit`, and answers it as an error.
The build stops: "kiln could not go on with the build: a merge is unfinished, with conflicts in ...". The build
session's same case is a round: `sessionWork` in `internal/kiln/build.go` answers the unfinished merge as a
setback, with the files that still conflict.

## What should happen

The verify session's unfinished merge goes back to it as a round, with the files that still conflict, as the build
session's does. Past the verify session's last round, the build stops for the conflict, and not as an error of
kiln's.

## How to see it

Build a spec `x` under `"workflow": "verify-first"`. While the build session's first run goes on, commit a
different `acceptance/x/x_test.go` on main, so the landing conflicts in a file the verify session alone writes. The
verify session's next run does `git merge main || true`, and ends. `kiln fire` prints "stopped: kiln could not go
on with the build: a merge is unfinished, with conflicts in acceptance/x/x_test.go".
