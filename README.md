# kiln

The developer approves a spec and walks away. kiln builds it with headless Claude Code sessions, gates it, and lands
it on main. The developer comes back to the spec met, asked nothing but about the spec.

`PRODUCT.md` holds, for each accepted spec, the `git show` command that prints it from the commit that last held it.

What spec 1 left for later is in `docs/later.md`. A plan the developer means to build waits in `specs/` as a starter
spec, with Status Proposed, until `/slice` takes it up.

## Use

```text
kiln init                write a starting kiln.json, and install the pre-commit hook
kiln queue <name>        queue a build of specs/<name>.md, as main holds it
kiln run                 build each queued spec, one at a time, until none is queued
kiln status              show where each spec stands
kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
kiln accept <name>       take the work: retire the spec, or land its amendments first
kiln reject <name>       turn the amendments away: drop the build, and its branch
```

1. Write `specs/<name>.md` with the `/slice` skill, in an interactive Claude Code session, and commit it on main.
2. Run `kiln queue <name>`, then `kiln run`. kiln queues only a spec whose Status is Approved.
   Queue only once the spec reads as you want it, diagrams included. `kiln tui` shows each build's spec as the
   commit it was queued at held it, so a later fix on main shows only in a build queued after the fix.
3. Read `kiln status`. A spec waiting for UAT is on main. A spec waiting on its branch with amendments shows them.
4. Run `kiln accept <name>`. A spec waiting on its branch lands with its amendments folded in. The spec is deleted,
   and `PRODUCT.md` gains the command that shows it. If that landing stops, `kiln accept <name>` lands it again.
   - When UAT finds a problem, change the spec with `/slice`, commit it, and run `kiln queue <name>`. The new build
     starts from main, which holds the landed work. kiln refuses to queue a landed spec whose spec is unchanged.
   - To turn amendments away, run `kiln reject <name>`. To change an amendment's wording, commit your own wording to
     the spec, then run `kiln reject <name>` and `kiln queue <name>`.
5. Read a landed build's commit for its time, cost, turns and failed calls. The ledger is `.git/kiln/ledger.jsonl`,
   and each session's stream, for why a number is what it is, is `.git/kiln/sessions/<id>/stream.jsonl`.
6. Run `kiln tui` in a terminal of its own, beside `kiln run`, to watch the running session and look back over each
   build run by run. It only reads.

To reinstall kiln from main's tip, with its zsh completion, run `go run ./tools/install` in this repository. It
builds main, never the working tree, and refuses while any kiln drives a build session. `go install` puts kiln in
`GOBIN`, and `_kiln` goes to `$XDG_DATA_HOME/zsh/completions`, which `fpath` must hold. The completion asks
`kiln complete` for kiln's commands, and the spec names each would not refuse.

`kiln.json` holds the gate's commands and the rounds a failing gate goes back to the session. kiln reads it from
main's tip, so a build cannot weaken its own gate. The pre-commit hook runs the same commands on a commit to main.

## Where things are

- `cmd/kiln`: the command line.
- `internal/kiln`: queuing, building, gating, landing, accepting and rejecting a spec. `go doc ./internal/kiln` shows how.
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
- `cmd/kiln/_kiln`: the zsh completion, which `tools/install` copies.
- `tools/install`: reinstalls kiln and its completion from main's tip.
- `tools/style`: the layout rules gofmt leaves out. `style_test.go` and `arch_test.go` hold them in `go test ./...`.
