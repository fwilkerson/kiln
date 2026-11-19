# Spec 15: spec builds and monitor fixes run side by side

Status: Approved, 2026-11-19

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

One kiln works in a repository at a time. `kiln fire` and `kiln monitor run` each take the run lock, and each
refuses while the other holds it. A monitor may hold it for hours: one that drains a queue runs fix after fix. A spec
the developer queues meanwhile waits until that run ends.

```text
today    kiln monitor run ── fix ── fix ── fix ── fix ──▶          kiln fire: refused, another kiln builds
with     kiln monitor run ── fix ── fix ── fix ── fix ──▶          the monitors' lane
         kiln fire ───────── build x ───────── build y ──▶         the specs' lane
                                  each lands on main in its turn
```

After this spec, the repository has two lanes. A lane is the work that one kiln at a time may do. The specs' lane
holds each spec's build, and the monitors' lane holds each monitor's fix. `kiln fire` works in the first and
`kiln monitor run` in the second, at the same time. Two kilns in one lane still refuse.

The sessions run side by side, and a build's time goes there. What judges the work, and what moves main, does not.
Across both lanes, kiln runs one of these at a time: a gate, a landing, a monitor's check, or a read of the gauges.
A kiln that finds another at such work waits for it, and says so.

What showed the need: on 2026-11-17 the developer filed 31 bugs, and the bugs monitor drained them one fix at a
time, for hours. No `kiln fire` could start until it ended.

Most of what two lanes need is there. Each build and each fix works in a checkout of its own. A landing already
expects main to move under it: it rebases, and gates again. When the rebase conflicts, the conflict goes back to
the session that made the work, which merges main, resolves it, and is gated again. A fix that a monitor's
`command` made alone has no session, and stops on a conflict, as today.

## Goals

- `kiln fire` builds specs while `kiln monitor run` runs monitors, and each lands on main in its turn.
- A second `kiln fire`, or a second `kiln monitor run`, still refuses while the first runs.
- The next kiln of a lane recovers what a dead kiln left in that lane. No kiln touches what the other lane builds.
- `kiln accept` lands a spec's amendments while a monitor runs, and lands a fix while a spec builds.
- No gate runs beside another gate, a monitor's check or a gauge.
- No landing is sent back to rebase because the other lane landed while its gate ran.
- `kiln status` and `kiln tui` show both lanes' work as they show one today.

## Non-goals

- Two spec builds at once, or two fixes at once.
- A new agent that resolves a conflict. The session that made the work resolves it today.
- A share of the usage limit for each lane. Both draw on one account, as they do today in turn. Nothing shows the
  need yet: the ledger holds no build and no fix that a usage limit ended.
- A `kiln monitor run` that lands a fix the developer accepted while it runs. It lands queued landings only as it
  starts, as today.
- A landing's conflict that costs no round. A conflict with what the other lane landed goes back to the session as
  a round, as each conflict does today.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

None remain.

## Acceptance tests

<!-- A build writes test N's code, as .kiln/agents/verify.md says. A Then names a state the test can read through
the program, as its user reaches it. kiln refuses to queue a spec whose sections leave this file's order, or whose
tests are not numbered from 1, each with a Given, a When and a Then. Name only what the intent names: never a
timeout, a length or a message's wording. -->

### 1. A spec builds and lands while a monitor's fix is at work

- **Given**: `kiln monitor run` at work on a fix, whose agent's session has not ended its turn. A spec is queued.
- **When**: the developer runs `kiln fire`.
- **Then**: the spec's build lands on main while the fix's session is still at work. Once that session ends its
  turn, the fix lands, and main holds both.

### 2. A monitor runs while a spec builds

- **Given**: `kiln fire` at work on a spec, whose build session has not ended its turn. A monitor is due.
- **When**: `kiln monitor run` runs.
- **Then**: the monitor's check runs, and its fix lands on main, while the build session is still at work. After that
  run ends, `kiln status` still shows the spec building. Once the session ends its turn, the build lands, and main
  holds both.

