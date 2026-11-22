# kiln

Approve a spec, walk away, and come back to it built, tested and on main.

A coding agent writes code fast, but someone has to check that code, and today that is you. You prompt, wait, read
the diff, run the tests, and prompt again. Nothing but you checks the work, so you cannot walk away. A second agent
only gives you more to check.

kiln does that checking before the work reaches you. You say what done means before the work starts. A spec holds
one feature's acceptance tests, and your project's checks, such as `go test ./...`, must pass too. kiln then runs
[Claude Code](https://claude.com/claude-code) sessions with no one at the keyboard. A failed check goes back to the
session, and only work that passes lands on main. A build asks you nothing while it runs.

When a build still gets something wrong, you change what kiln held the build to: the spec, the gate in
`.kiln/kiln.json`, or an agent's instructions in `.kiln/agents/`. The next build starts from that change, so you
correct a mistake once.

![You shape and queue a spec; kiln builds, gates, audits and lands it on main.](docs/flow.svg)

kiln is one Go binary, for macOS. The docs use these words:

- A **spec** is a Markdown file that says what to build, and how to test it. You approve the spec before any code
  exists.
- A **session** is one run of Claude Code. kiln runs each session headless, in a sandbox.
- The **gate** is your project's list of checks. kiln runs the gate on each build's work.
- The **audit** is a session that wrote none of the work. The audit reads the work once the gate passes, and sends
  its findings back to be fixed.
- A **monitor** is a job kiln runs on a schedule. A monitor's check looks for a problem, such as a filed bug, and a
  session fixes what the check finds.

## Install

kiln needs macOS, git 2.55 or newer, and the `claude` command of Claude Code, signed in. Xcode's git may be older than
2.55. kiln builds on the branch named `main`, and never pushes.

```sh
go install github.com/fwilkerson/kiln/cmd/kiln@latest        # with Go 1.27 or newer
```

Or take a built binary, for Apple silicon (`arm64`) or Intel (`amd64`):

```sh
curl -fsSL https://github.com/fwilkerson/kiln/releases/latest/download/kiln_darwin_arm64.tar.gz | tar -xz
mv kiln_darwin_arm64/kiln ~/.local/bin/        # or any directory on your PATH
```

The archive also holds `_kiln`, kiln's zsh completion. Put `_kiln` in a directory that `fpath` names.

## Quick start

**1. Set the project up.** `kiln init` writes kiln's settings and agents into `.kiln/`, with the `/shape` skill and
its reviewers. It also installs a git pre-commit hook that runs your gate. A preset is the set of defaults for one
language. `kiln init` picks the preset by the files your repository holds:

- `go`, for a `go.mod`.
- `python`, for uv, pytest, ruff and ty, by a `pyproject.toml` and `uv.lock`.
- `typescript`, for npm, tsc and Node's test runner, by a `tsconfig.json` and `package-lock.json`.

When no preset matches, or more than one does, `kiln init` refuses and asks for `--preset`.

```console
$ cd my-project                      # a git repository, on the branch main
$ kiln init
picked the preset go, since the repository holds go.mod
wrote .kiln/agents/audit.md
wrote .kiln/agents/build.md
...
wrote .kiln/generated/kiln.schema.json
wrote .kiln/generated/monitor.schema.json
wrote .kiln/kiln.json
wrote .kiln/reviewers/comprehension.md
...
wrote .kiln/generated/init.json
linked .claude/skills/shape to ../../.kiln/skills/shape
linked .claude/agents/comprehension.md to ../../.kiln/reviewers/comprehension.md
...
commit .kiln/ on main: kiln reads it there
the pre-commit hook runs the gate on each commit to main, so make its commands pass before you commit .kiln/:
  go vet ./...
  go test ./...
installed /path/to/my-project/.git/hooks/pre-commit
$ git add .kiln .claude && git commit -m "chore: set up kiln"
```

Read `.kiln/kiln.json` once. Here is the go preset's `kiln.json`, without its two sandbox keys, `"allowedHosts"` and
`"writablePathCommands"`:

```json
{
  "$schema": "./generated/kiln.schema.json",
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "acceptanceTests": "acceptance/{spec}/"
}
```

`gate` lists the commands that must pass before any work lands. A failing gate sends the work back to the session, and
each time is a round. `rounds` is how many rounds a build gets before it stops. `"acceptanceTests"` is where the
acceptance tests go. `$schema` names a schema in `.kiln/generated/`, so an editor completes each key. [The
reference](docs/reference.md#kilnjson) explains every key.

**2. Write a spec.** In an interactive Claude Code session, run `/shape` and say what you want. `/shape` asks you
questions, has the project's reviewers read the draft, and writes `.kiln/specs/<name>.md`. A reviewer is an agent
that reads a spec before you approve it. Each acceptance test in the spec says what done means:

```markdown
### 1. The answer is printed

- **Given**: nothing.
- **When**: someone runs `answer`.
- **Then**: it prints 42.
```

When you approve the spec, `/shape` marks it `Status: Approved`, commits it on main, and queues it with
`kiln queue answer`. kiln builds only what main holds.

**3. Build it.**

```console
$ kiln fire
answer: landed on main at 3f9c2ab81d0e, and waits for UAT
  4.2 min · $0.42 · 3 turns · 1 failed call
  1 of 3 rounds · the audit sent findings back
  2 files changed, 18 insertions, 0 deletions
  next: read it with git show 3f9c2ab81d0e, then run kiln accept answer, or change the spec and run
        kiln queue answer
```

`kiln fire` builds each queued spec in turn, and exits when none is left. A landed build waits for UAT, user
acceptance testing: you try the work on main, then accept it. `kiln fire` notifies you when a build needs you. The
notice shows in the terminal, or goes to the `notify` program that `kiln.json` names. In a second terminal,
`kiln tui` shows each session live, and every past build.

**4. Take the work, or send it back.** A build ends in one of three states. `kiln status` shows each spec's state:

| The build                 | Means                                  | You run                                      |
|---------------------------|----------------------------------------|----------------------------------------------|
| landed, and waits for UAT | The work is on main. Try it.           | `kiln accept <name>`                         |
| waits on its branch       | The build asks to amend the spec.      | `kiln accept <name>` or `kiln reject <name>` |
| stopped                   | Usually, the last round's gate failed. | fix main or the spec; `kiln queue <name>`    |

`kiln accept` retires the spec: it deletes the spec from main, and adds a line to `PRODUCT.md`. That line holds the
`git show` command that prints the spec. If UAT finds a problem, revise the spec with `/shape`. `/shape` commits the
revision and queues the spec again. The new build starts from main, so the build revises the landed work.

## What kiln holds a build to

A build runs with no one watching, so kiln holds each build to these:

- **Your gate.** kiln reads `kiln.json` from main, never from the build's checkout, so no build weakens its own
  gate. A test that also fails on main does not fail the build.
- **Acceptance tests written apart from the code.** With `"verifySession": true`, one session writes the code and
  another writes the tests, from the spec alone. Neither session can read the other's checkout. Otherwise,
  one session writes both.
- **An audit.** The audit reads the work once the gate passes, before the work lands.
- **A sandbox, while the session runs.** A session's shell commands write only in the session's checkout, and in the
  paths that the commands of `kiln.json`'s `"writablePathCommands"` print. A session's shell commands reach only the
  hosts that `kiln.json`'s `"allowedHosts"` lists. kiln's hook keeps a session's Edit and Write tools off the paths
  kiln guards, such as `.kiln/kiln.json`. The gate runs outside the sandbox, as your own `go test` does.
- **Your spec's wording.** A build that finds the spec wrong writes an amendment, and waits on its branch for you.
  No build changes what the spec asks for on its own.

With `"verifySession": true`, a build runs this way. kiln stacks the two sessions' commits on one branch before the
gate:

```text
                    ┌─ build session, in its own clone:   the code  ─┐
  kiln fire ─▶ main ┤                                                ├─▶ stacked ─▶ gate ─▶ audit ─▶ land
                    └─ verify session, in its own clone:  the tests ─┘
```

## Monitors

A monitor does work that otherwise waits for a free hour, such as fixing a filed bug or a stale doc. kiln runs a
monitor with no terminal open, under the sign-in that `claude` has on the Mac. A monitor's check looks for a
problem. When the check finds one, the monitor's agent fixes it. kiln gates, audits and lands the fix like any build.
kiln ships four monitors:

| Monitor        | What it fixes                                                                        |
|----------------|--------------------------------------------------------------------------------------|
| `bugs`         | each bug report filed under `bugs/`, one fix at a time, with a test that fails first |
| `go/deps`      | each Go module behind its newest release that is at least 7 days old                 |
| `go/docs`      | each Go package doc that a change made false                                         |
| `go/gate-time` | a gate that slowed, by making the Go tests faster                                    |

A fix of `go/deps`, `go/docs` or `go/gate-time` lands only once you accept it, such as with `kiln accept deps`.

```console
$ kiln monitor add bugs           # writes .kiln/monitors/bugs/, for you to commit on main
$ kiln monitor install            # once: a launchd job then runs kiln monitor run each hour
$ kiln monitor run bugs           # or run one now
$ kiln status
monitors:
bugs: landed, last ran 2026-11-17 00:24, runs next after 2026-11-17 00:54
```

A monitor is a directory, and the project owns its copy once added:

```text
.kiln/monitors/bugs/
  check          a script whose last line of output says whether a fix is due
  agent.md       the agent that fixes what the check found
  monitor.json   how often it runs, {"hours": 1}, and what its fix may do
```

A check can be any script. This short one asks for a fix while `bugs/` holds a report:

```sh
# .kiln/monitors/bugs/check: ask for a fix while bugs/ holds a bug report.
first=$(ls bugs/*.md 2>/dev/null | head -n 1)
[ -n "$first" ] || { echo '{"fix": false, "summary": "bugs/ holds no bug"}'; exit 0; }
cat "$first"
echo '{"fix": true, "summary": "a bug waits in bugs/"}'
```

kiln runs six monitors on its own repository, in `.kiln/monitors/`. Four do the jobs of the monitors above, such as
[`bugs`](.kiln/monitors/bugs/). Two are kiln's own. `code-health` refactors the Go files that a gauge scores lowest.
`uat` tidies the acceptance tests of accepted specs.

## Gauges

A gauge suits a number you watch across landings, such as test time or a mutation score. kiln reads each gauge after
every landing. `kiln status` shows the latest reading, and `kiln tui` shows the reading at each landing. A gauge
never blocks a landing. A monitor that holds only a gauge and its `monitor.json`, as `gate-seconds` above does,
watches the number and fixes nothing. [Gauges](docs/reference.md#gauges) says the rest.

## Making kiln your own

A project can change kiln in five places. Each is a file or a directory, and [the reference](docs/reference.md)
covers each in full.

**A preset** of a team's own is a directory, laid out as kiln's [built-in presets](internal/kiln/presets/) are.
`kiln help presets` says what each file holds:

```console
$ ls ../presets/web
audit-tests.md  build-checks.md  kiln.json  verify-tests.md
$ kiln init --preset ../presets/web
```

**A monitor** is a job kiln runs on a schedule, as [Monitors](#monitors) above shows. A team can keep a monitor in a
directory, and add the monitor to each project, as [the reference](docs/reference.md#monitors) shows:

```console
$ ls ../monitors/deps
agent.md  check  monitor.json
$ kiln monitor add ../monitors/deps
```

**A reviewer** is a file in `.kiln/reviewers/`. After you add a reviewer, run `kiln init`, which links the file into
`.claude/agents/` for Claude Code, as [Reviewers](docs/reference.md#reviewers) shows:

```markdown
---
name: upgrade
description: Reports each stored record a spec's plan would read wrong. Give it the spec's path.
tools: Read, Grep, Glob
---

Report each record already written that the spec's Technical Plan would refuse or read wrong.
```

**The notify program** receives a notice when a build or a monitor's fix needs you. `{"notify": ".kiln/notify"}` in
`kiln.json` names the program. kiln passes the notice in `$KILN_NOTICE`, as [notify](docs/reference.md#notify) shows:

```sh
#!/bin/sh
# .kiln/notify: show each notice in macOS's Notification Center.
set -eu
said=$(printf '%s' "$KILN_NOTICE" | jq -r '"\(.name) \(.state)"')
osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "kiln"' -e 'end run' "$said"
```

**A gauge** is a monitor's script that prints one number, as [Gauges](#gauges) above shows. A gauge can read kiln's
own numbers from `kiln ledger`. This gauge prints the slowest of the last twenty gates that passed on a build's work.
It skips each landing's gate, the one run on the work rebased onto main. The gauge lives in
`.kiln/monitors/gate-seconds/`, beside a `monitor.json` of `{"atMost": 300}`, as [gauges](docs/reference.md#gauges)
shows:

```sh
#!/bin/sh
# .kiln/monitors/gate-seconds/gauge
set -eu
gates=$("$KILN" ledger --kind gate)
printf '%s\n' "$gates" | jq -s 'map(select(.passed and (.landing | not)) | .seconds) | .[-20:] | max // empty'
```

## After an upgrade, or a change to `.kiln/`

Run `kiln init` again after you upgrade kiln, and whenever `kiln status` says `.kiln/` is behind. `kiln init` merges
kiln's new agents and `/shape` into your edited copies. In your `kiln.json`, `kiln init` only adds `$schema` or `rounds`
when either is missing:

```console
$ kiln init
updated .kiln/agents/audit.md
merged .kiln/agents/build.md
merged .kiln/agents/verify.md, with conflicts: resolve them before you commit it
wrote .kiln/generated/init.json
...
$ git diff .kiln                     # read what changed, resolve each conflict, then commit
```

After you change a file under `.kiln/`, run `kiln doctor` before you commit. `kiln doctor` prints each problem that
would make kiln refuse or fail later, with its fix. A line that starts `note:` does not count as a problem:

```console
$ kiln doctor
.kiln/kiln.json names the host "https://proxy.golang.org/": name a bare host, such as "proxy.golang.org", or "*.example.org"
the working tree holds .kiln/monitors/deps/check without the executable bit, so kiln cannot run it: run chmod +x .kiln/monitors/deps/check, and commit it
note: .kiln/kiln.json differs from main's in allowedHosts: kiln acts on main's, so commit it to main to act on yours
kiln doctor: found 2 problems
```

## Commands

```text
kiln init [--preset go|python|typescript|<dir>]
                         write a preset of kiln's defaults into .kiln/, or merge a newer kiln's into yours,
                         from the preset .kiln/generated/init.json records, or else the one your files match;
                         and install the pre-commit hook
kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
kiln fire                build each queued spec, one at a time, until none is queued
kiln status              show the specs, the monitors, each gauge's reading, and whether .kiln/ is behind
kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
kiln monitor add [<name>|<dir>]
                         copy a monitor into .kiln/monitors/: one kiln ships, such as bugs or go/docs, or the
                         one in a directory, by a path that starts with . or /; with neither, list those kiln
                         ships, and the preset each fits
kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
kiln monitor run [<name>]
                         run each monitor that is due; or run one now, due or not
kiln monitor show <name> show how a monitor's last run ended, then wait for return
kiln doctor [<name>]     print each problem in the project's setup, with its fix; or a spec's form too
kiln ledger [--kind <kind>] [--name <name>] [--last <n>]
                         print what kiln's ledger holds, one JSON object a line, for a script to read
kiln help [<topic>]      print this usage and the topics of kiln's docs, as kiln --help and kiln -h do; or the
                         part of kiln's docs a topic names, such as kiln help monitors
```

## Read more

- [kiln on one page](https://fwilkerson.github.io/kiln/): what kiln offers, and the monitors kiln runs on itself.
  `docs/one-page.txt` is the page's source, which [onepage](https://github.com/fwilkerson/onepage) renders.
- [docs/reference.md](docs/reference.md): each key of `kiln.json`, the agents' files, and what kiln records.
- `kiln help`: the topics of kiln's docs. `kiln help specs` prints a diagram of every state a spec moves through.
- [PRODUCT.md](PRODUCT.md): each spec kiln was built from, since kiln builds itself.

## Working on kiln

```sh
go test -timeout 30m ./...    # the gate. The terminal UI's scenarios need a real terminal, and skip in a sandbox.
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
| `internal/hometest`     | A HOME for tests, with no `GIT_*` variable that aims their git elsewhere.             |
| `internal/scenariotest` | A scripted `claude`, and the harness the acceptance tests run on.                     |
| `internal/termtest`     | A pseudo-terminal that runs a program and reads its screen.                           |
| `uat/`                  | The acceptance tests: `uat/<spec>/` until a spec is accepted, then `uat/testdata/`.   |
| `.kiln/`                | kiln's own settings, agents, monitors and specs.                                      |
| `docs/`                 | The reference, and how kiln's Go code is written and judged.                          |

Each package's doc opens with a sample and a diagram: `go doc ./internal/git`. [docs/guide.md](docs/guide.md) holds
the rules for Go code, from the module [practices](https://github.com/fwilkerson/practices). `go test ./...` runs the
practices module's checks. [docs/practices.md](docs/practices.md) adds kiln's own rules.

## License

[Apache 2.0](LICENSE).
