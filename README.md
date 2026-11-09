# kiln

The developer approves a spec and walks away. kiln builds it with headless Claude Code sessions, gates it, and lands
it on main. The developer comes back to the spec met, asked nothing but about the spec.

`PRODUCT.md` holds, for each accepted spec, the `git show` command that prints it from the commit that last held it.

What spec 1 left for later is in `docs/later.md`.

## Use

```text
kiln init                      write a starting kiln.json, and install the pre-commit hook
kiln approve [--again] <name>  queue a build of specs/<name>.md, as main holds it
kiln run                       build each queued spec, one at a time, until none is queued
kiln status                    show where each spec stands
kiln stats [<name>]            show what each build cost, one line each, or each build of one spec in full
kiln tui [<name>]              show every build, live and past, in a full-screen view; or open it on one spec
kiln accept <name>             end UAT: land the spec if it waits on its branch, then retire it
```

1. Write `specs/<name>.md` with the `/slice` skill, in an interactive Claude Code session, and commit it on main.
2. Run `kiln approve <name>`, then `kiln run`.
3. Read `kiln status`. A spec waiting for UAT is on main. A spec waiting on its branch with amendments shows them.
4. Run `kiln accept <name>`. The spec is deleted, and `PRODUCT.md` gains the command that shows it.
   To revise a spec that waits for UAT instead, commit the edit and run `kiln approve --again <name>`.
5. Read `kiln stats` for the trend across builds, and `kiln stats <name>` for one spec's builds by role and session.
   A landed build's commit holds its time, cost, turns and failed calls too. The ledger is
   `.git/kiln/ledger.jsonl`, and each session's stream, for why a number is what it is, is
   `.git/kiln/sessions/<id>/stream.jsonl`.
6. Run `kiln tui` in a terminal of its own, beside `kiln run`, to watch the running session and look back over each
   build run by run. It only reads.

`kiln.json` holds the gate's commands and the rounds a failing gate goes back to the session. kiln reads it from
main's tip, so a build cannot weaken its own gate. The pre-commit hook runs the same commands on a commit to main.

## Where things are

- `cmd/kiln`: the command line.
- `internal/kiln`: approving, building, gating, landing and accepting a spec. `go doc ./internal/kiln` shows how.
- `internal/tui`: the full-screen view of `kiln tui`, on Bubble Tea. `internal/kiln` feeds it what kiln wrote.
- `internal/claude`, `internal/git`, `internal/proc`: Claude Code sessions, git, and child processes, from Eidos.
- `internal/scenariotest`: the scripted claude and the harness the acceptance tests run on.
- `internal/termtest`: a pseudo-terminal that runs a program and reads its screen, for the acceptance tests of
  `kiln tui`. No sandboxed session can open one, so those tests run only in kiln's gate.
- `acceptance/<spec>/`: the code of each acceptance test, `TestAT<N>` for test N.
- `guidance/`: the `/slice` skill and the `zoom-out`, `comprehension`, `critic` and `audit` agents, for `.claude/`.
  `.claude/agents` and `.claude/skills` link to `guidance/agents/` and `guidance/skills/`, and a landing makes the
  links where `.claude/` has neither. kiln's worktrees hold the links as plain files, since Claude Code's sandbox
  refuses a shell's write through a link under `.claude/`.
- `tools/style`: the layout rules gofmt leaves out. `style_test.go` and `arch_test.go` hold them in `go test ./...`.
