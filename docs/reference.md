# Reference

What you write under `.kiln/`, what to do when kiln waits on you, and what kiln records. The [README](../README.md)
is the quick start. `kiln help <topic>` prints the rules kiln follows, and a section that has a topic ends with it.

```text
.kiln/
  kiln.json                  the settings below
  specs/<name>.md            a spec, and <name>.amend.md, the amendments a build asks for
  agents/build.md            the build agent: frontmatter, then its prompt
  agents/verify.md           how the acceptance tests' code is written, and the verify agent of a split build
  agents/audit.md            the audit, which reads the work once the gate passes
  reviewers/<name>.md        each reviewer /shape runs on a spec before you approve it, as Reviewers below says
  skills/shape/SKILL.md      /shape, which writes a spec with you
  skills/shape/template.md   the form kiln queue holds a spec to
  monitors/<name>/           a monitor: its check or gauge, its command or agent if it fixes, and its monitor.json
  generated/                 the files kiln owns, which kiln init writes over each time, so never edit one:
    kiln.schema.json         kiln.json's JSON Schema, which an editor reads by kiln.json's "$schema"
    monitor.schema.json      monitor.json's JSON Schema, which an editor reads by each monitor.json's "$schema"
    init.json                what kiln init and kiln monitor add last wrote, which kiln init merges your edits against
    .gitattributes           marks each file there as generated, so a tool such as GitHub collapses its diff
```

`kiln init` writes each file from a preset, except the monitors, and links `/shape` and each reviewer into
`.claude/`. Each file is yours to edit from then on, except those in `generated/`. kiln reads `.kiln/` from main, so
commit a change before you expect kiln to act on it. Before you commit, `kiln doctor` reads the working tree. It prints
each problem that would make kiln
refuse, with its fix.

After you upgrade kiln, run `kiln init` again. It merges kiln's new defaults into your edits, for you to read and
commit, and `kiln status` says when `.kiln/` on main is behind. In `kiln.json`, `kiln init` adds only `$schema` and
`rounds`, where the file lacks them.

A build writes nothing under `.kiln/` but its spec's amendment file. A file meant for `.kiln/` or `.claude/`, such
as a new agent, is yours to commit: a build names it in its last reply instead of writing it.

## Commands

`kiln help` prints this list:

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
kiln tend <name> [--landed]
                         repair a stopped build or landing, or a monitor that waits on you, in a Claude Code
                         session of your own; kiln goes on once it exits; --landed records a stopped build's
                         work landed on main
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

## kiln.json

```json
{
  "$schema": "./generated/kiln.schema.json",
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "allowedHosts": ["proxy.golang.org", "sum.golang.org"],
  "allowUnixSockets": true,
  "writablePathCommands": ["go env GOCACHE GOMODCACHE"],
  "mcpServers": {"docs": {"command": "docs-mcp"}},
  "acceptanceTests": "acceptance/{spec}/",
  "verifySession": true,
  "testHelpers": ["internal/testutil/"],
  "reaudit": ["migrations/"],
  "notify": ".kiln/notify",
  "timeout": "2h"
}
```

| Key                      | What it holds                                                                | When absent                       |
|--------------------------|------------------------------------------------------------------------------|-----------------------------------|
| `"$schema"`              | The JSON Schema an editor checks this file against                           | no editor help                    |
| `"gate"`                 | Commands that must pass before work lands, each run with `sh -c`             | kiln refuses the file             |
| `"rounds"`               | How many times a build goes back to its session before the build stops       | the first failing gate stops it   |
| `"allowedHosts"`         | The only hosts a session's shell commands reach                              | a session's commands reach none   |
| `"allowUnixSockets"`     | `true`: a session's commands may connect to, and listen on, any Unix socket  | the sandbox refuses each one      |
| `"writablePathCommands"` | Commands that print the paths a session may write, besides its checkout      | a session writes in its checkout  |
| `"mcpServers"`           | The only MCP servers a session calls, as Claude Code's settings hold one     | a session calls none              |
| `"acceptanceTests"`      | Where a spec's acceptance tests go, with `{spec}` for the spec's name        | `kiln queue` refuses every spec   |
| `"verifySession"`        | `true`: a second session writes the acceptance tests, from the spec alone    | one session writes code and tests |
| `"testHelpers"`          | Paths of helper code the acceptance tests call, such as a test harness       | no helper path                    |
| `"reaudit"`              | Paths whose changes the audit must read before the work lands                | the audit reads the work once     |
| `"notify"`               | A program that receives each notification, run with `sh -c`                  | kiln prints each in its output    |
| `"timeout"`              | How long a command may run, or a session print nothing, before kiln kills it | 1 hour                            |

