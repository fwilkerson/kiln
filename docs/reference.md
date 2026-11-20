# Reference

What a project tells kiln in `.kiln/`, and what kiln records in `.git/kiln/`. The [README](../README.md) shows the
commands in use.

```text
.kiln/
  kiln.json                  the settings below
  kiln.schema.json           kiln.json's JSON Schema, which an editor reads by kiln.json's "$schema"
  specs/<name>.md            a spec, and <name>.amend.md, the amendments a build asks for
  init.json                  what kiln init last wrote, which its next run merges your edits against
  agents/build.md            the build agent: frontmatter, then its prompt
  agents/verify.md           how the acceptance tests' code is written, and the verify agent of a split build
  agents/audit.md            the audit, which reads the work once the gate passes
  reviewers/<name>.md        each reviewer /shape runs on a spec before you approve it, as Reviewers below says
  skills/shape/SKILL.md      /shape, which writes a spec with you
  skills/shape/template.md   the form kiln queue holds a spec to
  gauges/                    the scripts a gauge's command runs
  monitors/<name>/           a monitor: its check or gauge, its command or agent, and its monitor.json
```

`kiln init` writes these from a preset, all but the monitors, and links `/shape` and each reviewer into
`.claude/`. The project owns them from then on, but for `kiln.schema.json`, which `kiln init` writes over each
time. Run `kiln init` again once you upgrade kiln: it merges kiln's new defaults with your edits, in the working
tree, for you to read and commit. `kiln status` says when `.kiln/` on main is behind. `kiln.json` stays yours:
kiln init adds only `$schema` and `rounds`, where the file lacks them. `go doc ./internal/kiln Repo.Init` says how
it merges each file. `kiln monitor add` writes a monitor, as Monitors says.

A preset of your own is a directory laid out as kiln's in [`internal/kiln/presets/`](../internal/kiln/presets/):
`kiln.json`, then `build-checks.md`, `verify-tests.md` and `audit-tests.md`, the "Tailor this" sections of the
build, verify and audit agents. Keep it in a repository of its own, and name it with `--preset`, as in
`kiln init --preset ../presets/go`. `go doc ./internal/kiln InitOptions` says how kiln reads the path, and finds
it again on the next run.

A build writes nothing under `.kiln/` but its spec's amendment file. A file meant for `.kiln/` or `.claude/`, such
as a new agent, is yours to commit: a build names it in its last reply instead of writing it.

## kiln.json

```json
{
  "$schema": "./kiln.schema.json",
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "hosts": ["proxy.golang.org", "sum.golang.org"],
  "writable": ["go env GOCACHE GOMODCACHE"],
  "mcp": {"docs": {"command": "docs-mcp"}},
  "acceptance": "acceptance/{spec}/",
  "workflow": "split",
  "harness": ["internal/testutil/"],
  "reaudit": ["migrations/"],
  "gauges": [{"name": "test-seconds", "command": ".kiln/gauges/test-seconds", "max": 60}],
  "notify": ".kiln/notify",
  "timeout": "2h"
}
```

| Key          | What it holds                                                               | Read at           |
|--------------|-----------------------------------------------------------------------------|-------------------|
| `$schema`    | The JSON Schema an editor checks this file against, which kiln never reads  | never             |
| `gate`       | Commands that must pass before work lands, each run with `sh -c`. Required. | main's tip        |
| `rounds`     | How many times a failing gate goes back to the session before a build stops | main's tip        |
| `hosts`      | The only hosts a session's commands reach                                   | main's tip        |
| `writable`   | Commands that print the paths, beside its checkout, a session may write     | main's tip        |
| `mcp`        | The only MCP servers a session calls, as Claude Code's settings hold one    | main's tip        |
| `acceptance` | Where a spec's acceptance tests live, with `{spec}` for its name. Required. | the commit queued |
| `workflow`   | `"split"`, or absent for one session that writes code and tests             | the commit queued |
| `harness`    | Paths both sessions of a split build may write                              | the commit queued |
| `reaudit`    | Paths the audit reads again whenever the work changes them                  | main's tip        |
| `gauges`     | Numbers kiln reads after each landing                                       | main's tip        |
| `notify`     | A program that receives each notification, run with `sh -c`                 | main's tip        |
| `timeout`    | How long a command may run, or a session be silent, before kiln kills it    | main's tip        |

kiln reads the gate from main's tip, never from the build's checkout, so a build cannot weaken its own gate. It
refuses a `kiln.json` with a key it does not know. `kiln doctor` reads the working tree's `kiln.json`, before you
commit it, and prints each refusal any kiln command would make of it.

