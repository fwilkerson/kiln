# kiln init says to commit, and its own hook refuses that commit

Found 2026-11-17, by an audit of `kiln init`.

## What happened

In a repository that is not yet a Go module, `kiln init` writes Go's gate, prints "commit .kiln/ on main: kiln
reads it there", and installs the pre-commit hook. The commit it asked for then fails:

```text
pattern ./...: directory prefix . does not contain main module or its selected dependencies
kiln: the commit to main is refused, since the gate fails.
```

`kiln init --preset python` does the same in a uv project without ruff, ty and pytest. Nothing init printed says
that a hook now runs the gate on each commit to main, or what the gate is.

## What should happen

After its files, `kiln init` prints the gate's commands, and says that the hook it installed runs them on each
commit to main, so they must pass before `.kiln/` is committed. The hook's refusal names the same fix: make the
gate's commands pass, or change `gate` in `.kiln/kiln.json`.

## How to see it

A scenario in a repository that holds only a README: `kiln init`, then `git add .kiln .claude` and `git commit`.

## Where to look

`Init` in `internal/kiln/init.go`, and `Precommit` in `internal/kiln/gate.go`. `uat/testdata/setup/init.txtar` and
`precommit.txtar` hold today's output.