- kiln reads each key at main's tip, so a committed change takes effect on the next thing kiln does. Three keys
  differ: a build keeps `"acceptanceTests"`, `"verifySession"` and `"testHelpers"` as main held them when
  `kiln queue` ran.
- kiln reads the gate from main, never from the build's checkout, so a build cannot weaken its own gate.
- kiln refuses a `kiln.json` with a key it does not know.
- With `"$schema"`, an editor such as GoLand or VS Code completes each key and shows its doc. The editor also marks
  an unknown key, and a value of the wrong type. The schema holds each key's type, not the rules `kiln doctor`
  checks, such as a host written as a bare name.
- `"mcpServers"` holds each server in the form of Claude Code's own setting of that name. kiln passes it on as written.

### gate and rounds

```text
session ──▶ gate ──passes──▶ audit ──no findings──▶ land
   ▲          │                 │
   └─failures─┘◀────findings────┘        each failure sent back spends a round, and findings spend none;
                                         past `rounds`, the build stops
```

- A build also spends a round when its work conflicts with main, or changes nothing.
- The gate excuses a test that fails at main too. A command names its failed tests in `go test`'s output, or by
  writing JUnit XML to the path kiln gives it in `$KILN_JUNIT`.
- A command that fails and names no test is never excused. Nor is a run that left out a test main ran.
- The pre-commit hook, which `kiln init` installs, runs the gate's commands on each commit to main. Where
  `core.hooksPath` is set, or another pre-commit hook is in place, `kiln init` installs none: add the line
  `kiln precommit` to your own hook.

`kiln help builds` and `kiln help the-gate` say the rest.

### acceptanceTests

Each acceptance test in a spec is a Given, a When and a Then, numbered from 1. A build writes one test in code for
each, at the `"acceptanceTests"` path, with the spec's name in place of `{spec}`. The test checks each clause of its
Then, through the program as its user reaches it. In kiln's presets, that is the command line or HTTP:

| Preset       | `"acceptanceTests"`               | Test N of the spec `login`                         |
|--------------|-----------------------------------|----------------------------------------------------|
| `go`         | `acceptance/{spec}/`              | a Go test whose name starts `TestAT<N>`            |
| `python`     | `tests/acceptance/test_{spec}.py` | the pytest function `test_at<N>`                   |
| `typescript` | `tests/acceptance/{spec}.test.ts` | a `node:test` test whose name starts `login AT<N>` |

- Your project says how a test is written, under "Acceptance tests" in `.kiln/agents/verify.md`. A project may
  write a Go test, a Playwright script, an HTTP request, or a session in a terminal.
- The tests join your project's suite. The gate's commands run them on that build, and on every build after it.
- `kiln accept` deletes the spec, and leaves its tests.
- The audit reads each test's code beside its Given, When and Then. It reports code that checks less than its Then.
- A monitor's fix writes no acceptance test, unless its `monitor.json` says `"writesTests": true`.

The value is one path, or a list, which gives each spec several paths. With a list, say which test goes where in the
verify agent, and under "Acceptance tests" in the audit agent. `kiln queue` refuses a build agent that says where the
tests go:

```json
{"acceptanceTests": ["uat/{spec}/", "integration/{spec}/"]}
```

`kiln queue` refuses:

- a `kiln.json` that names no path,
- a path without `{spec}`, or one that starts with it,
- a path that `path.Clean` would change, apart from a last slash, or one that starts with a slash,
- a list in which one path's part before `{spec}` starts another's, since two specs' paths could then meet,
- a spec whose acceptance path equals a `"testHelpers"` path, holds one, or lies under one.

### verifySession, testHelpers and reaudit

```text
                       ┌─ build session, in its own clone:  the code       ─┐
kiln fire ─▶ main ─────┤                                                    ├─▶ stacked on kiln/<name> ─▶ gate ─▶ audit
                       └─ verify session, in its own clone: the tests      ─┘
                          written from the spec alone, never seeing the code
```

