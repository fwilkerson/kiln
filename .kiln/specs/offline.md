# Spec 18: A monitor waits for a connection

Status: Approved, 2026-11-25

## Overview

A monitor's fix is a Claude Code session, and a session needs Claude's API. Today `kiln monitor run` starts each due
monitor whether or not the Mac can reach it. Without a connection the fix ends session failed. `kiln status` then
names `kiln tend`, kiln notifies the developer, and the monitor waits out its whole interval before it runs again.
A weekly monitor that came due on a train loses its week.

Afterward, kiln asks for a connection before it runs a due monitor. With none, it skips the monitor, runs nothing,
and notifies nothing. `kiln status` and `kiln tui` say the monitor was skipped, and when. A skipped monitor stays
due, so the next `kiln monitor run` runs it, whether launchd starts that run or the developer does.

The developer asked on 2026-11-25: "currently the monitors run and I may not have internet access, right now they
fail and require a tend. I would prefer if kiln first checked for connectivity. If there is none, it should just
skip the monitor." They added that it matters most for a monitor that runs once a day, or every few days.

What kiln's own ledger holds, through 2026-11-25: seven fixes ended session failed.

| The session's error                                  | Fixes | What the session had done                    |
|------------------------------------------------------|------:|----------------------------------------------|
| `Can't reach the API server`, from its first request |     2 | nothing: 1 turn, $0, each failed 2 h later   |
| `Can't reach the API server`, after it had worked    |     1 | 15 turns, $0.91, failed 7.6 h after it began |
| `Your computer went to sleep mid-response`           |     3 |                                              |
| `Request timed out`                                  |     1 | 32 turns, $0.95                              |

Each of the seven showed `next: run kiln tend <name>`, and its monitor was not due again until its interval passed.

## Goals

- A `kiln monitor run` with no name starts no check, gauge, command or session of a due monitor that fixes with
  an agent, while kiln has no connection to Claude's API. launchd's hourly run is such a run.
- A skip asks nothing of the developer: no notification, and no `kiln tend` step.
- A skipped monitor stays due. The next `kiln monitor run` that has a connection runs it, with no name and no flag.
- `kiln status` and `kiln tui` say which monitors were skipped, and when, beside where each stood before.
- A fix whose session fails once the connection is gone ends the same quiet way: nothing notified, no `kiln tend`
  step, and due again at the next run. The developer chose this on 2026-11-25, since one of the three fixes that
  could not reach the API had worked for hours first.

## Non-goals

- A session that fails because the Mac slept. Three of the seven failed that way. A Mac that wakes may have its
  connection back before kiln asks, so this spec's check does not name that cause. It comes back with a spec that
  keeps the Mac awake while a fix runs, or runs a fix again after a sleep.
- `kiln fire`. A spec's build runs while the developer watches, and none has failed for lack of a connection.
- A run that waits more than 15 s for a connection. kiln skips, and the next hourly run asks again.
- A check that needs a host of its own, such as the `deps` monitor's, which asks Go's module proxy. With a
  connection to Claude's API and none to that host, the check couldn't run, and the monitor is due again, as today.
- A monitor with an agent whose command alone would have fixed the problem, as the `deps` monitor's often does.
  kiln cannot tell before the gate whether the agent runs, so it skips the monitor.
- A session that reaches Claude through Bedrock or Vertex. kiln asks Anthropic's API, as the README's install says.

## Open Questions

None remain.

## Acceptance tests

### 1. kiln skips a due monitor while it has no connection

- **Given**: a monitor with an agent that is due, whose check asks for a fix. A notify program keeps each notice it
  receives. kiln has no connection to Claude's API.
- **When**: `kiln monitor run` runs.
- **Then**: kiln runs neither the monitor's check nor a session, and the notify program receives nothing.
  `kiln status` says the monitor was skipped with no connection, and that it runs at the next `kiln monitor run`.
  It names no `kiln tend` for the monitor.

### 2. A skipped monitor runs at the next run that has a connection

- **Given**: a daily monitor that `kiln monitor run` skipped a moment ago, with no connection. kiln has a connection
  again.
