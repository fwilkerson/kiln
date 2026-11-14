# Spec 11: Monitors

Status: Approved, 2026-11-14

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

A monitor is a job the developer names in `kiln.json`, which kiln runs on its schedule, in the background, with no
terminal open: hourly, daily, weekly, or any number of hours. It looks for a problem, and when it finds one it fixes
it. The developer comes back to a fix already gated and landed, rather than to an alert. Setting up a monitor is the
developer's approval of what it does: kiln asks nothing of the developer on each run, unless the monitor says its
fixes wait for `kiln accept`.

A monitor is one shape, with three optional steps. Which steps it names is its configuration. It names a command, an
agent, or both, since a check alone finds a problem and fixes nothing:

- A **check**: a command that prints, as its last line, a JSON object saying whether to fix, and a summary. The rest of
  what it prints goes to the agent. A check that exits non-zero couldn't run, such as on a dead network, and starts no
  fix.
- A **command**: a fix that needs no model, such as one that bumps the dependencies.
- An **agent**: a Claude Code session with an agent file under `.kiln/agents/`, and the monitor's prompt. It fixes
  what the check found, or what the command left failing. It may instead end by asking for the developer, with a
  report: a triage agent that reads a service's errors does so for an error it judges a person should look at.

```text
kiln monitor run (launchd wakes it each hour) ─▶ each monitor that is due
  │
  ▼
check ── exits non-zero ──▶ couldn't run: shown in the report, no fix, still due
  │ {"fix": false}  ──────▶ healthy: shown in the report, no fix
  │ {"fix": true}, or no check
  ▼
held? ── the same finding as a fix that stopped, asked or changed nothing ──▶ held: no fix
  │
  ▼
command, then gate (+ check) ── pass ─────────────────────┐
  │ fail, or no command                                   │
  ▼                                                       │
agent, then gate (+ check), up to rounds ── pass ─▶ audit ┤
  │ fail       │ asks for the developer     │ session     ▼
  ▼            ▼                            │ failed    lands on main, or waits for kiln accept
stopped     needs you: its report           ▼
            main as it was                  session failed: its error
```

Each fix is a build with no spec, on the branch `kiln/<name>`, in a worktree, as a spec's build is. kiln's gate holds
it. A monitor whose check reads the repository, such as the dependency check, adds its check to the gate, so the fix
lands only once the check no longer asks for one. A check that reads something outside the repository, such as
Datadog, can't see a fix until it is deployed, so it stays out of the gate. Work that changes nothing on main never
lands.

A fix may write anything a spec's build may, except the acceptance tests, so it can't edit the tests that judge it.
A monitor that refines the acceptance tests says its fixes may write them, with `"tests": true`. kiln refuses a fix's
write to the tests as it refuses a build's write under `.kiln/`: the hook stops it and says why, and the gate fails a
fix that holds one.

`kiln status` and `kiln tui` show each monitor: when it last ran, what came of it, its summary, and when it runs next.
When a fix lands, stops, waits for `kiln accept`, an agent asks for the developer, or a session fails, kiln opens a
Ghostty window in the background. It sends Ghostty's notification, and a click on it brings forward the window, which
shows the summary, or the agent's report, and the command to run next.

The developer's monitors, as configuration:

| Monitor             | Every | Check                                                 | Gates | Command                          | Agent                     | Accept                       |
|---------------------|-------|-------------------------------------------------------|-------|----------------------------------|---------------------------|------------------------------|
| Dependency updates  | 24h   | fix when `go list -m -u` lists a direct module        | yes   | `go get -u ./... && go mod tidy` | fixes what the gate fails | no                           |
| Vulnerabilities     | 24h   | fix when `govulncheck -format json` reports a finding | yes   |                                  | weighs each finding       | no                           |
| Documentation drift | 168h  |                                                       |       |                                  | compares docs with code   | yes                          |
| Acceptance tests    | 168h  |                                                       |       |                                  | consolidates the tests    | yes, and may write the tests |
| A service's errors  | 1h    | fix when Datadog lists an error in the last hour      | no    |                                  | triages: fixes, or asks   | the developer's choice       |

On kiln at `3d40cf0`, on 2026-11-13, `go list -m -u all` listed 22 modules with an update, and 10 after the command,
since `go get -u ./...` leaves the modules only a dependency's tests use. Counting direct modules alone, with
`not .Indirect`, it listed 5, and 0 after. So the dependency check counts direct modules, or it would ask for a fix
each day that no fix gives.