- With `"verifySession": true`, kiln runs a split build. The build session writes nothing under the
  `"acceptanceTests"` paths or in the amendment file. The verify session writes only those, and the `"testHelpers"`
  paths.
- Each session's settings deny it reads of the other's clone.
- kiln gates the build session's work alone first, and its failures go to the build session.
- Then kiln stacks both sides and gates the stack. Its failures go to the verify session. That session fixes its
  tests, or names the tests the code fails, which go to the build session.
- The audit reads the stack, and its findings go to both.
- `"reaudit"` lists paths that never land unread, such as test helpers or migrations. When the work's change to one
  differs from what the audit last read, the audit reads the work again. That second reading spends a round.

### allowedHosts, allowUnixSockets, writablePathCommands and mcpServers

- A session's shell commands run in Claude Code's sandbox. They write only in the session's checkout, and in the
  paths that the commands of `"writablePathCommands"` print. They reach only the hosts in `"allowedHosts"`. A
  session whose sandbox cannot start runs no command.
- Each host is a bare host name, such as `proxy.golang.org` or `*.example.org`, with no scheme, port or path. Each
  command of `"writablePathCommands"` prints only absolute paths, or no session starts.
- A session's shell commands may listen on a local port, so a test that starts a server runs in a session.
- With `"allowUnixSockets": true`, a session's shell commands may connect to and listen on any Unix socket. Set it
  for a tool that opens a Unix socket, such as GoLand, which `go tool inspect` starts. A Unix socket can reach a local
  service outside the sandbox, such as Docker.
- These run outside the sandbox, with the environment kiln has: an MCP server, the gate's commands, the commands of
  `"writablePathCommands"`, a monitor's scripts and the `notify` program. The gate runs the code a session wrote,
  so that code runs outside the sandbox too. `$KILN` holds the path of the kiln that runs each script.
- A session's file tools are held by kiln's hook, not by the sandbox.

### notify

kiln notifies you when a build or a monitor's fix needs you. Without `"notify"`, kiln prints each notification in its
output as OSC 9, an escape sequence that a terminal shows as a desktop notification. Ghostty shows it with
`desktop-notifications` on. With `"notify"`, the program you name receives each notification as JSON, in
`$KILN_NOTICE`:

```json
{"notify": ".kiln/notify"}
```

| Field     | What the notice holds                                                         |
|-----------|-------------------------------------------------------------------------------|
| `kind`    | `spec`, for a spec's build, or `monitor`                                      |
| `name`    | the spec's name, or the monitor's                                             |
| `state`   | where it stands, as `kiln status` words it, such as `stopped`                 |
| `summary` | the lines kiln prints below the state, such as the time, the cost and the why |
| `next`    | the command to run next, or `""` when you have nothing to run                 |
| `top`     | the top directory of the checkout kiln runs in                                |
| `commit`  | the commit that landed the work on main, absent when none did                 |

This `.kiln/notify` posts each notification to a Slack incoming webhook. Make it executable, and commit it:

```sh
#!/bin/sh
set -eu
printf '%s' "$KILN_NOTICE" |
  jq '{text: "kiln: \(.name) \(.state)\n\(.summary)\nnext: \(.next)"}' |
  curl --silent --show-error --fail --max-time 10 --header 'Content-Type: application/json' \
    --data-binary @- "$(cat "$HOME/.config/kiln/slack-webhook")" >/dev/null
```

- Drop the `jq` line to post the notice as it is, for a webhook that reads kiln's JSON. macOS ships `jq` in
  `/usr/bin`.
- The launchd job of `kiln monitor install` writes kiln's output to `.git/kiln/monitor.log`, where no terminal shows
  OSC 9. To hear of a fix that job makes, name a `notify` program.
- That job passes on only your `PATH` and `TMPDIR`, beside launchd's own `HOME`. Keep a secret, such as a webhook's
  URL, in a file under your home directory, never in the repository.
- kiln kills a `notify` program that runs past 30 seconds, and prints the notification as OSC 9 instead.
- `kiln monitor show` prints its own OSC 9 even with `"notify"` set. A `notify` program may open a terminal tab that
  runs it, and that tab is what notifies. kiln's own `.kiln/notify` does so for Ghostty: on the first tab, macOS asks
  to let kiln control Ghostty.

`kiln help notifications` says when a build or a fix notifies.

### timeout

```json
{"timeout": "2h"}
```

