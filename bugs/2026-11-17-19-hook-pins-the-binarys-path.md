# The pre-commit hook fails every commit once the kiln binary moves

Found 2026-11-17, by an audit of `kiln init`.

## What happened

The hook `kiln init` writes runs kiln by the absolute path of the binary that ran init:
`exec /path/to/kiln precommit`. A developer who ran init from a downloaded binary and later moved it, or through
`go run`, then fails every commit to main:

```text
.git/hooks/pre-commit: line 3: exec: /path/to/kiln: cannot execute: No such file or directory
```

## What should happen

The hook says what is wrong and how to fix it. When the path is no longer executable, it runs the `kiln` on the
PATH if there is one. Otherwise it prints that kiln is gone from that path, and that `kiln init` writes the hook
again, and exits 1.

## How to see it

A scenario that copies kiln to a directory of its own, runs that copy's `init`, deletes the copy, and commits.

## Where to look

`installHook` in `internal/kiln/kiln.go`, which writes the hook from `r.exe`.