What showed the need: the developer's daily CVE job for another project. A launchd agent ran a
Claude Code session each morning at 10:17 that ran `make vuln` and filed a task for each finding. Each run of the
week before 2026-11-13 reported the same 14 reachable findings, across 5 modules and the standard library. By
2026-11-13, 13 of them waited as filed tasks, and the job had fixed none. The developer disabled it on 2026-11-13.
GitHub's Dependabot does this for a repository on GitHub. kiln's developer keeps off GitHub, and kiln runs on the
developer's machine. The first monitors run on kiln itself.

`docs/later.md` holds two topics this spec takes up. "Repairs" is a session with no Given, When, Then, whose goal is
that one failing command passes: a monitor's agent is one, with the gate as its command. "A project's own workflow"
is a role other than build, verify and audit: a monitor's agent is one.

## Goals

- The developer names any number of monitors in `kiln.json`. Each has a name, how often it runs, and a command, an
  agent with its prompt, or both. It may name a check too. It says whether its check joins the gate, whether its
  fixes wait for `kiln accept`, and which paths its fixes may not write.
- `kiln monitor install` sets up a launchd job for the repository, which runs `kiln monitor run` each hour, and when
  the developer's machine wakes after a missed hour. `kiln monitor run` runs, at main's tip, each monitor `kiln.json`
  names that is due: one that never ran, whose last run couldn't run its check or met a usage limit, or whose last
  run started more than half an hour short of its interval ago. A monitor taken out of `kiln.json` never runs again.
- `kiln monitor run <name>` runs that monitor now, due or not, and held or not.
- A check that exits non-zero, or whose last line is no such object, couldn't run: kiln records its last line of
  output, and starts no fix. A check that prints `"fix": false` is healthy: kiln records its summary, and starts no
  fix.
- A check that prints `"fix": true`, or a monitor with no check, starts a fix: a build with no spec, in a worktree.
  The command runs first. The agent runs when the command left the gate failing, or when the monitor has no command.
  Its session reads what the check printed and what the gate printed. The gate holds the fix, with the check added
  when the monitor says it joins the gate, and the agent gets `kiln.json`'s rounds. An agent's fix gets the audit, as
  a build does.
- A fix that passes lands on main, or waits on its branch when its monitor says so. `kiln accept <name>` lands a
  waiting fix as it lands a build: rebased onto main, gated again, check included, and moved onto main if main hasn't
  moved. A landing that stops is tried again by `kiln accept <name>`. `kiln reject <name>` drops a waiting fix and
  its branch.
- kiln refuses a fix's write under the acceptance path, through the hook and the gate, as it refuses a build's write
  under `.kiln/`, unless its monitor says its fixes may write the tests. The gate's refusal goes back to the agent as
  a round. A fix never writes under `.kiln/`, not even an amendment file.
- A fix that can't pass stops, and main stays as it was.
- An agent may end its session by asking for the developer, with a report. kiln then drops what the fix changed, and
  `kiln status` shows that the monitor needs the developer, with the report's summary.
- A session that fails, such as on an expired sign-in, is its own result: kiln records the session's error, and the
  monitor runs again once its interval has passed, or when the developer runs it by name. A failure that repeats,
  such as a prompt too long, then costs one session each interval, not one each hour. A session that a usage limit
  ends is its own result too: kiln doesn't wait for the limit to reset, as it does for a build, since it would hold
  the run lock until then. It drops the fix, and the monitor stays due. A gated check that couldn't run during the
  gate ends its fix as couldn't run, not stopped, and the monitor stays due and isn't held.
- A monitor is held when its last fix stopped, asked for the developer, or was rejected, and nothing has changed since
  that fix started. For a fix that stopped or was rejected, a change is to main's tip, the monitor's entry in
  `kiln.json`, or what the check printed. For one that asked, a change is to the entry or what the check printed, so
  an hourly monitor that finds the same errors asks once, not each hour. A monitor with a check is held the same way
  when its last fix changed nothing and asked for no one, so an agent that judges a finding noise runs once for it.
  A monitor with no check has no finding to compare, so for it a change to main's tip releases an ask too. A monitor
  whose check stays out of the gate is held after a fix lands, too, until what its check printed changes, since the
  check can't see the fix before it is deployed. A held monitor's check still runs when due, and kiln records that
  it was held.