### 3. kiln status shows both lanes at work, and a second kiln in a lane refuses

- **Given**: `kiln fire` at work on a spec, and `kiln monitor run` at work on a fix.
- **When**: the developer runs `kiln status`, then `kiln fire` again, and `kiln monitor run` again.
- **Then**: `kiln status` shows the spec building, and the monitor's fix building. `kiln fire` and
  `kiln monitor run` each refuse. Neither starts a session, and no check runs.

### 4. A fix cut short is recovered while a spec builds

- **Given**: a fix whose `kiln monitor run` was interrupted while its session worked. `kiln fire` is at work on a
  spec.
- **When**: `kiln monitor run` runs again.
- **Then**: it drops the fix cut short, and runs the monitor's check again. The spec's build goes on in the session
  it had, and lands.

### 5. A build whose kiln stopped is taken up while a monitor's fix is at work

- **Given**: a spec whose `kiln fire` was interrupted while its build session worked. `kiln monitor run` is at work
  on a fix.
- **When**: the developer runs `kiln fire`.
- **Then**: the build goes on in the session it had, and lands. The fix is not dropped: once its session ends its
  turn, the fix lands.

### 6. A spec's amendments land while a monitor's fix is at work

- **Given**: a spec waiting on its branch with amendments. `kiln monitor run` is at work on a fix.
- **When**: the developer runs `kiln accept` with the spec's name.
- **Then**: the spec lands on main, and is retired, while the fix's session is still at work.

### 7. An accepted fix lands while a spec builds

- **Given**: a monitor's fix waiting on its branch for `kiln accept`. `kiln fire` is at work on a spec.
- **When**: the developer runs `kiln accept` with the monitor's name.
- **Then**: the fix lands on main while the build session is still at work.

### 8. One gate runs at a time

- **Given**: a spec's build and a monitor's fix, each ready for its gate.
- **When**: both reach the gate.
- **Then**: the second gate starts only once the first has ended. The kiln that waits says that it waits.

### 9. A landing waits for the other lane's landing

- **Given**: a spec's build whose landing gate runs, and a monitor's fix ready to land.
- **When**: the fix reaches its landing.
- **Then**: the fix lands after the build, on a main that holds the build's work. Each ran its landing gate once.

## Technical Plan

<!-- Name the commands, packages and files, and the rules each holds. Name the code to copy, and from where. Name
each output form a Then reads, such as a line a command prints: a split build's tests are written from it alone. -->

The change is in `internal/kiln`, with one piece each in `internal/git` and `internal/tui`. No command, flag or key
of `kiln.json` is new, and `kiln init` writes nothing new.

### The lanes

kiln's book is `state.json`, which holds a record for each spec and each fix kiln has yet to end. A record belongs
to one lane, by its `Monitor` field: a spec's record to the specs' lane, and a fix's to the monitors' lane. Each
lane has a lock, in `<git common dir>/kiln/`, and the two replace `run.lock`:

| Lock            | Held by the one kiln that                                                    |
|-----------------|------------------------------------------------------------------------------|
| `specs.lock`    | builds specs: `kiln fire`, or a `kiln accept` that lands a spec's amendments |
| `monitors.lock` | runs monitors: `kiln monitor run`, or a `kiln accept` that lands a fix       |

- A lane's lock holds the name of the command that took it, as `run.lock` does today.
- Three changes to a record need its lane. They are marking it building, queueing again one that a dead kiln left
  building, and dropping a fix cut short. The code in `state.go` that makes each takes the lane as a parameter, so
  no caller makes one without naming the lane it holds.
- `kiln queue`, `kiln accept` and `kiln reject` change a record with no lane, as they do with no run lock today.
  The book refuses each on a record that is building.
- `kiln fire` takes `specs.lock`, and `kiln monitor run` takes `monitors.lock`. Each refuses at once while another
  kiln holds its lane, as today. Neither reads the other lane's lock.
