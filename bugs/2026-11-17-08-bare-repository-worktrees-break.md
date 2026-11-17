# kiln's first checkout breaks every worktree of a bare repository

Found 2026-11-17, by an audit of `internal/git`.

## What happened

A developer keeps a bare repository with worktrees: `git clone --bare ... repo.git`, then
`git -C repo.git worktree add ../work main`. kiln opens from `work`, and `kiln queue` works. The first `Checkout`
or `Detached` sets `extensions.worktreeConfig` in the shared config, which holds `core.bare = true`. git then
reads every linked worktree as bare. kiln's own `reset --hard` fails with "this operation must be run in a work
tree". So does the developer's `git status` in `work`, and it keeps failing after kiln exits.

## What should happen

kiln leaves the developer's repository usable. Before it sets `extensions.worktreeConfig`, it moves `core.bare`,
and `core.worktree` if set, from the shared config to the main worktree's `config.worktree`, as git's documentation
of that extension says to.

## How to see it

A test in `internal/git`: make a repository with one commit, clone it bare, add a worktree of main, open kiln's
`Git` from that worktree's top, and call `Checkout`. Today `Checkout` fails, and `git status` in the worktree fails.

## Where to look

`addWorktree` in `internal/git/git.go`, at `config set extensions.worktreeConfig true`.