- One monitor has at most one fix at a time. A monitor whose fix is building, waits for `kiln accept`, or has an
  accepted landing queued starts no other fix, not even when run by name, and kiln records that it was held.
  `kiln monitor run` lands each accepted landing that is queued before it runs any check.
- A fix cut short, such as by a reboot, belongs to its monitor still. `kiln run` never takes it.
  `kiln monitor run` drops it, with its branch, before it runs that monitor, and the monitor runs as if it were due.
  An accepted landing cut short, such as by an interrupt during `kiln accept`, is queued again, not dropped, so no
  fix the developer accepted is lost.
- `kiln monitor run` starts nothing while another `kiln run` or `kiln monitor run` builds in the repository, and
  `kiln run` starts nothing while `kiln monitor run` builds. A monitor that didn't run stays due, for the next hour.
- `kiln status` shows each monitor `kiln.json` names: when it last ran, what came of it, its summary or why it
  couldn't run, and when it runs next. `kiln tui` shows them too, and shows each fix as it shows a build.
- When a fix lands, stops, waits on the developer, or asks for the developer, or a session fails, kiln opens a Ghostty
  window that runs a kiln command. The command sends Ghostty's notification, which names the monitor and what came
  of it, and shows the summary, the agent's report or the session's error, and the command to run next, as `kiln run`
  sums up a build. A healthy run, a check that couldn't run, a held monitor, a fix that changed nothing and asked
  for no one, a session a usage limit ended, and a session that failed as the monitor's last run did open no window.
  So an expired sign-in opens one window for each monitor, not one each run.

## Non-goals

- A store kiln keeps of what monitors found, keyed by a signature and counted. A check that wants to tell a new error
  from one it has seen keeps its own record, as a gauge's command computes its own ratio. A check that reads running
  software prints the same findings the same way each time, such as without timestamps, so the hold can see them as
  the same.
- A workflow of several roles per monitor, such as a triage agent that hands to a separate fix agent. The monitor's
  agent can spawn subagents, as a build's session can. `docs/later.md` "A project's own workflow" holds the rest.
- A monitor kiln ships, or one in a preset's `kiln.json`. Which monitors a project runs is the developer's, as with
  gauges.
- Building a spec the developer queued, in the background. A spec builds when the developer runs `kiln run`.
- A schedule at a time of day, such as Mondays at 9. A monitor runs once its interval has passed since its last run.
- A schedule finer than an hour.

## Open Questions

## Acceptance tests

Each test runs kiln on a scratch repository whose `kiln.json` names one or two monitors, each with a short shell
script for its check and command, and the scripted claude for its agent.

### 1. kiln shows what a check found

- **Given**: a monitor whose check prints that nothing needs a fix, with a summary; and a second monitor whose check
  prints a line and exits non-zero.
- **When**: the developer runs `kiln monitor run`.
- **Then**: main is unchanged, and `kiln status` shows the first monitor healthy, with its summary, and the second
  monitor couldn't run, with that line.

### 2. A monitor runs when it is due, or when named

- **Given**: a daily monitor that ran healthy a moment ago, and a second monitor that has never run.
- **When**: the developer runs `kiln monitor run`, and then `kiln monitor run` with the daily monitor's name.
- **Then**: the first run runs the second monitor's check and not the daily monitor's. The second run runs the daily
  monitor's check.

### 3. A command's fix lands

- **Given**: a monitor whose check joins the gate, and asks for a fix until a file holds a line, and whose command
  writes that line.
- **When**: the developer runs `kiln monitor run`.
- **Then**: main holds the line, and `kiln status` shows the monitor's fix landed.

### 4. The agent fixes what the command left failing

- **Given**: a monitor whose check prints a finding above its last line, whose command leaves a gate command failing,
  and whose agent makes it pass.
- **When**: the developer runs `kiln monitor run`.
- **Then**: the agent's session was told the finding and what the gate printed, and main holds the command's work and
  the agent's.

### 5. A check that stays out of the gate

- **Given**: a monitor whose check doesn't join the gate, and asks for a fix whatever the repository holds, and whose
  command makes a change the gate passes.
- **When**: the developer runs `kiln monitor run`.
- **Then**: main holds the change.

### 6. A fix that can't pass stops, and waits for something to change

