# Reference

What a project tells kiln in `.kiln/`, and what kiln records in `.git/kiln/`. The [README](../README.md) shows the
commands in use.

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

`kiln init` writes these from a preset, all but the monitors, and links `/shape` and each reviewer into `.claude/`. The
project owns each from then on, except the files in `generated/`. After an upgrade, `kiln init` merges kiln's new
defaults into your edits, for you to read and commit. `kiln status` says when `.kiln/` on main is behind. `kiln.json`
stays yours: kiln init adds only `$schema` and `rounds`, where the file lacks them. `kiln help presets` says how `kiln
init` merges each file. `kiln monitor add` writes a monitor, as Monitors says.

A build writes nothing under `.kiln/` but its spec's amendment file. A file meant for `.kiln/` or `.claude/`, such
as a new agent, is yours to commit: a build names it in its last reply instead of writing it.

## Presets

A preset of your own is a directory laid out as kiln's in [`internal/kiln/presets/`](../internal/kiln/presets/). Keep
yours in a repository of its own, and name it with `--preset` by a path that starts with `.` or `/`, as in
`kiln init --preset ../presets/go`. `kiln help presets` says what each file holds, what each Markdown file's text
replaces, and how kiln reads the path and finds it again on the next run.

## kiln.json

```json
{
  "$schema": "./generated/kiln.schema.json",
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "allowedHosts": ["proxy.golang.org", "sum.golang.org"],
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

| Key                      | What it holds                                                                | Read at           |
|--------------------------|------------------------------------------------------------------------------|-------------------|
| `"$schema"`              | The JSON Schema an editor checks this file against, which kiln never reads   | never             |
| `"gate"`                 | Commands that must pass before work lands, each run with `sh -c`. Required.  | main's tip        |
| `"rounds"`               | How many times a failing gate goes back to the session before a build stops  | main's tip        |
| `"allowedHosts"`         | The only hosts a session's commands reach                                    | main's tip        |
| `"writablePathCommands"` | Commands that print the paths a session may write, besides its checkout      | main's tip        |
| `"mcpServers"`           | The only MCP servers a session calls, as Claude Code's settings hold one     | main's tip        |
| `"acceptanceTests"`      | Where a spec's acceptance tests live, with `{spec}` for its name. Required.  | the commit queued |
| `"verifySession"`        | `true`: a second session writes the tests; absent: one writes code and tests | the commit queued |
| `"testHelpers"`          | Paths the verify session may write besides the tests, such as a test harness | the commit queued |
| `"reaudit"`              | Paths whose changes the audit must read before the work lands                | main's tip        |
| `"notify"`               | A program that receives each notification, run with `sh -c`                  | main's tip        |
| `"timeout"`              | How long a command may run, or a session be silent, before kiln kills it     | main's tip        |

kiln reads the gate from main's tip, never from the build's checkout, so a build cannot weaken its own gate. It
refuses a `kiln.json` with a key it does not know. `kiln doctor` reads the working tree's `kiln.json`, before you
commit it, and prints each refusal any kiln command would make of it.

`"$schema": "./generated/kiln.schema.json"` points an editor, such as GoLand or VS Code, at the schema `kiln init`
writes in `.kiln/generated/`. The editor then completes each key and shows its doc. The editor also marks a key kiln
does not know, and a value of the wrong type. The schema holds each key's type, not the rules `kiln doctor` checks, such
as a host written as a bare name.

### gate and rounds

```text
session ──▶ gate ──passes──▶ audit ──no findings──▶ land
   ▲          │                 │
   └─failures─┘◀────findings────┘        each failure sent back spends a round, and findings spend none;
                                         past `rounds`, the build stops