- **When**: the developer runs `kiln monitor run`, with no name.
- **Then**: that run runs the monitor's check, which the skipped run did not. `kiln status` shows what the check
  found, and no longer says the monitor was skipped.

### 3. A fix that loses its connection asks nothing of the developer

- **Given**: a monitor that is due, whose check asks for a fix. A notify program keeps each notice it receives.
  kiln has a connection when the run starts, and none once the fix's session fails.
- **When**: `kiln monitor run` runs.
- **Then**: the notify program receives nothing. `kiln status` says the monitor's fix ended with no connection, and
  that the monitor runs at the next `kiln monitor run`. It names no `kiln tend` for the monitor.

### 4. A monitor whose fix lost its connection runs again at the next run

- **Given**: a daily monitor whose fix ended with no connection a moment ago. kiln has a connection again.
- **When**: the developer runs `kiln monitor run`, with no name.
- **Then**: that run runs the monitor's check again, and starts a new fix.

## Technical Plan

All of it is in `internal/kiln`, but for the view's part in `internal/tui` and the stand-in in
`internal/scenariotest`.

### The connection

- One func on `Repo`, in a new file `internal/kiln/online.go`, answers whether Claude's API replies, and the host
  it asked, for kiln's words.
- It sends one `GET`, with no sign-in, to the URL that `$ANTHROPIC_BASE_URL` names in kiln's environment. It sends
  it to that URL as given, with its scheme and path. With the variable unset, the URL is
  `https://api.anthropic.com`. A session reads the same variable, which `proc.Session` keeps, so kiln asks the host
  the session would.
- Any HTTP reply is a connection, whatever its status. On 2026-11-25, `https://api.anthropic.com` answered such a
  request 404. An expired sign-in is a session's failure, as today.
- An error is no reply: no route, no DNS, a refused or reset connection, or a certificate that fails. At the
  default URL, a captive portal fails the certificate, so it is no reply too.
- kiln asks again each second, until a reply comes or 15 s have passed since it first asked. Only then has it no
  connection. launchd starts a run its minute missed as soon as the Mac wakes, before Wi-Fi is back. A session that
  a sleep ended fails at that moment too. The 15 s keep kiln from reading either as no connection.
- The second and the 15 s are constants. The package doc states each, with a row in `TestFacts` of
  `internal/kiln/facts_test.go`.

### The run

`monitorRunning.pass`, in `internal/kiln/monitor_run.go`, runs one monitor once. Today it asks whether the monitor
is due, then whether a check that ran past the timeout still holds it, then runs its check or gauge. kiln asks for
a connection between the last two:

```text
due?      not due: kiln says when it is, and goes on             as today
stuck?    its check or gauge ran past the timeout, and nothing   as today
          changed since: held
online?   no connection: skipped                                 new
check     the check or gauge runs, and a finding starts a fix    as today
```

- kiln asks only before a pass that runs because the monitor is due. `kiln monitor run <name>` runs a monitor due
  or not, so it never asks. Neither does any other pass that runs due or not. A name already overrides each reason
  kiln has not to run a monitor, and the developer who names one is at the keyboard.
- kiln asks only for a monitor that holds an `agent.md`. No other monitor starts a session: one with a command
  alone fixes with no model, and one with a gauge alone only notifies. Each runs with no connection, as today.
- kiln asks once for each such monitor, so a run with nothing due asks nothing.
- A skip writes a monitor line to the ledger. `kiln ledger --kind monitor` prints it as any monitor line:

```text
name      the monitor's name
at        when kiln asked
main      main's tip
found     skipped
summary   no connection to <host>
```

- It prints, as each monitor line is said: `<name>: skipped: no connection to <host>`.
- A skip starts no check, gauge, command or session. It drops no stopped fix, and notifies nothing.

### A skip changes no standing

A skip is a run that did not happen, so it is no result of the monitor's.

- `histories`, in `internal/kiln/hold.go`, reads each monitor's history from the ledger: its last line, its last
  finding, and the line that holds it stuck. A skipped line changes none of the three. The history keeps only the
  last skipped line that no other monitor line follows.
- So each rule that reads a history reads as if the skip were absent: when the monitor is due, what holds it, and
  whether a session failed the same way twice. The monitor was due when kiln skipped it, and stays due.