- **Given**: a monitor whose check joins the gate, whose command leaves the check still asking for a fix, and that
  names no agent.
- **When**: the developer runs `kiln monitor run` three times: once, again with nothing changed, and again once a
  commit lands on main.
- **Then**: after the first run, main is unchanged and `kiln status` shows the fix stopped. After the second, it shows
  the monitor held, and no fix ran. The third run starts a fix.

### 7. An agent asks for the developer once

- **Given**: a monitor whose check finds an error, and whose agent changes a file, then asks for the developer, with a
  report.
- **When**: the developer runs `kiln monitor run` three times: once, again once a commit lands on main, and again once
  its check prints a different finding.
- **Then**: after the first run, main is unchanged and `kiln status` shows the monitor needs the developer, with the
  report's summary. After the second, it shows the monitor held, and no agent ran. The third run starts the agent.

### 8. A session that fails

- **Given**: a monitor with only an agent, whose session fails.
- **When**: the developer runs `kiln monitor run`, and runs it again.
- **Then**: after the first run, `kiln status` shows the monitor's session failed, with its error, and the second run
  starts the agent again.

### 9. A fix that waits for accept

- **Given**: a monitor whose fixes wait for `kiln accept`, and whose command makes a change that passes.
- **When**: the developer runs `kiln monitor run`, and then `kiln accept` with the monitor's name.
- **Then**: after the run, main is unchanged and `kiln status` shows the fix waiting on the developer. After the
  accept, main holds the change.

### 10. A fix writes the tests only when its monitor says it may

- **Given**: a monitor that doesn't say its fixes may write the tests, whose command changes a file under the
  acceptance path; and a second monitor that says they may, and whose command does the same.
- **When**: the developer runs `kiln monitor run`.
- **Then**: the first monitor's fix stops and its change is not on main, and main holds the second monitor's change.

### 11. A monitor waits for a running build

- **Given**: a monitor that is due, while `kiln run` builds a spec.
- **When**: the developer runs `kiln monitor run`, and then runs it again once the build has ended.
- **Then**: the first run runs no check, and the second runs the monitor's check.

## Technical Plan

### kiln.json

- `kiln.json` gains `monitors`, each with `name`, `every` (a Go duration in whole hours, at least `1h`), and
  `command`, `agent` or both. `check` is optional. `agent` is a path under `.kiln/agents/`, read as `kiln queue`
  reads an agent's file, and `prompt` is text that follows the check's and the gate's output in the session's first
  message. The gate's output is there only when a command ran. `gates`, true by default, says whether the check
  joins the gate. `accept`, false by default, says whether fixes wait for `kiln accept`. `tests`, false by default,
  says whether fixes may write under the acceptance path with `{spec}` cut off, as `config.fixedPart` gives it.
  `accept` and `tests` are independent: each says one thing.
- `parseConfig` in `internal/kiln/config.go` refuses a monitor with no name, a name another monitor has, an interval
  under an hour or not in whole hours, no command and no agent, or a name a spec's name couldn't be, by `specName`'s
  rule. It refuses a monitor without `"tests": true` when `acceptance` has no fixed part before `{spec}`, since kiln
  could then guard no tests. It reads only `kiln.json`'s bytes, so it can't see the specs. `kiln queue` refuses a spec
  named as a monitor is, and `kiln monitor run` refuses a monitor named as a spec the book holds a spec's record
  for, so one name is never both.

### A monitor's run

- `kiln monitor run` takes the repository's run lock, `run.lock`, which `Repo.lockRun` at
  `internal/kiln/build.go:78` takes for `kiln run`. A `kiln run` or a second `kiln monitor run` that finds it held
  refuses, as `kiln run` refuses today, and exits non-zero. `errRunning`'s text becomes "another kiln builds in this
  repository". It holds the lock through each due monitor, one at a time. It takes the shared `build.lock` as every
  kiln that drives a build session does, so `tools/install` refuses while it runs, as `HoldBuilds` at
  `internal/kiln/kiln.go:229` refuses for a `kiln run`. `HoldBuilds`' doc names the kilns that drive a session, and
  gains `kiln monitor run`.
- `kiln accept <name>` lands a waiting fix itself when no kiln holds the run lock. `Repo.landAccepted` at
  `internal/kiln/kiln.go:694` starts a run for that name, and `book.next` takes a fix's record only then: when it is
  an accepted landing, and the run was started for its name.
