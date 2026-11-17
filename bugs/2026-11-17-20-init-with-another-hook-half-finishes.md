# kiln init half-finishes beside another pre-commit hook, or writes into a shared one

Found 2026-11-17, by audits of `kiln init` and of `internal/git`.

## What happened

With a pre-commit hook of the developer's own in place, as husky or the pre-commit framework writes, `kiln init`
writes every file, prints "commit .kiln/ on main: kiln reads it there", and then fails:

```text
kiln init: refused: .git/hooks/pre-commit is another pre-commit hook; call /path/to/kiln precommit from it
```

With a global `core.hooksPath`, and no pre-commit hook there, init writes kiln's hook into that directory, which
every repository on the machine shares. Each commit anywhere then runs `kiln precommit`.

## What should happen

`kiln init` installs its hook only in this repository's own hooks directory. Where `core.hooksPath` is set, or
another pre-commit hook is in place, it writes no hook. It still writes its files and exits 0, and its last line
says that it installed no hook, why, and the line to add to the developer's own hook: `kiln precommit`.

## How to see it

Two scenarios: one with a script at `.git/hooks/pre-commit` before `kiln init`, and one with `core.hooksPath` set
to a directory outside the repository.

## Where to look

`installHook` in `internal/kiln/kiln.go`, which asks `rev-parse --git-path hooks`, and `Init` in
`internal/kiln/init.go`.
