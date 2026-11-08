# kiln

The developer approves a spec and walks away. kiln builds it with headless Claude Code sessions, gates it, and lands
it on main. The developer comes back to the spec met, asked nothing but about the spec.

The first spec is `specs/kiln.md`. What it leaves for later is in `docs/later.md`.

## Use

```text
kiln init            write a starting kiln.json, and install the pre-commit hook
kiln approve <name>  queue a build of specs/<name>.md, as main holds it
kiln run             build each queued spec, one at a time, until none is queued
kiln status          show where each spec stands
kiln accept <name>   end UAT: land the spec if it waits on its branch, then retire it
```

1. Write `specs/<name>.md` with the `/slice` skill, in an interactive Claude Code session, and commit it on main.
2. Run `kiln approve <name>`, then `kiln run`.
3. Read `kiln status`. A spec waiting for UAT is on main. A spec waiting on its branch with amendments shows them.
4. Run `kiln accept <name>`. The spec is deleted, and `PRODUCT.md` names the commit that last held it.

`kiln.json` holds the gate's commands and the rounds a failing gate goes back to the session. kiln reads it from
main's tip, so a build cannot weaken its own gate. The pre-commit hook runs the same commands on a commit to main.

## Where things are

- `cmd/kiln`: the command line.
- `internal/kiln`: approving, building, gating, landing and accepting a spec. `go doc ./internal/kiln` shows how.
- `internal/claude`, `internal/git`, `internal/proc`: Claude Code sessions, git, and child processes, from Eidos.
- `internal/scenariotest`: the scripted claude and the harness the acceptance tests run on.
- `acceptance/<spec>/`: the code of each acceptance test, `TestAT<N>` for test N.
- `guidance/`: the `/slice` skill and the `zoom-out`, `comprehension`, `critic` and `audit` agents, for `.claude/`.
  Copy `guidance/agents/` to `.claude/agents/` and `guidance/skills/` to `.claude/skills/`. A landing does it too.
- `tools/style`: the layout rules gofmt leaves out. `style_test.go` and `arch_test.go` hold them in `go test ./...`.
