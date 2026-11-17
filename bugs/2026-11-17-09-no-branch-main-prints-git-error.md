# A repository with no branch main gets a raw git error

Found 2026-11-17, by audits of `kiln init` and of `internal/git`.

## What happened

In a repository whose branch is `master`, or one with no commit, `kiln init` succeeds and says nothing of main.
Then `kiln queue x` prints:

```text
kiln queue: refused: git --literal-pathspecs ls-tree -z --full-tree ^{commit} -- .kiln/kiln.json: exit 128: fatal:
Not a valid object name ^{commit}
```

`kiln status` prints the same error after "kiln could not read its gauges", and exits 0.

## What should happen

Each command that reads main refuses with one message that names the rule and the fix: kiln builds on the branch
named main, and this repository has none. `kiln init` says so too, after it writes its files, and still exits 0.

## How to see it

A scenario that renames its branch with `git branch -m master`, then runs `kiln queue x` and `kiln status`.

## Where to look

`Tip` in `internal/git/land.go` answers "" for a branch that is missing, and callers pass it on as a revision:
`Queue` in `internal/kiln/kiln.go`, and the gauges' read in `internal/kiln/gauge.go`. `tools/install/main.go`
already words the refusal: "the repository has no branch main".