```

- The gate excuses a test that fails at main too. A command names its failed tests in `go test`'s output, or by
  writing JUnit XML to the path kiln gives it in `$KILN_JUNIT`.
- A command that fails and names no test is never excused. Nor is a run that left out a test main ran.
- The pre-commit hook, which `kiln init` installs, runs the gate's commands on each commit to main.
  Where `core.hooksPath` is set, or another pre-commit hook is in place, `kiln init` installs none. Add the line
  `kiln precommit` to your own hook.

### allowedHosts, writablePathCommands and mcpServers

- A session's shell commands run in Claude Code's sandbox. A session's shell commands write only in the session's
  checkout, and in the paths that the commands of `"writablePathCommands"` print. A session's shell commands reach
  only the hosts in `"allowedHosts"`. A session whose sandbox cannot start runs no command.
- Each host is a bare host name, such as `proxy.golang.org` or `*.example.org`, with no scheme, port or path. kiln
  refuses any other. Each command of `"writablePathCommands"` prints only absolute paths, or no session starts.
- A session's shell commands may listen on a local port, so a test that starts a server runs in a session.
- An MCP server runs outside the sandbox. So does each script kiln runs: the gate's commands, a monitor's scripts
  and the `notify` program, each with the developer's environment. `$KILN` holds the kiln that runs it, as
  `kiln help files` says. The gate runs the code a session wrote.
- A session's file tools are held by kiln's hook, not by the sandbox.

### acceptanceTests

```json
{"acceptanceTests": ["uat/{spec}/", "integration/{spec}/"]}
```

One path may be a plain string. `kiln queue` refuses:

- a `kiln.json` that names no path,
- a path without `{spec}`, or one that starts with it,
- a path not in its clean form, as `path.Clean` writes it,
- a list in which one path's part before `{spec}` starts another's, since two specs' paths could then meet,
- a spec whose acceptance path equals a `"testHelpers"` path, holds one, or lies under one.

A project with more than one path says which test goes where in its verify agent, and in the "Acceptance tests"
section of its audit agent.

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
- `reaudit` lists paths that never land unread, such as test helpers or migrations. When the work's change to one
  differs from what the audit last read, the audit reads the work again, as a round.

### notify

kiln notifies you when a build or a fix needs you. Without `notify`, it prints each notification in its output as OSC 9,
which a terminal such as Ghostty shows with `desktop-notifications` on. `kiln monitor show` prints its own OSC 9 even
with `notify` set, by design: a `notify` program may open a tab that runs it, and that tab is what notifies. `notify`
names a program that receives each notification instead:

```json
{"notify": ".kiln/notify"}
```

- `kiln help notifications` says where and how kiln runs the program, and what the notice in `$KILN_NOTICE` holds.
  It says when a build or a fix notifies, how long kiln waits, and what a program that fails changes.
- `kiln monitor install`'s job writes kiln's output to `.git/kiln/monitor.log`, where no terminal shows OSC 9. To
  hear of a fix that job makes, name a `notify` program.
- That job passes on only your `PATH` and `TMPDIR`, beside launchd's own `HOME`. Keep a secret, such as a
  webhook's URL, in a file under your home directory, never in the repository.

This `.kiln/notify` posts each notification to a Slack incoming webhook. Make it executable, and commit it:

```sh
#!/bin/sh
set -eu
printf '%s' "$KILN_NOTICE" |
  jq '{text: "kiln: \(.name) \(.state)\n\(.summary)\nnext: \(.next)"}' |
  curl --silent --show-error --fail --max-time 10 --header 'Content-Type: application/json' \
    --data-binary @- "$(cat "$HOME/.config/kiln/slack-webhook")" >/dev/null
```

Drop the `jq` line to post the notice as it is, for a webhook that reads kiln's JSON. macOS ships `jq` in
`/usr/bin`.

kiln's own repository keeps a `.kiln/notify` for Ghostty. A monitor's fix opens a tab that runs
`"$KILN" monitor show <name>`, behind the tab you work in. The tab notifies you, and a click on the notification
brings it forward. On the first tab, macOS asks to let kiln control Ghostty. Without that, the tab never opens, and
the output says why.

### timeout

```json
{"timeout": "2h"}
```

- `timeout` bounds each command kiln runs for you: the gate's, a monitor's check and command, a gauge, and each
  command of `"writablePathCommands"`. `timeout` bounds how long a session may print nothing, too. The `notify`
  program has a wait of its own, as `notify` above says.
- Past it, kiln kills a command with its process group, and a session with each process it started. The build or fix
  stops, or the check couldn't run, with words that name what ran past, the limit and this key. `kiln help hangs` gives
  the default, and what each ends in.
- It bounds a hang, not a slow run, so set it well above your gate's slowest run on a loaded machine. A suite that
  took 2 minutes alone took up to 27 with eight sessions beside it.

## Monitors

A monitor looks for a problem on a schedule, and fixes it, or only watches a gauge, as [Gauges](#gauges) says. It is
a directory under `.kiln/monitors/`, named for the monitor, and its fix builds on the branch `kiln/<name>`. kiln reads
each file below at main's tip, each optional, and runs each script by its path. So a script's first line, such as
`#!/usr/bin/env python3`, picks its language. kiln refuses a script git holds without the executable bit: `chmod +x`
it, and commit. `kiln doctor` reads each monitor in the working tree, before you commit it, and prints each refusal
`kiln monitor run` would make of it.