- `kiln accept` takes the lane of the record it lands. For a spec waiting on its branch with amendments, that is
  the specs' lane, whatever the monitors' lane does. For a fix, it is the monitors' lane, whatever `kiln fire`
  does. A `kiln accept` that retires a landed spec takes no lane, as it takes no run lock today.
- A `kiln accept` that finds its lane held leaves the landing queued, and replies as today. For a spec, the reply
  names the `kiln fire` that is building, or the `kiln accept` that is landing. For a fix, it names the next
  `kiln monitor run`. The reply "the next kiln fire or kiln monitor run" goes, with the case that printed it.
- `kiln monitor run` lands only a fix's queued landing as it starts. Today it lands a spec's too.
- A spec's queued landing belongs to the specs' lane. The kiln at work there lands it. Should that kiln stop
  first, the next `kiln fire` lands it. The book refuses a second `kiln accept` of a queued landing, so
  `kiln fire` is the one command the developer has. `kiln status` says both, as under What the developer sees.

### Recovery, by lane

Today the kiln that takes the run lock reads every record marked building as a dead kiln's. With two lanes, a
record of the other lane may be a live kiln's. So each kiln recovers its own lane alone, as it starts:

| What it recovers         | The specs' lane                      | The monitors' lane                             |
|--------------------------|--------------------------------------|------------------------------------------------|
| lost runs, in the ledger | ends each lost run of a spec's build | ends each lost run of a fix                    |
| records left building    | queues each spec's again             | queues each fix's accepted landing again       |
| a fix left building      | never                                | `kiln monitor run` drops each not yet accepted |

- A lost run is a start line of the ledger with no end line. The ledger's build line names a fix by its `monitor`
  field, so the recount in `ledger.go` tells a run's lane from the ledger alone.
- A kiln in the monitors' lane leaves every spec's record as it stands, and a kiln in the specs' lane every fix's.
- No process ID decides. The lane's lock says that no other kiln works in the lane, as the run lock says it of
  the repository today.
- `kiln status` and `kiln tui` still read a record's process to show a build halted, as today.

### An accepted fix's landing

Spec 14 sends a setback of an accepted fix's landing back to the fix's session. It composes with the lanes:

- `kiln accept <monitor>` holds the monitors' lane through the whole landing, with each round of the fix's
  session. So `kiln monitor run` refuses meanwhile, as today, and `kiln fire` does not.
- Each landing, this one too, goes through `build.land` in `land.go`, where the lock of the next section is taken.
  A landing that meets a setback frees that lock before the session runs. The merge of main that kiln makes before
  a failed gate goes back is git's work on the fix's branch, and takes no lock.
- A fix that waits for `kiln accept` is now passed by each spec that lands meanwhile. Spec 14's path lands it.
- The tests in `uat/resolve/` stay true. None holds the run lock, or runs beside a `kiln fire`.

### One judging command at a time

One lock, `commands.lock`, beside the lanes' locks, is held while kiln runs what judges the work, or moves main:

| The kiln holds it through                                                   | Today                                  |
|-----------------------------------------------------------------------------|----------------------------------------|
| a gate of a build's or a fix's work, with the check or gauge it reads       | no lock, but `main.lock` at main alone |
| one try of a landing: reading main's tip, the rebase, the gate, moving main | no lock, but `main.lock` at main alone |
| a monitor's check, or its gauge, at main's tip                              | the run lock                           |
| the read of the gauges after a landing                                      | no lock                                |

- A try of a landing holds the lock from reading main's tip until main has moved, or the try answers a setback. So
  the other lane never moves main under a landing's gate. Main still moves under one when the developer commits,
  and the landing then tries again, as today.
- Each landing takes it: a build's, a fix's, and the one by which `kiln accept` retires a landed spec.
- A monitor's check holds the lock from before `kiln monitor run` reads main's tip for it, in `monitor_run.go`.
  So the check runs at the main its line in the ledger names, and its fix starts from. A check that read main's
  tip and then waited would run at a main the other lane had moved.