- `kiln accept <name>` while a kiln holds the lock queues the accepted landing, and prints who lands it. A fix's is
  landed by the next `kiln monitor run`, whichever kiln holds the lock, since `kiln run` skips a fix's record. A
  spec's is landed by the running `kiln run`, as today. While `kiln monitor run` holds the lock, a spec's is landed
  by the next `kiln run` or `kiln monitor run`, since a `kiln monitor run` lands queued landings only as it starts.
  `book.next`'s doc, `take`'s comment at `internal/kiln/build.go:91`, the Accept doc and `landAccepted`'s doc say
  today's rule, and change with it.
- Due: a monitor is due when it never ran, when its last run ended `couldn't run` or `usage limit`, or when its
  last run started at least `every` less 30 minutes ago. The slack takes in launchd's hourly tick, which lands a few
  seconds either side of the hour, and a run held up by an earlier monitor's long fix.
- The clock: the time a monitor's run starts at, which its monitor line holds and the due rule compares, is the
  machine's clock, unless `KILN_NOW` names a time, as RFC 3339. The acceptance tests set it, as they set `KILN_OPEN`.
  Tests 6, 7 and 8 set it an interval later before each later run, so the monitor is due again. In tests 6 and 7,
  only the hold then keeps its fix from starting.
- A check runs with `sh -c`, outside the sandbox, in a worktree of its own at main's tip, as `readGauge` in
  `internal/kiln/gauge.go` runs a gauge's command and reads its last line. Its last line of stdout is a JSON object:
  `fix`, a boolean, and `summary`, a string. A last line that isn't one, or a non-zero exit, is a check that couldn't
  run. kiln reads the object, what goes to the agent and the hold's hash from stdout alone, so a tool's progress on
  stderr releases no hold. Why a check couldn't run is the last line of stdout and stderr together, since a failing
  tool writes its error to stderr, and it is what `kiln status` shows. Keep a check's script under `.kiln/monitors/`,
  where a fix can't change it.
- A fix builds on `kiln/<name>`, in `Repo.tree(<name>)`, as a spec's build does, since one name is never both. The
  command runs with `sh -c` outside the sandbox. The agent's session runs inside it, with `kiln.json`'s hosts,
  writable paths and MCP servers. Its contract is a new one, `internal/kiln/contract/monitor.md`: the build's contract
  without amendments, saying whether the fix may write the acceptance tests, and with how to ask for the developer. A
  fix runs no verify session, since it has no spec.
- The guard: a fix's session and gate take `Side{Role: buildRole}` with the acceptance path's fixed part, unless its
  monitor says `tests`. `Side.refusal` at `internal/kiln/guard.go:40` already refuses that path and
  `.kiln/specs/<name>.amend.md` for a split build's build session, and its reason gains a fix's wording. With
  `tests`, `guarded` still refuses the amendment file, since a fix has no spec to amend. `within` at
  `internal/kiln/guard.go:58` reads a side that ends in no slash as one file, so the Python preset's fixed part,
  `tests/acceptance/test_`, would guard nothing: for a fix, such a part guards each path that starts with it. The
  hook and `Repo.refused` in `internal/kiln/gate.go` ask the same guard, against the merge base, so a
  command's write the hook never saw fails the gate, and a commit on main under the acceptance path is not the fix's.
- After the command, and after each of the agent's turns, kiln commits what was left and gates it. When `gates` is
  true, the gate runs the check too, and reads its last line as the run's first check was read: it fails while the
  check says `"fix": true`, whatever the check exits with. `runCheck` at `internal/kiln/gate.go:59` passes a command
  that exits 0, so the check is no gate command. A check that couldn't run there ends the fix `couldn't run`.
- An agent asks for the developer with the last line of its last reply, a JSON object: `ask`, true, and `summary`, a
  string. The rest of the reply is its report. kiln deletes the fix's branch and worktree. A session that
  `claude.Ended.Limit` says a usage limit ended ends `usage limit`: kiln deletes the fix's branch and worktree, and
  never pauses as `build.pause` at `internal/kiln/build.go:545` does. Any other session that `claude.Ended.Failed`
  reports failed ends `session failed`, with the session's error. A fix that changed nothing on main and asked for
  no one, a command's or a session's, ends `unchanged`.

### The ledger and the book

- The ledger, `.git/kiln/ledger.jsonl`, gains a monitor line per run: its name, when it started, main's tip, a hash
  of its entry in `kiln.json` and one of what its check printed, and what the run found: `healthy`, `couldn't run`,
  `held`, or the build that the run started. A fix's build writes a build's lines, under the monitor's name, and its
  outcome is the build's outcome: landed, stopped, waiting, or, new, `needs you`, `session failed`, `usage limit`,
  `couldn't run`, `unchanged` and `rejected`. An accepted fix's landing writes `landed`, not the `accepted` that
  `onMain` at `internal/kiln/build.go:392` writes for a spec. `kiln reject` of a fix writes `rejected`, not the
  `dropped` at `internal/kiln/kiln.go:453`, which a fix cut short keeps. So the hold tells a landing from a reject,
  and a reject from a fix cut short.
  `kiln status` reads a fix's result from its build's outcome line. The hold compares a run with the last monitor
  line that started a fix, and a `held` line never starts one.