`kiln monitor add bugs` writes the `bugs` monitor kiln ships into `.kiln/monitors/bugs/`, and
`kiln monitor add ../monitors/deps` the one in that directory, named as `--preset` names one. Commit what it writes on
main, where kiln reads it. The project owns it from then on. It makes each script it copies executable, as kiln needs.
`kiln monitor add` with neither lists the monitors kiln ships, and the preset each fits. `kiln help monitors` says what
`kiln monitor add` refuses.

When kiln ships a newer `bugs`, or your team changes `../monitors/deps`, `kiln status` says the monitor is behind. `kiln
init` then merges the new version with your edits, as it merges the defaults. A monitor you deleted stays deleted.

```text
.kiln/monitors/bugs/
  check          finds a problem: its last line of stdout is {"fix": true, "summary": "..."}, or false
  gauge          in place of check: a gauge named for the monitor, whose reading past its line asks for a fix,
                 or for you in a monitor with no command and no agent.md
  command        a fix that needs no model, run outside the sandbox
  agent.md       an agent's file, run when command left the gate failing, or is absent
  monitor.json   the keys below, each optional: {"$schema": "../../generated/monitor.schema.json", "hours": 1}
```

| Key             | What it holds                                                                                     |
|-----------------|---------------------------------------------------------------------------------------------------|
| `"$schema"`     | `"../../generated/monitor.schema.json"`, the schema an editor reads, which kiln never reads       |
| `"hours"`       | How often it runs, in whole hours. Its range and default: `kiln help monitors`.                   |
| `"recheck"`     | `false` keeps the check or gauge out of the fix's gate. Absent, the gate runs it again.           |
| `"mustAccept"`  | `true` keeps each fix on its branch until `kiln accept <name>`. Absent, a passing fix lands.      |
| `"writesTests"` | `true` lets a fix write every spec's acceptance tests, as the list below says.                    |
| `"atLeast"`     | Beside a gauge, its line: a reading below it crossed. Name `"atLeast"` or `"atMost"`, never both. |
| `"atMost"`      | Beside a gauge, its line: a reading above it crossed.                                             |

`kiln monitor add` gives a monitor.json it writes the `$schema` key when it lacks one, and `kiln init` writes the
schema.

```text
kiln monitor run ─▶ due? ─▶ check or gauge ─▶ fix: false ─────────────────────────▶ healthy
                                            ├▶ fix: true ─▶ command ─▶ agent ─▶ gate ─▶ lands, or waits for accept
                                            │                                    └────▶ stops, or asks for you
                                            └▶ crossed, with no command or agent ─────▶ asks for you
```

- `kiln monitor install` gives the launchd job the `PATH` it was run with, and its `TMPDIR` if it has one. The
  gate's tests write their temporary files there. Spotlight watches macOS's own temporary directory, so a heavy
  suite runs lighter with `TMPDIR` on a volume that holds `.metadata_never_index`. Set it, then install again.
