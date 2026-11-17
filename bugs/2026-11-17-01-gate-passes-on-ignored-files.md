# The gate passes on ignored files that the landing leaves out

Found 2026-11-17, by an audit of `internal/git`.

## What happened

A build's session writes a file that `.gitignore` ignores, such as a fixture `acceptance/x/want.golden` under
`*.golden`, and its test reads it. The file stays in the build's worktree, so the gate passes there, at the first
gate and at the landing. The commit lacks the file. kiln prints "x: landed on main", and the test fails on main.

## What should happen

The gate judges what lands. A build whose tests need a file the commit does not hold fails its gate, and the
failure goes back to the session, which can then name the file in a way git keeps.

## How to see it

A scenario with `.gitignore` holding `*.golden`, a build that copies in `acceptance/x/want.golden` and a test that
reads it. `kiln fire` lands, and `go test ./acceptance/x/` on main then fails: "open want.golden: no such file".

## Where to look

`Commit` in `internal/git/git.go` runs `add --all`, which skips ignored files. `Reset` there runs `clean -qfd`,
without `-x`, so the worktree keeps them. The gate runs in the build's worktree, in `internal/kiln/build.go`, and
the landing's reset is in `internal/kiln/land.go`. A `Detached` tree is cleaned with `-x`, and sees only the commit.