`"$schema": "./kiln.schema.json"` points an editor, such as GoLand or VS Code, at the schema `kiln init` writes beside
`kiln.json`. The editor then completes each key, shows its doc, and marks a key kiln does not know or a value of the
wrong type. The schema holds each key's type, not the rules `kiln doctor` checks, such as a host with no scheme.

### gate and rounds

```text
session ──▶ gate ──passes──▶ audit ──no findings──▶ land
   ▲          │                 │
   └─failures─┘◀────findings────┘        each trip back is a round; past `rounds`, the build stops
```

- The gate excuses a test that fails at main too. A command names its failed tests in `go test`'s output, or by
  writing JUnit XML to the path kiln gives it in `$KILN_JUNIT`.
- A command that fails and names no test is never excused. Nor is a run that left out a test main ran.
- The pre-commit hook, which `kiln init` installs, runs the gate's commands on each commit to main.
  Where `core.hooksPath` is set, or another pre-commit hook is in place, `kiln init` installs none. Add the line
  `kiln precommit` to your own hook.

### hosts, writable and mcp

- A session's commands run in Claude Code's sandbox. They write only in the session's checkout and the paths
  `writable` prints, and reach only `hosts`. A session whose sandbox cannot start runs no command.
- Each host is a bare host name, such as `proxy.golang.org` or `*.example.org`, with no scheme, port or path. kiln
  refuses any other. A `writable` command prints only absolute paths, or no session starts.
- They may listen on a local port, so a test that starts a server runs in a session.
- An MCP server runs outside the sandbox. So does each script kiln runs: the gate's commands, a gauge's, a monitor's
  scripts and the `notify` program, each with the developer's environment. `$KILN` holds the kiln that runs it, as
  `go doc ./internal/kiln` says under Files. The gate runs the code a session wrote.
- A session's file tools are held by kiln's hook, not by the sandbox.

### acceptance

```json
{"acceptance": ["uat/{spec}/", "integration/{spec}/"]}
```

One path may be a plain string. `kiln queue` refuses:

- a `kiln.json` that names no path,
- a path without `{spec}`, or one that starts with it,
- a path not in its clean form, as `path.Clean` writes it,
- a list in which one path's part before `{spec}` starts another's, since two specs' paths could then meet,
- a spec whose name puts one of its paths on a `harness` path, or around or inside one.

A project with more than one path says which test goes where in its verify agent, and in the "Acceptance tests"
section of its audit agent.

### workflow, harness and reaudit

```text
                       ┌─ build session, in its own clone:  the code       ─┐
kiln fire ─▶ main ─────┤                                                    ├─▶ stacked on kiln/<name> ─▶ gate ─▶ audit
                       └─ verify session, in its own clone: the tests      ─┘
                          written from the spec alone, never seeing the code
```

- With `"workflow": "split"`, the build session writes nothing under the `acceptance` paths or in the amendment
  file. The verify session writes only there, and under `harness`.
- Each session's settings deny it reads of the other's clone.
- kiln gates the build session's work alone first, and its failures go to the build session.
- Then kiln stacks both sides and gates the stack. Its failures go to the verify session. That session fixes its
  tests, or names the tests the code fails, which go to the build session.
- The audit reads the stack, and its findings go to both.
- `reaudit` lists paths that never land unread, such as a harness or migrations. When the work's change to one
  differs from what the audit last read, the audit reads the work again, as a round.

### gauges

A gauge is a number you watch across landings, such as a mutation score or the tests' time.

```json
{"name": "health", "command": ".kiln/gauges/health", "min": 10}
```

- Its `command` prints the number as its last line. It has exactly one of `min` and `max`, its line. A reading below
  `min`, or above `max`, crossed it.
- kiln runs the command with `sh -c` at the landed commit, with main's tip before the landing in `$KILN_BASE`. So
  `git diff --name-only --diff-filter=d "$KILN_BASE" HEAD` names the files one build left.
- A reading kiln could not take is unknown, never zero, and never crossed.
- A gauge never blocks a landing. A line that must hold is a gate command.
- Keep its script under `.kiln/gauges/`, where no build can change it.

A monitor's `gauge` is a gauge too, named for the monitor, with its line in the monitor's `monitor.json`. Then:

- The monitor's run gives `$KILN_BASE` as git's empty tree, so the same `git diff` names every file. A command that
  names a commit's parents, such as `git log "$KILN_BASE"..HEAD`, fails there.