- The book holds a fix's record as it holds a build's: building, stopped or waiting on its branch, under the
  monitor's name, with a field naming it a monitor's. `kiln run` skips such a record, and `book.next` takes one
  only as an accepted landing, for a run started for its name. `kiln monitor run` lands a fix's queued accepted
  landing as `runsFor`'s Accepted rule lands a spec's. `book.requeue` queues again a fix's accepted landing that was
  left building, as it does a spec's, and leaves any other fix's record. `kiln monitor run` drops a building fix
  whose kiln is gone and that the developer hadn't accepted. `book.may`'s table at `internal/kiln/state.go:112`
  gains a fix's rows: `accept` lands a waiting fix and retires nothing, `reject` drops it, and `queue` refuses. A
  landed fix leaves no record, since a monitor has no UAT. The package comment's state diagram gains them, and its
  paragraphs on `kiln.json`'s keys, what the gate refuses and who notifies gain a monitor's.
- `nextStep` in `internal/kiln/stats.go:344` gains a fix's next steps: none for a landed fix,
  `kiln monitor run <name>` for a stopped fix or a failed session, and `kiln accept <name>` or `kiln reject <name>`
  for a waiting one. `TestNextStep` in `internal/kiln/rules_test.go` holds `kiln accept` and `kiln reject` against
  `book.may`. Its pattern at `internal/kiln/rules_test.go:527` finds no `kiln monitor run <name>`, which `book.may`
  has no command for, so a row of the test names that step's text.

### What the developer sees

- `kiln status` prints a `monitors` section, one line per monitor: its name, its result, when it last ran, when it
  runs next, then its summary, the report's summary, or why it couldn't run. A monitor that never ran shows
  `never run`. A held monitor shows what holds it: the result of the fix that holds it, with that fix's summary or
  its report's summary, so a monitor that needs the developer still says so an hour later.
- `kiln tui` lists the monitors beside the specs, each with the same fields, and opens a fix's build as it opens a
  spec's. `Specs`, `gone` and `Text` in `internal/kiln/view.go` read each build in the ledger as a spec's: `gone` at
  `internal/kiln/view.go:132` would show a landed fix as a rejected spec, and `Text` would read a spec file a fix
  doesn't have. They leave a fix's builds out of the specs. `tui.Source` at `internal/tui/tui.go:39` gains the
  monitors, and the fake in `internal/tui/tui_test.go` follows.
- The notification: `kiln monitor run` runs
  `open -gna Ghostty --args --working-directory=<the repository's top> -e <kiln's own path> monitor show <name>`,
  with kiln's own path as the hook at `internal/kiln/kiln.go:280` names it. LaunchServices passes on neither the
  launchd job's working directory nor its PATH. `kiln monitor show` writes the OSC 9 escape that
  `internal/kiln/build.go:140` writes, then the result, its summary, report or error, and the command to run next,
  and waits for return. On 2026-11-13, the developer saw such a window's notification, and a click on it brought
  Ghostty forward. The opener is `open` unless `KILN_OPEN` names another command, which the acceptance tests set, so
  no test opens a window. An opener that fails changes no result: kiln prints its error and goes on. UAT checks the
  notification.