- What makes the work holds no lock: a session, an audit, and a monitor's `command`, which makes a fix in the fix's
  own worktree as a session does. A pause for a usage limit holds none. These are where the time goes.
- `kiln precommit`, which the pre-commit hook runs in the developer's checkout, takes no lock, as today.
- A kiln that finds the lock held prints one line, then waits:
  `<name>: waits while another kiln runs the project's commands`. `<name>` is the spec's, or the monitor's.
- The wait ends when kiln is interrupted. A blocking `flock`, as `store.lock` in `state.go` takes `main.lock`
  today, ignores the interrupt. So kiln tries the lock without blocking, and waits on its context between tries.
- A second `flock` of one file by one process waits on the first. So a landing takes the lock once, and the gate
  inside it takes none.
- `main.lock` goes. The gate's run at main happens only inside a gate, which now holds `commands.lock`.
- The scratch checkouts stay as they are. `.main` and `.gauges` are used only under `commands.lock`.
  `.monitor-check` is used only by the kiln that holds the monitors' lane.
- A kiln takes a lane first, then `commands.lock`, then the book's `lock`. No kiln takes a lane while it holds
  `commands.lock`.
- The wait is no part of the gate. A gate's line in the ledger starts once the kiln holds the lock, and a build's
  time counts no wait.

### Two kilns in one repository's git

`internal/git` keeps its calls that add or remove a worktree one at a time, by a mutex in `git.go`. Each such call
writes the repository's shared config, and git fails a write that finds the config locked. A mutex holds for one
process, and two lanes are two. So those calls also hold a file lock, `<git common dir>/kiln/worktrees.lock`.
A build whose git call failed so would stop, and its next step, `kiln queue`, builds it again from main.

### What the developer sees

Each form here is one a Then reads.

- A refused `kiln fire` or `kiln monitor run` exits non-zero, and its error names the command that holds its
  lane: `kiln fire`, `kiln monitor run` or `kiln accept`.
- `kiln fire` prints `<name>: landed on main at <commit>, and waits for UAT` when a build lands, as today.
- `kiln status` shows a spec at work as `<name>: building`. Under `monitors:`, it shows a monitor whose fix is at
  work as `<name>: fix building, ` then when it last ran, and one whose fix landed as `<name>: landed, `.
- `kiln status` shows a spec whose accepted landing is queued as `<name>: queued, landing accepted amendments`,
  as today. Under it, a new line says who lands it: `  the kiln at work on specs lands it, or the next kiln fire`.
  The line is the same whichever kiln holds the lane. `kiln status` takes no lane's lock to find out, since a
  `kiln fire` that started just then would find the lane held, and refuse.
- `kiln monitor run` prints `<name>: dropped its fix, which a kiln cut short` for each fix it drops, as today.
- `git log main` shows each landing, a build's and a fix's, as one commit.
- A landing that main moved under prints `main moved while the gate ran; the work is rebased onto it again`. In
  test 9 no kiln prints it.
- `kiln tui` lists each spec and each monitor from the book and the ledger, which hold each lane's records and
  runs apart already. Its pane for the session at work shows one run today: the first it finds running, among the
  specs. It shows one for each lane instead, the spec's above the fix's, each in the rows one takes today. So a
  fix's session that sits idle shows there too. A test in `internal/tui` checks it through the model's `Update`
  and `View`, as `CLAUDE.md` says of the monitors.

### How a test holds a kiln at work

The harness, `internal/scenariotest`, needs no change.

- A session stays at work while its scripted step waits for a file, as the build session of
  `uat/testdata/monitors/waits-for-build.txtar` does. The script runs that kiln with `&`, and waits for the file
  the step touches first.