- Its fix's gate gives `$KILN_BASE` as the commit where the fix left main, and fails while the reading crosses the
  line. With `"gates": false`, the gate never reads it.
- Read only the files the commit still holds, as `--diff-filter=d` lists them. A change that only deletes files then
  reads no number, and no deleted file fails the command.
- Print the same for the same finding, with no line numbers and no timestamps. kiln compares what the gauge printed
  to decide whether anything changed.

A gauge can read kiln's own numbers from `kiln ledger`, as [What kiln records](#what-kiln-records) says. It runs
`"$KILN"`, the kiln that reads it. The median cost of the last ten
builds that ended, in dollars:

```sh
#!/bin/sh
set -eu
builds=$("$KILN" ledger --kind build)
printf '%s\n' "$builds" | jq -s 'map(select(.outcome != null and .cost_usd != null) | .cost_usd) | .[-10:] | sort
  | if length > 0 then (.[(length - 1) / 2 | floor] + .[length / 2 | floor]) / 2 else empty end'
```

The slowest of the last twenty gates that passed on a build's work, in seconds, as `{"max": 300}` would watch it:

```sh
#!/bin/sh
set -eu
gates=$("$KILN" ledger --kind gate)
printf '%s\n' "$gates" | jq -s 'map(select(.passed and (.landing | not)) | .seconds) | .[-20:] | max // empty'
```

Each takes its output whole first, so a failed `kiln ledger` fails the gauge, rather than reading no number.

### notify

kiln notifies you when a build or a fix needs you. Without `notify`, it prints each notification in its output as
OSC 9, which a terminal such as Ghostty shows with `desktop-notifications` on. `notify` names a program that
receives each one instead:

```json
{"notify": ".kiln/notify"}
```

- kiln reads the key at main's tip, and runs it with `sh -c` at the top of the checkout kiln runs in, outside the
  sandbox, with your environment. A script it names is that checkout's copy. `$KILN_NOTICE` holds the notice, as
  JSON. What the program prints goes to kiln's output.
- The package doc of `internal/kiln` lists the notice's fields, when a build or a fix notifies, and how long kiln
  waits, under Notifications: `go doc ./internal/kiln`.
- A program that fails, hangs or is missing changes no build's or fix's result. kiln says why in its output, then
  prints the notification as OSC 9.
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
`"$KILN" monitor show <name>`, behind the tab you work in. The tab notifies you, and a click on the notification brings it
forward. On the first tab, macOS asks to let kiln control Ghostty. Without that, the tab never opens, and the output
says why.

### timeout

```json
{"timeout": "2h"}
```

- It bounds each command kiln runs for you: the gate's, a monitor's check and command, a gauge, and a `writable`
  command. It bounds how long a session may print nothing, too. The `notify` program has a wait of its own, as `notify`
  above says.
- Past it, kiln kills a command with its process group, and a session with each process it started. The build or
  fix stops, or the check couldn't run, with words that name what ran past, the limit and this key. The package doc
  of `internal/kiln` gives the default, and what each ends in, under Hangs: `go doc ./internal/kiln`.
- It bounds a hang, not a slow run, so set it well above your gate's slowest run on a loaded machine. A suite that
  took 2 minutes alone took up to 27 with eight sessions beside it.

## Monitors

A monitor looks for a problem on a schedule, and fixes it. It is a directory under `.kiln/monitors/`, named for the
monitor, and its fix builds on the branch `kiln/<name>`. kiln reads each file below at main's tip, each optional,
and runs each script by its path. So a script's first line, such as `#!/usr/bin/env python3`, picks its language.
kiln refuses a script git holds without the executable bit: `chmod +x` it, and commit. `kiln doctor` reads each
monitor in the working tree, before you commit it, and prints each refusal `kiln monitor run` would make of it.

`kiln monitor add bugs` writes the `bugs` monitor kiln ships into `.kiln/monitors/bugs/`, and
`kiln monitor add ../monitors/deps` the one in that directory. Commit what it writes on main, where kiln reads it. The
project owns it from then on. `go doc ./internal/kiln` says what `kiln monitor add` refuses, under `MonitorAdd`.

```text
.kiln/monitors/bugs/
  check          finds a problem: its last line of stdout is {"fix": true, "summary": "..."}, or false
  gauge          in place of check: a gauge named for the monitor, whose reading past its line asks for a fix
  command        a fix that needs no model, run outside the sandbox
  agent.md       an agent's file, run when command left the gate failing, or is absent
  monitor.json   the keys below, each optional: {"hours": 1, "tests": true}
```