- `kiln monitor install` writes `~/Library/LaunchAgents/dev.kiln.monitor.<hash of the repository's path>.plist`, with
  `StartCalendarInterval` naming the minute of the hour it was installed at, which runs `kiln monitor run` in the
  repository with the PATH it was installed from, and loads it with `launchctl bootstrap`. launchd runs such a job
  on wake when its minute passed during sleep. It doesn't for `StartInterval`, by `man launchd.plist`. Installed
  again, it replaces the job. No acceptance test runs it, since it changes the developer's launchd. UAT checks it.
- An unattended session needs a long-lived sign-in. The CVE job's log warned that its sign-in ends between
  2026-11-18 and 2026-11-21, and named `claude setup-token` for unattended jobs. The README says so.
- kiln's own monitors: the build names, in its last reply, the dependency monitor's entry for `kiln.json` and its
  check script for `.kiln/monitors/deps`, for the developer to commit, since a build writes nothing under `.kiln/`.

### Files the plan changes beyond internal/kiln

- `README.md`: Use, `.kiln/`, and "Where things are", which today calls `internal/kiln` specs only.
- `cmd/kiln/main.go`: its doc, `usage` and `commands` gain `kiln monitor install|run|show`. `kiln complete` names
  monitors to `monitor run`, and waiting fixes to `accept` and `reject`. `cmd/kiln/_kiln` follows.
- `internal/tui/tui.go`'s package doc, whose home lists specs only. `internal/kiln/ledger.go`'s doc, which lists each
  kind of line.
- `CLAUDE.md`: "Where things are" names `.kiln/monitors/`. `kiln tui`'s monitors are checked through the model's
  `Update` and `View` in a test of `internal/tui`, so no acceptance test drives it in a pseudo-terminal.
- `docs/later.md`: "Repairs" goes, since a monitor's agent is one. "A project's own workflow" keeps only a workflow
  of several roles, and says a monitor's agent is a role of a project's own.

## Alternatives Considered

### The starter's monitor: a store of observations, and a failing test as the only way in

- **Pros**: no judge weighs findings in English. Each fix ties to a Given, When, Then the developer approved. kiln
  counts each signature, so it can tell a new error from a known one.
- **Cons**: no dependency bump, vulnerability or drifted doc breaks a Given, When, Then, so none of the developer's
  monitors could reach kiln. kiln would own a store whose signatures only the project's tools understand.
- **Reason for Rejection**: a check that reads logs, such as one that asks Datadog for a service's errors, is a
  monitor of running software in this spec's shape. Its agent triages: it writes a failing test and fixes the error,
  or asks for the developer. The hold keeps a known finding from asking twice, and a check that wants counts keeps
  its own.

### A check's exit code says whether to fix

- **Pros**: any tool works as a check unwrapped.
- **Cons**: tools disagree. govulncheck v1.9.0 exits 3 on a finding and 1 on an error (`internal/scan/errors.go`), so
  one rule would read a dead network as a finding. An exit code carries no summary for the report.
- **Reason for Rejection**: the developer chose a check that prints what it found.

### Kinds of monitor

- **Pros**: each kind could check its own configuration, such as a command monitor with no agent.
- **Cons**: the dependency monitor wants a command and an agent both, the agent fixing what the bump broke.
- **Reason for Rejection**: one shape with optional steps covers each of the developer's monitors.

### A list of paths each monitor denies

- **Pros**: a monitor could deny any path, such as the harness.
- **Cons**: naming a list replaces the default, so `"deny": ["docs/"]` would quietly let a fix write the tests, and
  `[]` would be how a monitor allowed them. The developer would have to know the default to read an entry.
  `docs/later.md` "Who may write what" defers a finer table, and no monitor yet needs another path. Denying the
  harness by default would stop kiln's dependency monitor whenever a bump broke `internal/termtest`.
- **Reason for Rejection**: one yes-or-no, `tests`, says the one thing a monitor sets, and reads the same alone.

### A notification through osascript

- **Pros**: needs no terminal app, as the CVE job's script did.
- **Cons**: the developer didn't care for it, and a click on it brings forward nothing kiln wrote.
- **Reason for Rejection**: the developer asked for Ghostty's notification, which a click on brings forward the
  window with the summary.

### Keep the launchd job, and have it file a spec

- **Pros**: kiln changes nothing. The job writes a spec, and the developer queues it.
- **Cons**: the developer approves each spec, so each finding still waits on the developer, as the CVE job's 13
  waiting tasks did.
- **Reason for Rejection**: the developer wants the fix, not the alert.
