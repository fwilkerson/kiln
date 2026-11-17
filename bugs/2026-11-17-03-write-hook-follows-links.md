# The write hook follows a link out of the session's worktree

Found 2026-11-17, by an audit of the sandbox.

## What happened

A session runs `ln -s "$MAIN/.kiln" k` and `ln -s "$(git rev-parse --path-format=absolute --git-common-dir)" g` in
its worktree, where `$MAIN` is the main checkout. Then it calls Write on `k/monitors/planted` and on
`g/hooks/post-commit`. kiln's hook judges each path as written, so it lets both through. One file lands in the main
checkout's `.kiln/monitors/`, and the other is a git hook that the developer's next commit runs. The session
removes both links before the gate. The same passes the guard on `.claude/settings*.json`.

## What should happen

`Hook`'s doc says it refuses a write outside the session's worktree. The hook resolves each link in the path's
existing part, then judges the path it reaches, by every rule it holds today.

## How to see it

A scenario whose scripted session makes both links, writes through each, and removes them. `$WORK` is the main
checkout. Afterward `.kiln/monitors/planted` and `.git/hooks/post-commit` must not exist, and the session's stream
must hold a refused Write.

## Where to look

`Hook`, `resolve` and `canonical` in `internal/kiln/guard.go`. None calls `filepath.EvalSymlinks`.