- `standing`, in `internal/kiln/monitor_show.go`, is where a monitor stands, as `kiln status` and `kiln tui` show
  it. It gains the skip's time, which `kiln status` puts between the last run and the next:

```text
deps: landed, last ran 2026-11-21 01:19, skipped 2026-11-25 09:03 with no connection, runs at the next kiln monitor run
  fix(deps): land the monitor's fix
fresh: never ran, skipped 2026-11-25 09:03 with no connection, runs at the next kiln monitor run
```

- Each other line of the monitor is as today. A monitor that stood stopped keeps its `next: run kiln tend` line,
  since its stopped fix still waits. A skip adds no step, and `kiln tend` takes or refuses the monitor as it stood.
- `kiln monitor show` is unchanged, since no notice opens it for a skip.
- `tui.Monitor`, in `internal/tui/tui.go`, gains the skip's time, zero when the monitor was not skipped. The view
  in `internal/kiln/view.go` sets it. With the monitor selected, the builds panel's title says it, between when the
  monitor last ran and when it runs next. The monitor's row on home keeps its state's word. A test of
  `internal/tui` checks the title through `View`, as `CLAUDE.md` says of the terminal UI's monitors.

### A fix that loses its connection

- `build.talk`, in `internal/kiln/build.go`, runs each session of a build or a fix: the fix's own, its verify
  session's, and its audit's. There a usage limit already ends a fix, by an error that `fixSettle`, in
  `internal/kiln/fix.go`, turns into the fix's outcome. A lost connection ends a fix the same way.
- When a run of any session of a fix ends failed, as `claude.Ended.Failed` says, kiln asks for a connection. With
  none, `talk` answers a new error. With one, the run is read as today, and a failed fix session ends the fix
  session failed.
- `fixSettle` turns that error into a new outcome, `no connection`. Its summary is the session's own error, as a
  fix that ended session failed keeps it. The fix ends as one a usage limit ended: its record is dropped, with its
  branch and worktree. Where two sessions of a fix end in one round, the error decides the outcome, as a usage
  limit's does.
- An accepted fix's landing never ends so. `landingStops`, in `fix.go`, already turns each error of such a landing
  into a stop. So the landing stops, with the error as why, and keeps the fix the developer accepted.
- `fixing.endedBy` is unchanged. It reads how a session ended, and asks nothing.
- After a fix that ended `no connection`, the monitor is due at each run, as after a usage limit. Nothing holds it,
  and nothing notifies. `kiln status` names no `kiln tend`, and `kiln tend` refuses the monitor, as it refuses each
  that waits on no repair.
- kiln notifies nothing of a session that failed the same way as the monitor's last run. That rule now reads the
  monitor's last result other than `no connection`, however many fixes ended so since. So an expired sign-in still
  notifies once, across a stretch with no connection.
- `kiln status` then reads:

```text
uat: no connection, last ran 2026-11-25 00:30, runs at the next kiln monitor run
  API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)
```

- `tui.State` gains a state for it, whose word is `no connection`, marked as a usage limit's is.

### Docs

- The package doc of `internal/kiln`, in `kiln.go`: "One kiln monitor run" gains the `online?` step. Monitors says
  what a connection is, the 15 s, which monitors kiln asks for, that a skipped monitor stays due, and that a name
  never asks. Its list of when a monitor is due gains the fix that ended `no connection`. The ledger's `monitor`
  kind lists `skipped` under `found`, and says a skip's `at` is when kiln asked. A fix's outcomes gain
  `no connection`. Notifications says a skip notifies nothing, and neither does that outcome.
- `internal/kiln/help.txt` is a copy of that doc, which `TestHelp` holds equal.
  `go test ./internal/kiln -run TestHelp -write` writes it again.
- `docs/reference.md`, under Monitors: the run's diagram gains the step after `due?`. The sentence that points to
  `kiln help monitors` names the skip. The rule itself stays in the package doc, as `docs/practices.md` says.
- `README.md` states no rule, and stays as it is.

### Tests

- `internal/scenariotest` gives each script a stand-in for Claude's API, so no script asks the network. Its
  package doc lists the variable and the file:

```text
ANTHROPIC_BASE_URL   a listener of the test process, at http://127.0.0.1:<port>/<the script's $WORK>
.tmp/offline         while this file exists under $WORK, the listener closes each request with no reply
```

- A script takes the connection away with `exec touch .tmp/offline`, and gives it back by removing the file. Each
  time kiln asks with the file there, the script waits out the 15 s. Tests 1 to 4 each wait so once, and no other
  script does.
- For test 3, the scripted fix session takes the connection away, then fails. Its `claude.json` holds a `tool`
  step that runs `touch "$WORK/.tmp/offline"`. Then come the two steps of
  `uat/testdata/monitors/session-fails.txtar`: a `result` with `"is_error": true`, and `"exit": 1`.
- A Go test built on `scenariotest.Repo` has the same variable in the environment that func answers. A Go test that
  opens a `Repo` with an environment of its own sets the variable itself. An `httptest.Server`'s URL is a
  connection, and the URL of one it closed is none. No Go test asks `api.anthropic.com`.
- Test 1 reads that the check did not run from a log the check appends to, as
  `uat/testdata/monitors/due-or-named.txtar` does. Tests 1 and 3 read the notices from the file a notify program
  appends `$KILN_NOTICE` to.
- Rules no acceptance test holds, since main already meets each, or no command line shows it. Each gets the
  cheapest test that fails with the rule broken:
    - a named run never asks, and runs with no connection
    - a monitor with no `agent.md` is never skipped
    - a skipped line changes no hold, and leaves a stuck monitor stuck
    - a fix whose audit session fails with no connection ends `no connection`
    - an accepted fix's landing whose session fails with no connection stops, and keeps its branch
    - a session that fails the same way before and after two fixes that ended `no connection` notifies once
    - a reply that comes within the 15 s, after asks that got none, is a connection
- `TestKilledAtEachStep`, in `internal/crash`: a skip is one write of the ledger, which its flows already hold. It
  gains no row.

## Alternatives Considered

### Skip only the fix, and still run each check

- **Pros**: A healthy monitor still records healthy with no connection. A skip then means a fix kiln could not
  start.
- **Cons**: A skipped monitor stays due, so its check or gauge runs again each hour without a connection. kiln's
  `inspect` gauge takes about 25 minutes a run.
- **Reason for Rejection**: The developer asked that kiln check first and skip the monitor. The gauge's cost decides
  it.

### Show skipped as the monitor's result

- **Pros**: One word in the monitor's line and its row in `kiln tui`, as `refused` shows.
- **Cons**: It hides where the monitor stood. A fix that waits for `kiln accept`, or stopped, would read skipped.
  `due`, `holds` and `stuckAt` each read the last line, so each would need a case for it.
- **Reason for Rejection**: A skip is no run. Kept beside the last result, it changes none of those rules.

### Ask for a connection in a named run too

- **Pros**: `kiln monitor run <name>` never ends session failed for lack of a connection.
- **Cons**: When kiln's check is wrong, such as behind a proxy kiln does not read, no command runs a monitor.
- **Reason for Rejection**: A name already overrides each reason kiln has not to run a monitor.

### Ask with a session, such as one turn of Haiku

- **Pros**: It asks what a fix needs: the API, and a sign-in that still holds.
- **Cons**: It spends the developer's usage for each due monitor, each hour. Claude Code tries a request again for
  a long time before it fails, as the two fixes that took 2 hours show. An expired sign-in would then read as no
  connection, and never notify.
- **Reason for Rejection**: A request with no sign-in costs nothing, and answers in under a second.

### Skip at the first request that gets no reply

- **Pros**: A run with no connection loses no 15 s for each monitor it skips.
- **Cons**: A Mac that just woke has no Wi-Fi for a few seconds, and launchd starts its missed run right then. A
  laptop that sleeps again within the hour would skip a weekly monitor at each wake.
- **Reason for Rejection**: The critic's second read traced that path. The 15 s cost nothing while a reply comes.

### Read the session's error in place of asking first

- **Pros**: No request, and no variable for the tests.
- **Cons**: The two fixes that never reached the API each took 2 hours to fail. The error's wording is Claude
  Code's, and may change.
- **Reason for Rejection**: The developer asked that kiln check first.