- A gate stays at work the same way, as the gate of `uat/testdata/review/accept-while-accept-lands.txtar` does.
- The gate runs its commands at the work's commit in `<trees>/.gate-<name>`, and at main in `<trees>/.main`. A
  gate command tells where it runs by `basename "$PWD"`. kiln keeps main's result, and runs the commands at main
  again only once main or the commands change.
- A monitor's agent's session has the role of its agent file's base name. A fix that a monitor's `command` makes
  alone has no session and no audit, and still gates and lands.
- Both lanes' audits are attempts of the one role `audit`, numbered in the order they start. A script holds one
  lane until the other's audit has run, or gives each attempt the same steps.
- A script interrupts a kiln it started with `&<name>&` by testscript's `kill -INT <name>`. kiln then ends its
  session's process, and leaves its record building. `kiln status` shows such a spec as `<name>: halted`.
- The next `kiln fire` resumes a halted build's session. That is the session's next call,
  `.claude-calls/build/1/2/`, whose `call.json` holds `"resume": true`. A build that no kiln touched has no
  second call.
- A fix cut short is dropped, and the monitor's next fix is a new session: `.claude-calls/<role>/2/1/`.

### Tests and docs the plan changes

The build changes each test here only as this section says, and the audit holds it to that.

- `uat/testdata/monitors/waits-for-build.txtar` shows a monitor waiting for a running build, which is no longer
  true. Its first `kiln monitor run` now runs the check. So its first check, `! grep 'ran'`, becomes
  `grep -count=1 'ran'`, and its comments say what it now shows. Its last check stays.
- `uat/testdata/monitors/queued-landing-opens-a-tab.txtar` holds `.git/kiln/run.lock` to stand for a running
  kiln. It holds `.git/kiln/monitors.lock` instead. No check of it changes.
- `uat/testdata/review/accept-while-accept-lands.txtar` stays true: both specs are in the specs' lane.
- `TestNextFix`, in `rules_test.go`: its rows "a kiln accept of the fix" and "kiln monitor run, landing" expect
  `y`, a spec's accepted landing, to be taken too. Each then takes the fix `f` alone. `TestNext` there, and
  `TestStateOf` in `view_test.go`, name a lane where they take the next record.
- New rows in `rules_test.go`: recovery in one lane leaves the other lane's record building.
- `TestRecount` and `TestRecountAudit`, in `ledger_test.go`, recount the specs' lane. A new row: the recount of
  one lane ends no run of the other.
- Comments that name the run lock or `main.lock`: the gauges' read in `gauge.go`, the accepted landing in
  `kiln.go`, the take of the next record in `build.go`, and the usage limit's error in `fix.go`.
- The package doc of `internal/kiln`. Files lists the four new locks, and drops `run.lock` and `main.lock`. "One kiln
  monitor run" names its lane, and that it lands a fix's queued landing alone. Builds, The gate and Gauges each
  say what waits for `commands.lock`. A new paragraph says what a lane is, and what each kiln recovers.
- The docs of `Repo.Fire`, `Repo.MonitorRun` and `Repo.Accept`, which each say when the command refuses, and
  which kiln lands a queued landing.
- `docs/reference.md`, under monitors: "`kiln monitor run` refuses while `kiln fire` builds" and its reason. It
  refuses only while another `kiln monitor run` runs, or a `kiln accept` lands a fix.
- `docs/later.md`: a brief on each Non-goal that defers work, with what would show its need. They are two builds
  or two fixes at once, a share of the usage limit for each lane, and a conflict that costs no round.
- `README.md` does not change. `docs/practices.md` says it states no rule.

### Risks the plan accepts

The developer accepted each on 2026-11-19. How a project's own monitor or gauge accounts for a busy machine is the
project's to decide, and a flaky test is the repository's to fix. kiln's part is the one lock.

- A gate still runs beside the other lane's session, which may run the tests too. Then the gate is slower, and its
  time in the ledger grows. `.kiln/monitors/gate-time` may read that as slower tests, and ask for a fix. One of
  kiln's terminal scenarios may see its screen late, fail the gate, and cost a round. To make the first
  impossible, that check would count only a gate that no other build's session overlapped, by the ledger's start
  and end lines. That costs it gates to count while both lanes work. Nothing but one lane prevents the second.
