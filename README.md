# kiln

Approve a spec, walk away, and come back to it built, tested and on main.

kiln is one Go binary, for macOS. It builds each spec you approve with headless
[Claude Code](https://claude.com/claude-code) sessions, in a sandbox. It holds the work to your project's own gate,
has a second session audit it, and lands it on main as one commit. It asks you about nothing but the spec.

![You shape and queue a spec; kiln builds, gates, audits and lands it on main.](docs/flow.svg)

## Install

kiln needs macOS, git 2.55 or newer, which Xcode's git may not be, and the `claude` command of Claude Code, signed
in. It builds on the branch named `main`, and never pushes.

```sh
go install github.com/fwilkerson/kiln/cmd/kiln@latest        # with Go 1.27 or newer
```

Or take a built binary, for Apple silicon (`arm64`) or Intel (`amd64`):

```sh
curl -fsSL https://github.com/fwilkerson/kiln/releases/latest/download/kiln_darwin_arm64.tar.gz | tar -xz
mv kiln_darwin_arm64/kiln ~/.local/bin/        # or any directory on your PATH
```

The archive holds `_kiln`, the zsh completion, too. Put it in a directory that `fpath` names.

## Quick start

**1. Set the project up.** `kiln init` writes kiln's settings, agents and the `/shape` skill into `.kiln/`, and
installs a pre-commit hook that runs your gate. The preset is `go`, or `python` for uv, pytest, ruff and ty.

```console
$ cd my-project                      # a git repository, on the branch main
$ kiln init
wrote .kiln/agents/audit.md
wrote .kiln/agents/build.md
...
wrote .kiln/kiln.json
linked .claude/skills/shape to ../../.kiln/skills/shape
commit .kiln/ on main: kiln reads it there
the pre-commit hook runs the gate on each commit to main, so make its commands pass before you commit .kiln/:
  go vet ./...
  go test ./...
installed /path/to/my-project/.git/hooks/pre-commit
$ git add .kiln .claude && git commit -m "chore: set up kiln"
```

Run `kiln init` again once you upgrade kiln, and whenever `kiln status` says `.kiln/` is behind. It merges kiln's new
agents and `/shape` with your edits, and leaves `kiln.json` as you wrote it:

```console
$ kiln init
updated .kiln/agents/audit.md
merged .kiln/agents/build.md
merged .kiln/agents/verify.md, with conflicts: resolve them before you commit it
wrote .kiln/init.json
...
$ git diff .kiln                     # read what changed, resolve each conflict, then commit
```

A team can keep a preset of its own in a repository, laid out as kiln's are, and start each project from it:
`kiln init --preset ../presets/go`.

Read `.kiln/kiln.json` once. Its `gate` holds the commands that must pass before any work lands:

```json
{
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "acceptance": "acceptance/{spec}/"
}
```

**2. Write a spec.** In an interactive Claude Code session, run `/shape` and say what you want. It asks what it
needs, has the project's reviewers read the draft, and writes `.kiln/specs/<name>.md`. A spec's acceptance tests say
what done means:

```markdown
### 1. The answer is printed

- **Given**: nothing.
- **When**: someone runs `answer`.
- **Then**: it prints 42.
```

When you approve the spec, `/shape` marks it `Status: Approved`. Commit it on main: kiln builds only what main
holds.

**3. Build it.**

```console
$ kiln queue answer
$ kiln fire
answer: landed on main at 3f9c2ab81d0e, and waits for UAT
  4.2 min · $0.42 · 3 turns · 1 failed call
  1 of 3 rounds · the audit sent findings back
  2 files changed, 18 insertions, 0 deletions
  next: read it with git show 3f9c2ab81d0e, then run kiln accept answer, or change the spec and run
        kiln queue answer
```

`kiln fire` builds each queued spec in turn. Leave it running: it sends a terminal notification when a build needs
you. In a second terminal, `kiln tui` shows each session live, and every past build.

**4. Take the work, or send it back.** A build ends in one of three states, and `kiln status` shows each:

| The build                 | Means                                            | You run                               |
|---------------------------|--------------------------------------------------|---------------------------------------|
| landed, and waits for UAT | The work is on main. Try it.                     | `kiln accept <name>`                  |
| waits on its branch       | The build asks to amend the spec, and shows how. | `kiln accept <name>` or `kiln reject` |
| stopped                   | The gate still failed after the last round.      | fix main or the spec, `kiln queue`    |

`kiln accept` retires the spec, and `PRODUCT.md` gains the `git show` command that prints it. When trying the work
turns up a problem, change the spec with `/shape`, commit it, and queue it again: the new build starts from main,
which holds the landed work.

## What kiln holds a build to

- **Your gate.** kiln reads `kiln.json` from main, never from the build's checkout, so no build weakens its own
  gate. A test that fails on main too is not counted against the build.
- **Acceptance tests written apart from the code.** With `"workflow": "split"`, one session writes the code and
  another writes the tests, from the spec alone. Neither can read the other's checkout.
- **An audit.** Once the gate passes, a second session reads the work and sends its findings back.
- **A sandbox, while the session runs.** A session's shell commands write only in its own checkout and the paths
  `kiln.json` lists, and reach only the hosts it lists. kiln's hook keeps its file tools off `.kiln/`. The gate
  then runs the work outside the sandbox, as your own `go test` does.
- **Your spec's wording.** A build that finds the spec wrong does not quietly build something else. It writes an
  amendment and waits on its branch for you.

```text
                    ┌─ build session, in its own clone:   the code  ─┐
  kiln fire ─▶ main ┤                                                ├─▶ stacked ─▶ gate ─▶ audit ─▶ land
                    └─ verify session, in its own clone:  the tests ─┘
```

## Gauges

A gauge is a number you watch across landings, such as test time or a mutation score. kiln reads each after every
landing. `kiln status` shows the latest reading, and `kiln tui` each landing's. A gauge never blocks a landing.

```json
{"gauges": [{"name": "test-seconds", "command": ".kiln/gauges/test-seconds", "max": 60}]}
```

## Monitors

A monitor is a job kiln runs on a schedule, with no terminal open. Its check looks for a problem. When it finds
one, an agent fixes it, and the fix is gated, audited and landed like any build.

```json
{
  "monitors": [{
    "name": "bugs",
    "every": "1h",
    "check": ".kiln/monitors/bugs",
    "agent": "bugfix.md",
    "prompt": "Fix the one bug the check printed, as your instructions say."
  }]
}
```

A check is any script whose last line of output says whether a fix is due:

```sh
#!/bin/sh
# .kiln/monitors/bugs: ask for a fix while bugs/ holds a bug report.
first=$(ls bugs/*.md 2>/dev/null | head -n 1)
[ -n "$first" ] || { echo '{"fix": false, "summary": "bugs/ holds no bug"}'; exit 0; }
cat "$first"
echo '{"fix": true, "summary": "a bug waits in bugs/"}'
```

```console
$ kiln monitor install            # once: a launchd job then runs kiln monitor run each hour
$ kiln monitor run bugs           # or run one now
$ kiln status
monitors:
bugs: landed, last ran 2026-11-17 00:24, runs next after 2026-11-17 00:54
```

kiln runs its own repository this way. Its [`bugs`](.kiln/monitors/bugs) monitor fixes what is filed under
`bugs/`, each with a test that fails first. `code-health` refactors the files a gauge scores lowest, `uat` tidies
the acceptance tests of accepted specs, `docs` reads the docs of changed packages against their code, and
`gate-time` makes the tests faster once the gate's times in kiln's ledger rise. An
unattended session runs under the sign-in `claude` has on the machine.

## Commands

```text
kiln init [--preset <preset>]    write kiln's defaults into .kiln/, or update them, and install the pre-commit hook
kiln queue <name>                queue a build of .kiln/specs/<name>.md, as main holds it
kiln fire                        build each queued spec, one at a time, until none is queued
kiln status                      show where specs and monitors stand, each gauge's reading, and whether .kiln/ is behind
kiln tui [<name>]                show every build, live and past, in a full-screen view
kiln accept <name>               take the work: retire the spec, land its amendments, or land a waiting fix
kiln reject <name>               turn the amendments away, or drop a waiting fix
kiln monitor install             set up the launchd job that runs kiln monitor run each hour
kiln monitor run [<name>]        run each monitor that is due, or one now
kiln monitor show <name>         show how a monitor's last run ended
```

## Read more

- [kiln on one page](https://fwilkerson.github.io/kiln/): what kiln offers, and the monitors it runs on itself.
  `docs/one-page.txt` is its source, for [onepage](https://github.com/fwilkerson/onepage).
- [docs/reference.md](docs/reference.md): each key of `kiln.json`, the agents' files, and what kiln records.
- `go doc ./internal/kiln`: every state a spec moves through, as a diagram, and what each command refuses.
- [PRODUCT.md](PRODUCT.md): each spec kiln was built from. kiln builds itself.

## Working on kiln

```sh
go test ./...                 # the gate. The terminal UI's scenarios need a real terminal, and skip in a sandbox.
go run ./tools/install        # reinstall kiln and its zsh completion from main's tip
```

| Path                    | Holds                                                                                 |
|-------------------------|---------------------------------------------------------------------------------------|
| `cmd/kiln`              | The command line.                                                                     |
| `internal/kiln`         | Queuing, building, gating, landing, accepting and rejecting a spec, and the monitors. |
| `internal/tui`          | The full-screen view of `kiln tui`, on Bubble Tea.                                    |
| `internal/claude`       | Claude Code sessions, run headless and sandboxed.                                     |
| `internal/git`          | Checkouts, the commit a build lands as, and the one move of main.                     |
| `internal/proc`         | Child processes.                                                                      |
| `internal/scenariotest` | A scripted `claude`, and the harness the acceptance tests run on.                     |
| `internal/termtest`     | A pseudo-terminal that runs a program and reads its screen.                           |
| `uat/`                  | The acceptance tests: `uat/<spec>/` until a spec is accepted, then `uat/testdata/`.   |
| `.kiln/`                | kiln's own settings, agents, monitors and specs.                                      |
| `docs/`                 | The reference, and how kiln's Go code is written and judged.                          |

Each package's doc opens with a sample and a diagram: `go doc ./internal/git`. The rules the code is held to are
in [docs/guide.md](docs/guide.md) and [docs/practices.md](docs/practices.md), from the module
[practices](https://github.com/fwilkerson/practices), whose commands `go test ./...` runs.

## License

[Apache 2.0](LICENSE).