- A monitor holds a `command`, an `agent.md` or both, and a `check` or a `gauge`, never both. Or it holds a
  `gauge` alone, to watch, as [Gauges](#gauges) says. A helper a script runs goes in a subdirectory, such as
  `lib/`: kiln refuses any other file directly in the monitor's directory. A script finds its own directory as
  `$(dirname "$0")`, and the monitor's name as that directory's.
- The rest of a check's stdout goes to the agent. A check that exits non-zero, or prints no such last line, couldn't
  run, and starts no fix. So does a gauge kiln could not read.
- The agent's first message holds what the check or gauge printed, and what the gate printed after `command`. Then
  it says to fix what the monitor found, as the agent's instructions say: the body of `agent.md`.
- A monitor is due half an hour before its interval has passed, so the hourly job never misses it by a few seconds.
  `kiln help monitors` holds that rule, and `kiln help hangs` says when a check or gauge that ran past `timeout` holds
  it.
- A fix is a build with no spec. The gate holds it, with the check or gauge added unless `"recheck": false`. That
  setting is for a check that reads something a fix cannot change until it is deployed.
- A fix never writes under `.kiln/`. Without `"writesTests": true`, a fix never writes under an
  `"acceptanceTests"` path's part before `{spec}` either, so the fix cannot change a lasting suite.
- An agent asks for you with the last line of its reply: `{"ask": true, "summary": "..."}`. kiln then drops what it
  changed, but in the landing of a fix you accepted, where kiln keeps the fix, and `kiln status` shows the report.
- An agent titles the commit that lands its fix with the last line of its reply: `{"title": "fix(tui): ..."}`, a
  Conventional Commit of at most 72 characters. Without one, the title is `fix(<monitor>): land the monitor's fix`.
- When main moved past a fix that waits for `kiln accept`, the accept may send the fix back to its session.
  `kiln help monitors` says how that landing goes.
- A check's verdict that adds `"drain": true` drains a queue that each fix takes one item from, such as a folder of
  bug reports. After the fix lands, kiln runs the check again at once, due or not, and starts the next fix. The run
  ends when the check asks for no fix, when the monitor is held, or when a fix does not land.
- `kiln monitor run` runs beside `kiln fire`, which builds specs meanwhile, but never beside another `kiln monitor run`.
  So a long run, such as one that drains, is never joined by the next hour's. `kiln help lanes` says when each refuses,
  and how the two share the gate.

A monitor is held, and starts no fix, by how its last fix ended, or by its last `needs you` where it only watches,
until a change releases it. `kiln help monitors` lists each hold and what releases it.

### Gauges

A gauge is a number you watch across landings, such as a mutation score or the tests' time. It is a monitor's `gauge`
script, named for the monitor, with its line in the monitor's `monitor.json`. A monitor that holds a gauge alone,
with no `command` and no `agent.md`, watches it and fixes nothing:

```text
.kiln/monitors/health/
  gauge          prints the number as its last line
  monitor.json   {"$schema": "../../generated/monitor.schema.json", "atLeast": 10}
```

- Its `monitor.json` has exactly one of `"atLeast"` and `"atMost"`, its line. A reading below `"atLeast"`, or above
  `"atMost"`, crossed it.
- kiln reads each gauge after each landing. `kiln status` shows the latest reading, and `kiln tui` each landing's. A
  gauge never blocks a landing, so a line that must hold is a gate command.
- `kiln monitor run` reads it over the whole repository too, when the monitor is due. In a monitor that only
  watches, a crossed reading asks for you rather than for a fix. `kiln help monitors` says when that notifies, and
  which keys of `monitor.json` such a monitor may not name.
- `kiln help gauges` says where and when kiln runs the gauge, what `$KILN_BASE` holds, and how it reads an unknown
  number. So `git diff --name-only --diff-filter=d "$KILN_BASE" HEAD` names the files one build left. A gauge that
  measures one landing's change prints nothing in the monitor's run, which reads the whole repository: `kiln help
  gauges` shows how it tells.
- Print nothing when the landing holds nothing to read. kiln then says the landing read nothing, and `kiln status` keeps
  showing the last reading, with its landing.
- `kiln help monitors` says what `$KILN_BASE` holds in the monitor's run and in its fix's gate, and when that gate
  reads the gauge.
- Read only the files the commit still holds, as `--diff-filter=d` lists them. A change that only deletes files then
  reads no number, and no deleted file fails the gauge.
- Print the same for the same finding, with no line numbers and no timestamps. kiln compares what the gauge printed
  to decide whether anything changed.

A gauge can read kiln's own numbers from `kiln ledger`, as [What kiln records](#what-kiln-records) says. It runs
`"$KILN"`, the kiln that reads it. This `.kiln/monitors/build-cost/gauge`, beside a `monitor.json` of
`{"atMost": 5}`, reads the median cost of the last ten builds that ended, in dollars:

```sh
#!/bin/sh
set -eu
builds=$("$KILN" ledger --kind build)
printf '%s\n' "$builds" | jq -s 'map(select(.outcome != null and .cost_usd != null) | .cost_usd) | .[-10:] | sort
  | if length > 0 then (.[(length - 1) / 2 | floor] + .[length / 2 | floor]) / 2 else empty end'
```

This gauge is `.kiln/monitors/gate-seconds/gauge`, beside a `monitor.json` of `{"atMost": 300}`. The gauge reads the
slowest of the last twenty gates that passed on a build's work, in seconds. The gauge skips each landing's gate, the
one run on the work rebased onto main:

```sh
#!/bin/sh
set -eu
gates=$("$KILN" ledger --kind gate)
printf '%s\n' "$gates" | jq -s 'map(select(.passed and (.landing | not)) | .seconds) | .[-20:] | max // empty'
```

Each gauge takes its output whole first, so a failed `kiln ledger` fails the gauge, rather than reading no number.

## Agents

```markdown
---
tools: Read, Grep, Glob, Bash
model: opus
effort: high
---

You audit a build's work. ...
```

- An agent's file is frontmatter between lines of `---`, then its body. kiln reads `tools`, `model`, `effort` and
  `advisor`, and ignores any other key.
- Without them, a session has every tool and runs Opus. kiln sets no effort and no advisor of its own.
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
finds it. Commit both. Here a project whose stored records outlive its code adds a reader for them, in
`.kiln/reviewers/upgrade.md`:

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
  both. `kiln help presets` says which reviewers `kiln init` refuses, and when it writes the three.
- `/shape` reads two keys of its own, `uncontested` and `rerun`. Claude Code ignores both. Step 4 of
  `.kiln/skills/shape/SKILL.md` says what each does.
- To remove a reviewer, delete its file and run `kiln init`, which removes its link. To keep none, leave a
  `.gitkeep` in `.kiln/reviewers/`, since git keeps no empty folder.

## When a build waits on you

`kiln fire` and `kiln accept` notify you, as `notify` above says, with the spec and its state. Then the build sums
itself up, and its last line is the command to run next.

```text
x: stopped: the gate still failed after its last round:
  12 min · $3.40 · 41 turns · 2 failed calls
  3 of 3 rounds · the audit had not run
  failing: TestAT2 (example.com/scratch)
  the work is on kiln/x, in ~/Library/Caches/kiln/scratch-1f2e3d4c/x
  next: cd ~/Library/Caches/kiln/scratch-1f2e3d4c/x && claude --resume <session>, to ask the session why;
        then fix main or the spec, and run kiln queue x
```

- A stopped build names the command that resumes its session in its checkout, to ask it why. A split build names
  one for each session. A fix made there is lost to `kiln queue <name>`, which builds anew from main.
- `kiln queue` refuses a landed spec whose text on main is unchanged.
- To change an amendment's wording, commit your own wording to the spec, then run `kiln reject <name>` and
  `kiln queue <name>`.
- If the landing of accepted amendments stops, `kiln accept <name>` lands it again.
- kiln signs each commit it makes when your git config sets `commit.gpgSign`. A key that would ask for its passphrase
  stops the landing, never waits: load the key with `ssh-add`, then queue or accept again.
- `kiln tui` shows each build's spec as the commit it was queued at held it.
- A monitor's fix notifies you too, from `kiln monitor run`, as `notify` above says.
  `kiln monitor show <name>` shows how a monitor's last run ended, with its summary and the command to run next.

## Installing from source

`go run ./tools/install`, in a checkout of kiln, reinstalls kiln from main's tip, never the working tree. It
refuses while any kiln drives a build session. `go install` puts kiln in `GOBIN`, and `_kiln` goes to
`$XDG_DATA_HOME/zsh/completions`, which `fpath` must hold. The completion asks `kiln complete` for kiln's commands,
and for the spec names each command would not refuse.

## What kiln records

```text
.git/kiln/
  ledger.jsonl                    each build, session run, gate, outcome, gauge reading and monitor run
  sessions/<id>/stream.jsonl      each session's stream, for why a number is what it is
```

`ledger.jsonl` is kiln's own, and changes with kiln. A script reads it through `kiln ledger`, whose lines are a
contract. `kiln help kiln-ledger` lists each kind of line, each field, and what each option keeps.

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

A landed build's commit message holds its time, cost, turns and failed calls too. Three checkouts belong to no build,
and stay as worktrees `git worktree list` shows, for kiln to reuse: `.main/`, where the gate runs at main, `.gauges/`,
where gauges are read after a landing, and `.monitor-check/`, where a monitor's check or gauge runs. Each sits beside
the builds' checkouts, under `~/Library/Caches/kiln/`. `kiln help files` lists them, and says when kiln removes the
checkouts a build made.