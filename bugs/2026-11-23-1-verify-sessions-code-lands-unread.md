# After a proof stood, the verify session's change to code joins the work and lands unread

Found 2026-11-23, by the audit of the workflows change, commit 839db82.

## What happened

Once a proof stands, `proof` in `internal/kiln/work.go` asks `testsKept` whether a later commit of the verify
session leaves the proved tests as they were. `testsKept` reads `verifyChanged`, which drops each path the verify
session may not write. So a commit that changes only code counts as kept. `proof` moves the record's `Tested` to it
with no gate, and `join` merges the whole branch into `kiln/<name>`.

A first proof refuses such a path, since its gate reads the guard's refusals. No later commit meets that check. The
build's gates then judge the change as the build session's work, which may write code. The audit has read the work
by then, and reads it again only for a path that kiln.json's `reaudit` names. So the change lands on main, and no
audit read it.

## What should happen

A commit of the verify session that changes a path that session may not write never joins the work. It goes back to
the verify session as a round, with the refusal, as a first proof's does. That holds whether or not the commit
changes a test, for a spec's verify session and for a fix's.

## How to see it

Build a spec under `"workflow": "verify-first"` whose audit sends one finding. In its second run, the verify session
answers with a shell command that appends a line to `doc.go`, and changes no test. `kiln fire` lands the build.
`git show main:doc.go` holds the line, and `kiln ledger --kind gate` shows one proof gate.