| Key      | What it holds                                                                                       |
|----------|-----------------------------------------------------------------------------------------------------|
| `hours`  | How often it runs, in whole hours. Its range and default: `go doc ./internal/kiln`, under Monitors. |
| `gates`  | `false` keeps the check or gauge out of the fix's gate.                                             |
| `accept` | `true` keeps each fix on its branch until `kiln accept <name>`.                                     |
| `tests`  | `true` lets a fix write under each `acceptance` path's part before `{spec}`.                        |
| `min`    | Beside a gauge, its line: `min` or `max`, never both, as a gauge in `kiln.json` has.                |
| `max`    |                                                                                                     |

```text
kiln monitor run ─▶ due? ─▶ check or gauge ─▶ fix: false ─────────────────────────▶ healthy
                                            └▶ fix: true ─▶ command ─▶ agent ─▶ gate ─▶ lands, or waits for accept
                                                                                 └────▶ stops, or asks for you
```

- `kiln monitor install` gives the launchd job the `PATH` it was run with, and its `TMPDIR` if it has one. The
  gate's tests write their temporary files there. Spotlight watches macOS's own temporary directory, so a heavy
  suite runs lighter with `TMPDIR` on a volume that holds `.metadata_never_index`. Set it, then install again.
- A monitor holds a `command`, an `agent.md` or both, and a `check` or a `gauge`, never both. A helper a script
  runs goes in a subdirectory, such as `lib/`: kiln refuses any other file directly in the monitor's directory. A
  script finds its own directory as `$(dirname "$0")`, and the monitor's name as that directory's.
- The rest of a check's stdout goes to the agent. A check that exits non-zero, or prints no such last line, couldn't
  run, and starts no fix. So does a gauge kiln could not read.
- The agent's first message holds what the check or gauge printed, and what the gate printed after `command`. Then
  it says to fix what the monitor found, as the agent's instructions say: the body of `agent.md`.
- The package doc of `internal/kiln` says when a monitor is due, under Monitors, and when a check or gauge that ran
  past `timeout` holds it, under Hangs: `go doc ./internal/kiln`.
- A fix is a build with no spec. The gate holds it, with the check or gauge added unless `"gates": false`. That
  setting is for a check that reads something a fix cannot change until it is deployed.
- A fix never writes under `.kiln/`. Without `"tests": true`, it never writes under an `acceptance` path's part
  before `{spec}` either, so it cannot change a lasting suite.
- An agent asks for you with the last line of its reply: `{"ask": true, "summary": "..."}`. kiln then drops what it
  changed, but in the landing of a fix you accepted, where kiln keeps the fix, and `kiln status` shows the report.
- An agent titles the commit that lands its fix with the last line of its reply: `{"title": "fix(tui): ..."}`, a
  Conventional Commit of at most 72 characters. Without one, the title is `fix(<monitor>): land the monitor's fix`.
- When main moved past a fix that waits for `kiln accept`, the accept may send the fix back to its session. The
  package doc of `internal/kiln` says how that landing goes, under Monitors: `go doc ./internal/kiln`.
- A check's verdict that adds `"drain": true` drains a queue that each fix takes one item from, such as a folder of
  bug reports. After the fix lands, kiln runs the check again at once, due or not, and starts the next fix. The run
  ends when the check asks for no fix, when the monitor is held, or when a fix does not land.
- `kiln monitor run` runs beside `kiln fire`, which builds specs meanwhile, but never beside another
  `kiln monitor run`. So a long run, such as one that drains, is never joined by the next hour's. The package doc of
  `internal/kiln` says when each refuses, and how the two share the gate, under Lanes: `go doc ./internal/kiln`.

A monitor is held, and starts no fix, by how its last fix ended, until a change releases it. The package doc of
`internal/kiln` lists each hold and what releases it, under Monitors: `go doc ./internal/kiln`.

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
- Without the split workflow, the build session writes the tests too, so the verify agent's body follows the build
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

- `name` is the file's name without `.md`, and `description` says when to run it. Claude Code runs a subagent only
  with both. `go doc ./internal/kiln Repo.Init` says which reviewers `kiln init` refuses, and when it writes the
  three.
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
contract. `go doc ./internal/kiln` lists each kind of line, each field, and what each option keeps, under kiln ledger.

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

A landed build's commit message holds its time, cost, turns and failed calls too. kiln removes every checkout a
build made once the build lands, is accepted, is rejected or is queued anew. A build that stopped, or waits on its
branch, keeps them.
