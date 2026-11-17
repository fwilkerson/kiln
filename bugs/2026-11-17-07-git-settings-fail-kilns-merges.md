# Common git settings fail kiln's merges and clones, or change a landing's author

Found 2026-11-17, by an audit of `internal/git`.

## What happened

The package doc of `internal/git` says the developer's config cannot change what kiln's calls do. Four settings do:

- `merge.verifySignatures=true`: every move of main fails with "Commit ... does not have a GPG signature", since
  kiln's commits are never signed.
- A global `merge.ff=only`: a split build's `Merge` of main into its clone fails with "Not possible to
  fast-forward".
- A global `clone.defaultRemoteName=upstream`: each `Clone` fails at `git remote remove origin`.
- `log.showSignature=true`, when the build's HEAD is a signed commit: `git show` prints the signature check first,
  and the landing's author name becomes `Good "git" signature for ...`.

## What should happen

Each call does the same under any of the four. Pin each setting, or pass the flag that overrides it.

## How to see it

Tests in `internal/git` that build a temporary repository, as `move_test.go` does. Set the first in the
repository's config. Set the second and third in a scratch `HOME`'s `.gitconfig`, since a clone reads only global
config, and kiln strips `GIT_*` from git's environment. For the fourth, sign a commit with an SSH key the test
makes, then `Rebase` it.

## Where to look

The `pinned` list in `internal/git/git.go`. `Move` in `internal/git/land.go`, `Clone` and `Merge` in
`internal/git/clone.go`, and `commitTree` in `git.go`, which reads the author with `git show --format`.
