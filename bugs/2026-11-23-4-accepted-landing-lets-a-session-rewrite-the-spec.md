# During an accepted landing, a session can rewrite the spec, and the change lands as accepted

Found 2026-11-23, by the audit of the workflows change, commit 839db82. The cause is older than that change.

## What happened

`build.allowed` in `internal/kiln/build.go` names the spec's file and its amendment file for each record the
developer accepted. kiln's own commit needs them: it folds the amendments into the spec, and retires it. But the
same paths go to every gate of the session's work, to the landing, and to `verifyGuard`, and `guard.guarded` lets
a build change an allowed path.

So a session that runs during the accepted landing may change the spec too, by a shell command. It runs there after
a conflict with main, or a failed landing gate. The gate passes, and the changed text lands. The `git show` command
that `PRODUCT.md` records for the spec prints words the developer never accepted.

## What should happen

During an accepted landing, only kiln's commit that folds and retires the spec changes the spec's file and its
amendment file. A session's change to either fails the gate, as it does before the developer accepts.

## How to see it

Build a spec `x` under `"workflow": "verify-first"` whose verify session amends test 1 to "it holds 42, or more",
so the build waits on its branch. Commit a conflicting `x.txt` on main, and run `kiln accept x`. In its next run,
the build session merges main, and runs `sed` on `.kiln/specs/x.md` to make the amendment's words "it holds 41, or
more". The accept lands. The command `PRODUCT.md` records for `x` prints "it holds 41, or more".