- A test that fails at main only under the other lane's load is excused. The gate keeps main's result by main's
  commit, so each gate at that main then excuses the test, and work that breaks it can land. `kiln status` shows
  it as failing at main before the build. Today a flaky test at main does the same, and `docs/later.md` defers
  it under "Flaky tests in the gate". Keeping main's result only when each command passed would end the excuse
  at the next gate. That costs a second run at main for each gate while main truly fails. No rule here ends the
  first excuse.
- A work gate reads main's tip, which the other lane now moves. Say a test fails at main when a build starts, and
  the gate excuses it. Once a fix of it lands, the build's next gate no longer excuses it, and it costs a round.
  Gating the work against the main it was built on would make that impossible. That changes what the gate
  compares with, for every build, and its docs and scenarios.
- A slow check or gauge holds the other lane's gates, and `kiln accept`'s. Today the gauges' read takes no lock,
  so that no gate waits on a slow gauge. A lock of the gauges' own would keep that, at the cost named under
  Alternatives Considered.
- A monitor that lands often may spend a build's rounds, or an accepted fix's. Each landing in lines the work
  changed costs it a round, and work past `kiln.json`'s rounds stops. The ledger holds no round spent on a
  conflict yet. A count of conflicts apart from the rounds would make it impossible, at the cost of a second
  limit.
- Both lanes at work reach the account's usage limit sooner than either alone. A build then pauses until the limit
  resets, and a fix ends at once, as today. No rule prevents it but the share the Non-goals leave out.

## Alternatives Considered

### Two gates at once

- **Pros**: Neither lane ever waits for the other. No new lock.
- **Cons**: Two whole runs of the tests compete for the machine. kiln's terminal scenarios wait on a screen, and
  fail when it comes late. A project's gate may hold a port, or a database, that a second gate finds taken.
- **Reason for Rejection**: One gate at a time bounds the load on a gate to one session beside it. It costs the
  other lane a wait of one gate, and a build's time goes to its sessions.

### A lock for each gate alone, and none for the landing

- **Pros**: A shorter hold.
- **Cons**: A fix that lands between a build's landing gate and its move sends the build back to rebase and gate
  again. A monitor that drains could do so each time.
- **Reason for Rejection**: The landing's hold costs the other lane the time of one move, and removes the case.

### Either lane lands any accepted landing

- **Pros**: A fix accepted while `kiln monitor run` drains would land in the specs' lane, if that lane is free. A
  spec's queued landing would still land at the next `kiln monitor run`, as today.
- **Cons**: A record no longer belongs to one lane. Recovery must then tell a dead kiln's record from a live one's
  by its process ID, which the system may give to another process.
- **Reason for Rejection**: With one lane for each record, the lane's lock decides, and no state can say otherwise.

### A conflict with the other lane costs no round

- **Pros**: No monitor's landings can stop a build out of rounds.
- **Cons**: A session that never resolves a conflict needs a limit of its own, with its own test and its own line
  in each doc that names the rounds.
- **Reason for Rejection**: The developer chose the round on 2026-11-19. A merge of main is a turn of the session,
  as a failed gate's fix is. Nothing has yet shown a build stopped that way.

### A lock of the gauges' own

- **Pros**: No gate waits on a slow gauge, as today.
- **Cons**: A second lock, and a second rule. A gauge that runs beside a gate slows it.
- **Reason for Rejection**: One rule covers the gate, the check and the gauge. kiln's own gauge reads in seconds.

### A scratch checkout for each lane

- **Pros**: No lock for `.gauges`.
- **Cons**: `kiln accept` reads the gauges in no lane, and would need a third checkout.
- **Reason for Rejection**: `commands.lock` already keeps each shared checkout to one kiln.