- `"timeout"` is a Go duration above zero. It bounds each command kiln runs for you: the gate's, a monitor's check
  and command, a gauge, and each command of `"writablePathCommands"`. It also bounds how long a session may print
  nothing.
- Past it, kiln kills the command with its process group, or the session with each process it started. The build or
  fix stops, or the check couldn't run, and kiln names what ran past, the limit and this key.
- It bounds a hang, not a slow run, so set it well above your gate's slowest run on a loaded machine. A suite that
  took 2 minutes alone took up to 27 with eight sessions beside it.

`kiln help hangs` says what each killed command or session ends in.

## Monitors

A monitor looks for a problem on a schedule, and fixes it. It is a directory under `.kiln/monitors/`, named for the
monitor:

```text
.kiln/monitors/bugs/
  check          finds a problem: its last line of stdout is {"fix": true, "summary": "..."}, or the same with false
  gauge          in place of check: prints a number, and a reading past the limit in monitor.json asks for a fix
  command        a fix that needs no model, run outside the sandbox
  agent.md       an agent's file, run when there is no command, or command left the gate failing
  monitor.json   the keys below
```

A monitor holds a `command`, an `agent.md` or both, and at most one of `check` and `gauge`. With neither, each due
run starts a fix. A monitor with a `gauge` alone watches a number and fixes nothing, as [Gauges](#gauges) says.

| Key             | What it holds                                                                  | When absent            |
|-----------------|--------------------------------------------------------------------------------|------------------------|
| `"$schema"`     | `"../../generated/monitor.schema.json"`, the schema an editor reads            | no editor help         |
| `"hours"`       | Whole hours between runs, from 0 to 65535. 0: each `kiln monitor run` runs it. | 24                     |
| `"recheck"`     | `false` keeps the check or gauge out of the fix's gate                         | the gate runs it again |
| `"mustAccept"`  | `true` keeps each passing fix on its branch until `kiln accept <name>`         | a passing fix lands    |
| `"writesTests"` | `true` lets a fix write every spec's acceptance tests                          | a fix writes none      |
| `"atLeast"`     | Beside a gauge: the lowest reading that asks for no fix                        | kiln refuses a gauge   |
| `"atMost"`      | Beside a gauge: the highest reading that asks for no fix                       | with neither, or both  |

```text
kiln monitor run ─▶ due? ─▶ check or gauge ─▶ fix: false ─────────────────────────▶ healthy
                                            ├▶ fix: true ─▶ command ─▶ agent ─▶ gate ─▶ lands, or waits for accept
                                            │                                    └────▶ stops, or asks for you
                                            └▶ crossed, with no command or agent ─────▶ asks for you
```

**Adding one.** `kiln monitor add bugs` writes the `bugs` monitor kiln ships into `.kiln/monitors/bugs/`.
`kiln monitor add ../monitors/deps` writes the one in that directory, by a path that starts with `.` or `/`. Commit
what it writes on main. The copy is yours from then on. kiln ships four:

| Monitor        | Its check asks for a fix while                       | Its fix                                       | Runs      | Lands           |
|----------------|------------------------------------------------------|-----------------------------------------------|-----------|-----------------|
| `bugs`         | `bugs/` holds a bug report                           | fixes the first, with a test that fails first | each hour | once it passes  |
| `go/deps`      | a Go module has a newer release at least 7 days old  | updates each, and adapts the code             | each week | once you accept |
| `go/docs`      | a Go package's code changed since its docs were read | corrects each doc the code contradicts        | each day  | once you accept |
| `go/gate-time` | the gate takes 50% longer than it did                | makes the slowest Go tests faster             | each day  | once you accept |

When a monitor's source changes, `kiln status` says the monitor is behind, and `kiln init` merges the new version
with your edits. A monitor you deleted stays deleted.

**The scripts.**

- kiln runs each script by its path, so its first line, such as `#!/usr/bin/env python3`, picks its language. kiln
  refuses a script git holds without the executable bit: `chmod +x` it, and commit.
- A helper a script runs goes in a subdirectory, such as `lib/`. kiln refuses any other file directly in the
  monitor's directory. A script finds its own directory as `$(dirname "$0")`, and the monitor's name as that
  directory's.
- A check's last line of stdout is its verdict, and the rest of its stdout goes to the agent. A check that exits
  non-zero, or prints no verdict, couldn't run, and starts no fix. So does a gauge kiln could not read.
- A verdict that adds `"drain": true` is for a queue each fix takes one item from, such as a folder of bug reports.
  After the fix lands, kiln runs the check again at once, and starts the next fix. The run ends when the check asks
  for no fix, when the monitor is held, or when a fix does not land.

**The fix.**

- A fix is a build with no spec, on the branch `kiln/<name>`. `command` runs first. The agent runs when there is no
  `command`, or `command` left the gate failing.
- The agent's first message holds what the check or gauge printed, and what the gate printed after `command`. Its
  instructions are the body of `agent.md`.
- A fix must pass the gate, with the check or gauge added to it. `"recheck": false` leaves them out, for a check that
  reads something no fix changes until the fix is deployed.
- A fix never writes under `.kiln/`. Without `"writesTests": true`, a fix never writes under an
  `"acceptanceTests"` path's part before `{spec}` either, so the fix cannot change a lasting suite.
- An agent asks for you with the last line of its reply: `{"ask": true, "summary": "..."}`. kiln then drops what the
  agent changed, and `kiln status` shows the report. In the landing of a fix you accepted, kiln keeps the fix.
- An agent titles the commit that lands its fix with the last line of its reply: `{"title": "fix(tui): ..."}`, a
  Conventional Commit of at most 72 characters. Without one, the title is `fix(<monitor>): land the monitor's fix`.

**When a monitor runs.**

- `kiln monitor install` sets up a launchd job, which runs `kiln monitor run` each hour.
- A monitor is due half an hour before its interval has passed, so the hourly job never misses it by a few seconds.
- A monitor is held, and starts no fix, by how its last fix ended, until a change releases it. A monitor that only
  watches is held by its last `needs you`.
- `kiln monitor run` runs beside `kiln fire`, but never beside another `kiln monitor run`.
- The launchd job gets the `PATH` that `kiln monitor install` ran with, and its `TMPDIR` if it has one. The gate's
  tests write their temporary files there. Spotlight indexes macOS's own temporary directory, so a heavy suite runs
  lighter with `TMPDIR` on a volume that holds `.metadata_never_index`. Set it, then install again.

`kiln help monitors` lists each hold and what releases it, what `kiln monitor add` refuses, and how an accepted fix
lands once main has moved. `kiln help lanes` says how a monitor's run and `kiln fire` share the gate.

### Gauges

A gauge is a number you watch across landings, such as a mutation score or the tests' time. It is a monitor's `gauge`
script, which prints the number last. The monitor's `monitor.json` holds the gauge's limit, `"atLeast"` or
`"atMost"`. kiln's output calls that limit the gauge's line: a reading below `"atLeast"`, or above `"atMost"`,
crossed its line.

```text
.kiln/monitors/health/
  gauge          prints the number as its last line
  monitor.json   {"$schema": "../../generated/monitor.schema.json", "atLeast": 10}
```

- kiln reads each gauge after each landing. `kiln status` shows the latest reading, and `kiln tui` each landing's. A
  gauge never blocks a landing, so a limit that must hold is a gate command.
- `kiln monitor run` reads the gauge too, over the whole repository, when the monitor is due. A crossed reading
  starts a fix. In a monitor with no `command` and no `agent.md`, it notifies you instead.
- After a landing, `$KILN_BASE` holds main's tip before that landing. So
  `git diff --name-only --diff-filter=d "$KILN_BASE" HEAD` names the files one build left, without those it deleted.
  A change that only deletes files then reads no number, and no deleted file fails the gauge.
- In a monitor's run, `$KILN_BASE` holds git's empty tree, so the same command names every file.
- Print nothing when the landing holds nothing to read. kiln then keeps the last reading, with its landing.
- Print the same for the same finding, with no line numbers and no timestamps. kiln compares what the gauge printed
  to decide whether anything changed.

A gauge can read kiln's own numbers from `kiln ledger`, as [What kiln records](#what-kiln-records) says. This
`.kiln/monitors/gate-seconds/gauge` sits beside a `monitor.json` of `{"atMost": 300}`. It prints the slowest of the
last twenty gates that passed on a build's work, in seconds. It skips each landing's gate, the one run on the work
rebased onto main:

```sh
#!/bin/sh
set -eu
gates=$("$KILN" ledger --kind gate)
printf '%s\n' "$gates" | jq -s 'map(select(.passed and (.landing | not)) | .seconds) | .[-20:] | max // empty'
```

The script takes `kiln ledger`'s output whole first, so a failed `kiln ledger` fails the gauge, rather than reading
no number.

`kiln help gauges` says where kiln runs a gauge, and how kiln reads a number it cannot parse.

## Agents

An agent's file sets a session's tools, model and instructions. `kiln init` writes three: `build.md`, `verify.md` and
`audit.md`, in `.kiln/agents/`.

```markdown
---
tools: Read, Grep, Glob, Bash
model: opus
effort: high
---

You audit a build's work. ...
```

- An agent's file is frontmatter between lines of `---`, then its body. kiln reads four keys, and ignores any other:

  | Key       | What it holds                                             | When absent       |
  |-----------|-----------------------------------------------------------|-------------------|
  | `tools`   | the session's tools, such as `Read, Grep, Glob, Bash`     | every tool        |
  | `model`   | the model, as `claude --model` takes it, such as `sonnet` | Opus              |
  | `effort`  | the effort, as `claude --effort` takes it, such as `high` | Claude Code's own |
  | `advisor` | the model of Claude Code's advisor                        | Claude Code's own |

- A session's system prompt is the file's body, then kiln's contract with the agent, which is compiled into kiln.
  No file changes the contract, the denied commands, or the guard on `.kiln/`.
- `kiln queue` reads the build, verify and audit agents at the commit it queues. Each session of that build runs
  with them.
- Without `"verifySession"`, the build session writes the tests too, so the verify agent's body follows the build
  agent's.

## Reviewers

A reviewer reads a spec for `/shape` before you approve it, and changes nothing. Each is a Claude Code agent's file in
`.kiln/reviewers/`, and `/shape` runs every one there. kiln's builds never run one. `kiln init` writes three:
`zoom-out`, `comprehension` and `critic`.

To add one, write its file, then run `kiln init`, which links it as `.claude/agents/<name>.md`, where Claude Code
finds it. Commit both. This one reads a spec for stored records the new code would misread:

```markdown
---
name: upgrade
description: Reads a spec in .kiln/specs/ before it is approved, and reports each stored record that would read wrong once it is built. It changes nothing. Give it the spec's path.
tools: Read, Grep, Glob
model: opus
# A fix to one record's form can strand another, so /shape runs it once more.
rerun: true
---

You read a spec before anyone builds it. You change nothing.

For each file, table or JSON record whose form the Technical Plan changes, find the code that reads it today. Report
each record already written that the new code would refuse or read wrong, with the line of the spec that changes it.

End with one line: `read N files, found M`.
```

- `name` is the file's name without `.md`, and `description` says when to run it. Claude Code runs a subagent only with
  both.
- `/shape` reads two keys of its own, `uncontested` and `rerun`. Claude Code ignores both. Step 4 of
  `.kiln/skills/shape/SKILL.md` says what each does.
- To remove a reviewer, delete its file and run `kiln init`, which removes its link. To keep none, leave a
  `.gitkeep` in `.kiln/reviewers/`, since git keeps no empty folder.

## Presets

A preset is the set of defaults `kiln init` writes for one language. kiln ships `go`, `python` and `typescript`.
On its first run, `kiln init` picks one by the files your repository holds. A later run takes the preset that
`.kiln/generated/init.json` records:

| Preset       | Your repository holds                   |
|--------------|-----------------------------------------|
| `go`         | `go.mod`                                |
| `python`     | `pyproject.toml` and `uv.lock`          |
| `typescript` | `tsconfig.json` and `package-lock.json` |

When no preset matches, or more than one does, `kiln init` refuses and asks for `--preset`. A preset of your own is a
directory, like each preset kiln ships in [`internal/kiln/presets/`](../internal/kiln/presets/). Name it by a path that
starts with `.` or `/`, as in `kiln init --preset ../presets/rust`. Keep it in a repository of its own, so each project
takes the same one:

```text
../presets/rust/
  kiln.json         the project's kiln.json, which kiln init writes where the checkout has none
  build-checks.md   the text under "How this project checks its work" in .kiln/agents/build.md
  verify-tests.md   the text under "Acceptance tests" in .kiln/agents/verify.md
  audit-tests.md    the text under "Acceptance tests" in .kiln/agents/audit.md
```

`kiln help presets` says how `kiln init` merges each file on a later run, and which reviewers it refuses.

## When a build waits on you

A build ends in one of three states. `kiln status` shows each spec's state, and `kiln tui <name>` opens its build:

| The build                             | Means                                  | You run                                      |
|---------------------------------------|----------------------------------------|----------------------------------------------|
| waiting for UAT                       | The work is on main. Try it.           | `kiln accept <name>`, which retires the spec |
| waiting on its branch with amendments | The build asks to change the spec.     | `kiln accept <name>` or `kiln reject <name>` |
| stopped                               | Usually, the last round's gate failed. | `kiln tend <name>`, or `kiln queue <name>`   |

An amendment is a change a build asks for to a Given, a When or a Then that could not hold. The build finishes the
work to the amended wording, and writes `.kiln/specs/<name>.amend.md` on its branch, `kiln/<name>`. `kiln accept`
folds the amendments into the spec and lands the work. `kiln reject` drops the build and its branch.

`kiln fire` and `kiln accept` notify you, as [notify](#notify) says. Then kiln prints the build's summary, whose
last line is the command to run next:

```text
x: stopped: the gate still failed after its last round:
  12 min · $3.40 · 41 turns · 2 failed calls
  3 of 3 rounds · the audit had not run
  failing: TestAT2 (example.com/scratch)
  the work is on kiln/x, in ~/Library/Caches/kiln/scratch-1f2e3d4c/x
  next: run kiln tend x, to repair it in a session of your own; or kiln queue x, which builds anew
```

- `kiln tend <name>` opens a Claude Code session of your own, in your checkout, on a stopped build or landing, or on
  a monitor that waits on you. Its first message says why it stopped, and where its work and sessions are. Once you
  exit, kiln goes on at once. `kiln help kiln-tend` says how.
- `kiln queue <name>` builds a stopped build anew from main, for a spec you changed.
- `kiln queue` refuses a landed spec whose text on main is unchanged.
- To change an amendment's wording, commit your own wording to the spec, then run `kiln reject <name>` and
  `kiln queue <name>`.
- If the landing of accepted amendments stops, `kiln accept <name>` lands it again.
- kiln signs each commit it makes when your git config sets `commit.gpgSign`. A key that would ask for its passphrase
  stops the landing, and never waits: load the key with `ssh-add`, then queue or accept again.
- `kiln tui` shows each build's spec as the commit it was queued at held it.
- `kiln monitor show <name>` shows how a monitor's last run ended, with its summary and the command to run next. For
  a monitor that waits on a repair, that is `kiln tend <name>`.

## Installing

The README shows `go install` and the built binary. The binary's archive also holds `_kiln`, kiln's zsh completion:
put `_kiln` in a directory that `fpath` names.

In a checkout of kiln, `go run ./tools/install` reinstalls kiln from main's tip, never the working tree. It refuses
while any kiln drives a build session. It puts kiln where `go install` does, and `_kiln` in
`$XDG_DATA_HOME/zsh/completions`, which `fpath` must hold. The completion asks `kiln complete` for kiln's commands,
and for the spec names each command would not refuse.

## What kiln records

```text
.git/kiln/
  ledger.jsonl                    each build, session run, gate, outcome, gauge reading and monitor run
  sessions/<id>/stream.jsonl      each session's stream, for why a number is what it is
  sessions/<id>/tend.md           what a kiln tend session knows of kiln
  sessions/<id>/note.md           the note a kiln tend session leaves for the stopped build
```

`ledger.jsonl` is kiln's own, and changes with kiln. A script reads it through `kiln ledger`, whose lines are a
contract:

```console
$ kiln ledger --kind build --last 1 | jq .
{
  "kind": "build",
  "build": "k3v9q2x7m1ab",
  "name": "x",
  "fix": false,
  "at": "2026-11-18T05:00:00Z",
  "size": 2,
  "outcome": "landed",
  "seconds": 540.2,
  "cost_usd": 1.84,
  "turns": 41,
  "failed_calls": 2,
  "rounds": 1,
  "audits": 1,
  "findings": 1
}
```

A landed build's commit message holds its time, cost, turns and failed calls too. `kiln help kiln-ledger` lists each
kind of line and each field. `kiln help files` lists the checkouts kiln keeps under `~/Library/Caches/kiln/`, and
says when kiln removes one.
