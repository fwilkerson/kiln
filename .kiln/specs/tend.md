# Spec 16: Tend what stopped in a session of your own, then kiln goes on

Status: Approved, 2026-11-21

<!-- A starter, drafted on 2026-11-09 while slicing specs/notify.md, and widened to monitors on 2026-11-21. /shape
took it up on 2026-11-21, under the name talk. -->

## Overview

A build stops, or a monitor waits on the developer. The developer runs `kiln tend <name>`, and lands in a Claude
Code session that already holds what went wrong, and knows how kiln works. The session is the developer's own, in
their checkout. So it can change what no session kiln runs may: main, the spec, a monitor's own files, and the rest
of `.kiln/`. The developer has it make the repair, and exits. kiln then goes on from where it stopped. A stopped
build goes on in its own session, on its own branch. A stopped landing lands again. A monitor runs again. Where the
build's own session cannot finish the work, the developer's session finishes it on main, and kiln records it landed.

The developer set the rule on 2026-11-21: "it doesn't matter if it's a monitor or a build that runs into an issue,
we need to provide a way to help users resolve these issues and ensure the interactive session has the tools and
know how to help the developer correct what went wrong and get the kiln working again."

What showed the need, for builds: while slicing `specs/notify.md`, the developer asked to "hop in, answer a
question, and then exit / detach allowing kiln to finish". Today a stopped build prints the command that resumes its
session, but `kiln queue <name>` then drops its branch and builds anew from main.

What showed the need, for monitors: on 2026-11-20, kiln's `code-health` monitor asked for the developer on each run.
Its gauge listed a file whose finding the developer keeps, so its agent had no file to take. The agent's report named
the cause, but its session could not make the repair, since a fix never writes under `.kiln/`. The developer asked:
"when there's an issue I'd like to take it into a claude session, that claude session should get the context of what
the problem is and have the tools to resolve it."

What the ledgers of kiln, odh and odh-trial hold, through 2026-11-21:

| What waited on the developer                  | Times | What the developer ran                           |
|-----------------------------------------------|------:|--------------------------------------------------|
| A monitor's fix that ended needs you          |     5 | a repair by hand, then `kiln monitor run <name>` |
| A monitor's fix that ended session failed     |     3 | the same                                         |
| An accepted fix's landing that stopped        |     2 | `kiln accept <name>`, which landed both again    |
| A spec's build that stopped                   |     1 | `kiln queue <name>`, which built anew            |
| A monitor's fix that stopped, not yet landing |     0 |                                                  |

## Goals

- One command takes whatever waits on a repair: a stopped build, a stopped landing, and a monitor that needs the
  developer, whose session failed, whose fix stopped, whose check couldn't run, or that kiln refuses.
- Its session starts with the report: where the spec or monitor stands, why, and where its work, its sessions and
  its files are. The developer explains nothing twice.
- Its session has the tools. It is the developer's own, so it may change main, the spec, and `.kiln/`.
- Its session has the know-how. kiln tells it what each state means, which command moves each, and what kiln does
  once the session ends.
- A stopped build goes on in the same session, on the same branch, with what the developer decided. Today it is
  built anew.
- Work the build's session cannot finish, the developer's session finishes on main. The session tells kiln, with no
  confirmation from the developer, and kiln then shows the spec as it shows any landed build, in `kiln status` and
  `kiln tui`. The developer set this on 2026-11-21, as an edge case.
- A build that stopped on an error of kiln's goes on once the error's cause is gone. Today such a build is built
  anew, even one its gate and audit passed that stopped only because main's checkout was in the way of its landing.
- Once the session exits, kiln goes on at once, and asks the developer nothing.
- Each place kiln says a build or a monitor waits on a repair names the one command to run.

## Non-goals

- A build that stops to ask the developer a question. It needs a fourth waiting state, with its own rules.
- Attaching to a session while it builds. A session runs headless, as `claude -p`, and has no terminal to attach to.
- A build or a fix that waits for `kiln accept`. The developer reads it, then accepts or rejects it, as today.
- A spec that waits for UAT. `/shape` revises its spec, as today.
- A session that decides for the developer. It makes the repair they ask for, in their terminal.
- A file of the project's that tailors the session. The know-how is kiln's own, and the project's `CLAUDE.md`
  already reaches a session of the developer's.
- What a monitor's check printed, kept for the session. The ledger keeps its summary and a hash. The session runs
  the check itself.

## Open Questions

None remain.

## Acceptance tests

### 1. A monitor that needs the developer opens in a session that holds its report

- **Given**: a monitor whose fix asked for the developer, with a report.
- **When**: the developer runs `kiln tend` with the monitor's name.
- **Then**: an interactive Claude Code session starts in the developer's checkout. Its first message holds the
  monitor's name, where the monitor stands, the check's summary, the agent's report, and the path of the monitor's
  directory.

### 2. The session repairs the monitor's own files, and kiln runs the monitor again

- **Given**: a monitor that needs the developer, whose check finds nothing once the check's own script is changed.
- **When**: the developer runs `kiln tend` with the monitor's name. The session changes the script, commits it on
  main, and exits.
- **Then**: kiln runs the monitor again, and prints how the run ended. `kiln status` shows the monitor healthy.

### 3. A monitor kiln refuses opens with why

- **Given**: a monitor whose directory kiln refuses at main's tip.
- **When**: the developer runs `kiln tend` with the monitor's name.
- **Then**: the session's first message holds the monitor's name, and why kiln refuses it.

### 4. kiln tend refuses what waits on no repair

- **Given**: a monitor whose last run ended healthy, and a spec that waits for UAT.
- **When**: the developer runs `kiln tend` with each name.
- **Then**: kiln refuses each, and says where it stands. No session starts.

### 5. Each place that says a monitor needs the developer names kiln tend

- **Given**: a notify program that keeps each notice it receives, and a monitor whose fix asks for the developer.
- **When**: `kiln monitor run` ends that fix needs you.
- **Then**: the notice's next step names `kiln tend` with the monitor's name. So does the monitor's next step in
  `kiln status`, and in `kiln monitor show`.

### 6. A stopped build opens in a session that holds why it stopped

- **Given**: an approved spec whose build stopped, its gate still failing after its last round.
- **When**: the developer runs `kiln tend` with the spec's name.
- **Then**: an interactive Claude Code session starts in the developer's checkout. Its first message holds the
  spec's name, why the build stopped, the test that failed, the branch and the worktree that hold the work, and the
  build session's ID.

### 7. A stopped build goes on in its own session, with what the developer decided

- **Given**: an approved spec whose build stopped, its gate still failing after its last round, on a failure that a
  commit on main ends.
- **When**: the developer runs `kiln tend` with the spec's name. The session makes that commit on main, leaves a
  note for the build, and exits.
- **Then**: kiln starts no new build. It resumes the build's session, with a message that holds the note. The work
  lands on main, with the session's earlier work in it, and the spec waits for UAT.

### 8. A split build goes on in both its sessions

- **Given**: a project that chooses the split workflow, and a spec whose build stopped, the stack's gate still
  failing after its last round, on a failure that a commit on main ends.
- **When**: the developer runs `kiln tend` with the spec's name. The session makes that commit on main, and exits.
- **Then**: the session's first message names both of the build's sessions, each with its checkout. kiln starts neither
  session anew. The work of both lands on main, and the spec waits for UAT.

### 9. A build that stopped on an error of kiln's goes on

- **Given**: a build whose gate and audit passed, which stopped because a file of the developer's in main's checkout
  was in the way of its landing.
- **When**: the developer runs `kiln tend` with the spec's name. The session moves the file away, and exits.
- **Then**: kiln starts no new build. The work lands on main, and the spec waits for UAT.

### 10. A stopped landing lands again

- **Given**: a monitor's fix the developer accepted, whose landing stopped on a gate that fails at main.
- **When**: the developer runs `kiln tend` with the monitor's name. The session makes the gate pass on main, and
  exits.
- **Then**: the fix lands on main, as `kiln accept` would land it again.

### 11. A stopped build's summary and notice name kiln tend

- **Given**: a notify program that keeps each notice it receives, and an approved spec whose build stops.
- **When**: `kiln fire` ends.
- **Then**: the next step in the build's summary names `kiln tend` with the spec's name. So does the notice's.

### 12. The session finishes the build's work itself, and kiln records it landed

- **Given**: an approved spec whose build stopped.
- **When**: the developer runs `kiln tend` with the spec's name. The session commits the work and its acceptance
  tests on main, tells kiln the work landed, and exits.
- **Then**: kiln resumes no session of the build. `kiln status` shows the spec waiting for UAT, the build's branch and
  worktree are gone, and `kiln accept` retires the spec.

## Technical Plan

### The command

`kiln tend <name>` takes one name, a spec's or a monitor's. `kiln queue` already refuses a spec that shares a monitor's
name. `kiln tend <name> --landed` records a stopped build's work as landed, as below. `cmd/kiln/main.go` gains its line
in `usage`, and its entry in `onRepo`. `internal/kiln` gains `(*Repo).Tend`, which takes kiln's own standard input,
output and error, as `TUI` takes its files. kiln tend never asks whether they are a terminal: `claude` does.

One kiln tends a name at a time. `Tend` takes a lock named for the name, `tend-<name>.lock` beside kiln's other locks,
before it reads where the name stands, and holds it until it exits. A second `kiln tend` of that name refuses at once,
and says a kiln tend is at work on it. So no two sessions decide for one build.

### What it takes

The book is kiln's record of each spec and each fix, in `state.json`. `Tend` reads the book's record of the name, and
for a monitor its standing, as `kiln status` reads them. Its refusals are one list of guard cases, each with its own
message, of the kind `errRefused`. `may` in `internal/kiln/state.go` holds the table for the other commands, and gains
this column. Halted is what `kiln status` shows for a build or a fix whose kiln is gone mid-work: `kiln fire` or `kiln
monitor run` takes it up again.

| Where the name stands                                                       | kiln tend                       |
|-----------------------------------------------------------------------------|---------------------------------|
| no record, and main's tip holds no monitor of that name                     | refuses                         |
| a spec: queued, building or halted                                          | refuses, as `kiln accept` does  |
| a spec: waiting for UAT, or waiting on its branch with amendments           | refuses, with `nextStep`'s step |
| a spec: stopped, or landing accepted amendments, stopped                    | takes it                        |
| a monitor: its fix builds, waits on its branch, lands, or is halted         | refuses, with `fixStep`'s step  |
| a monitor: its fix stopped, or its accepted landing stopped                 | takes it                        |
| a monitor: needs you, session failed, couldn't run or refused, held or not  | takes it                        |
| a monitor: never ran, healthy, landed, unchanged, usage limit, or cut short | refuses: it waits on no repair  |

One name is never both a spec's and a monitor's. `kiln queue` refuses a spec whose name a monitor holds, and `kiln
monitor run` refuses a monitor whose name a spec's record holds. Should both exist, the book's record decides, and the
name is the spec's.

Needs you covers both kinds: a fix whose agent asked, and a monitor that fixes nothing whose gauge crossed. A refusal's
message starts `<name>: `, and holds where the name stands as `kiln status` words it.

### The session

`internal/claude` gains the one interactive launch, beside `Run`. Its package doc says so, and lists the command:

```text
claude --session-id <id> --append-system-prompt-file <dir>/tend.md <first message>
```

- It runs in the checkout kiln tend ran in, with kiln's standard input, output and error. `<dir>` is the session's
  directory under `sessions/`, as `Run` lays one out. It holds `tend.md`, and `note.md` once the session writes it.
- It passes no `-p`, `--settings`, `--setting-sources` or `--permission-mode`. The session is the developer's own: their
  settings, their permissions, no sandbox of kiln's and no hook of kiln's.
- It inherits kiln's environment whole. `proc.Session` withholds what would misdirect a session kiln drives, and kiln
  drives none here.
- kiln waits for the session to exit. Ctrl-C is how a developer leaves Claude Code, and the terminal sends it to kiln
  too, since both are in its foreground group. So the session starts without kiln's context, in kiln's own process
  group, never as `proc.OwnGroup` starts a child. `Tend` ignores an interrupt while the session runs. What follows the
  exit runs under a context `Tend` makes once the session has exited, which a later interrupt ends.
- A `claude` that is not on the PATH fails kiln tend with `proc.NotOnPath`, before anything else.

`tend.md` is the know-how, kiln's own text, embedded from `internal/kiln/contract/tend.md` beside the other contracts.
No file of the project changes it. It says:

- what kiln is, and that `kiln help specs`, `kiln help monitors` and `kiln help builds` print its rules;
- what each state the command takes means, and what usually causes it: a failing gate, a conflict with main, a wrong
  spec, a monitor's own script, an expired sign-in, a file in the way on main;
- where each repair goes: main for what is wrong on main or in `.kiln/`, `/shape` for a spec's Given, When, Then, and
  the note below for the build's own work;
- that the build's session finishes the build wherever it can, and that otherwise this session finishes the work on
  main, with the spec's acceptance tests, and then runs `kiln tend <name> --landed` itself. When kiln refuses that, the
  session makes the fix the refusal names, or tells the developer what it says;
- what kiln does once the session ends, and that the session never runs `kiln fire`, `kiln queue`, `kiln accept` or
  `kiln monitor run` itself unless the developer asks;
- that a build's worktree and sessions are the build's: the session reads them, and takes from the branch what it lands
  on main itself.

The first message is plain text. A test reads it from the scripted claude's record of the call, which keeps each call's
prompt. kiln.json's `"verifySession"` is true, so the verify session writes the tests from these forms alone:

```text
<kind> <name>: <state>
<why>
Its work: kiln/<name>, in <worktree>
Its sessions: build <id>, in <dir>; verify <id>, in <dir>
Its directory: .kiln/monitors/<name>/
A note for the build: <path>
```

- `<kind>` is `The spec` or `The monitor`. `<state>` is as `kiln status` words it, without `held: `.
- `<why>` for a spec is the record's Why, with the gate's output, and its `failing:` line as `sumUp` in
  `internal/kiln/stats.go` prints it. For a monitor it is `standing`'s summary and report, as `kiln monitor show` prints
  them, with no indent. A refused monitor's is kiln's refusal.
- `Its work` shows while the book holds a stopped record with a worktree. `Its sessions` shows each session the record
  holds: one for a build then audit, two for a split build, each with the checkout it runs in.
- `Its directory` shows for a monitor. `A note for the build` shows for a spec's stopped build alone.
- A line with nothing to hold is left out. What follows these lines, such as that the developer is there, is the build's
  to word.

The note is `note.md` in the tend session's directory. It is the one way the developer's decision reaches a stopped
build's sessions. A stopped landing and a monitor get none: each goes on by a command that takes no message.

### Once the session exits

`Tend` reads where the name stands again, since the session may have moved it, such as with `/shape` and `kiln queue`,
or with `--landed`. Where it no longer waits on a repair, kiln prints where it stands, and exits. Otherwise it goes on
at once, and asks nothing. It first prints a line that says so, worded as the build chooses, so the developer learns why
the terminal is busy. A session that exits non-zero fails kiln tend, and nothing goes on.

| What stopped                                 | Going on                                                          |
|----------------------------------------------|-------------------------------------------------------------------|
| a spec's build                               | queued, with its build ID, sessions and branch; then run in place |
| a landing of accepted amendments, or a fix's | what `kiln accept <name>` does with a stopped landing             |
| a monitor, with or without a stopped fix     | what `kiln monitor run <name>` does, which skips its hold         |

A spec's build that goes on:

- `book` gains the move, beside `accept`'s for a stopped landing, in `internal/kiln/state.go`. The record keeps its
  build ID, its sessions, what a split build's record holds of them, and the commit the audit read. Its rounds start
  again at 0, as a stopped landing's do. It is queued, so `goesOn` in `state.go` puts it first.
- That one update also writes the message kiln owes each of the build's sessions into the record, whole. The message
  holds why the build stopped, that the developer saw to it, the note's text if `note.md` holds any, and to go on and
  end its turn. The record gains a field of its own for it. `ForBuild` and `ForVerify` would not do: `passOn` in
  `internal/kiln/split.go` sends those as a bug, or as tests that can't hold. Only then does the update clear Why and
  Failing. So a kiln that dies next loses neither the reason nor the note.
- kiln runs it in place, as `landAccepted` in `internal/kiln/kiln.go` runs a landing: `run`, in the specs' lane, for
  that name, under a holder of its own, `kiln tend`, so a refusal names the right kiln.
- While another kiln holds the specs' lane, that kiln goes on with the build once its own work ends. `runsFor` in
  `state.go` gains the case: every run in the specs' lane takes a queued build that holds an owed message, as every run
  takes an accepted landing. Today a `kiln accept` that lands would skip it. A halted build holds none, so it still
  waits for `kiln fire`. kiln tend then prints `<name>: queued; <the kiln that holds the lane> goes on with it`, and
  exits 0.
- Before the gate, kiln merges main into the branch when main moved since the branch last held it, in a commit of
  kiln's. A split build merges it into each clone, as `mergeMain` in `internal/kiln/split.go` does. A conflict goes to
  the session, with the `conflicts` prompt of `internal/kiln/build.go`.
- Then kiln resumes each session the record holds once, headless, with the owed message, and clears it. Today `goOn` in
  `build.go` and `splitGoOn` in `split.go` resume a session whose kiln stopped with the `interrupted` prompt, "kiln
  stopped while you worked. Go on where you stopped." `splitGoOn` resumes only a session whose turn kiln cut short. An
  owed message goes to both of a split build's sessions, whichever turn had ended. A build with no session starts one,
  as a queued build does.
- From there it is a build: kiln commits what the session left, gates, audits what changed since the audit read it, and
  lands. Its ledger lines go on under the same build ID, and `kiln tui` shows them as one build.

A monitor goes on in the monitors' lane, which the hourly `kiln monitor run` may hold while a fix builds. `Tend` waits
for that lane, where `kiln monitor run <name>` would refuse. It prints one line that it waits, and tries the lock again
each 200 ms, as `lockCommands` in `internal/kiln/lane.go` waits for its lock. The session has ended by then, and the
repair is on main. A developer who interrupts the wait loses neither: kiln prints ` next: run kiln monitor run <name>`,
and exits.

`kiln monitor run` gains two rules, for every run. It skips a monitor while a kiln tends it, by `tend-<name>.lock`, and
says so. So no run drops or replaces a stopped fix the session reads. And a run whose check or gauge finds nothing to
fix drops its monitor's stopped fix that the developer has not accepted, with its branch and worktree. It drops it while
it holds the monitors' lane, as `dropLost` in `internal/kiln/monitor_run.go` drops a fix cut short. Today a healthy run
leaves that record and worktree until the monitor's next fix replaces them.

`kiln queue <name>` on a stopped build still builds anew from main, for a spec that changed.

### Work the session landed itself

`kiln tend <name> --landed` says the work of a spec's stopped build is on main, by commits kiln did not make. The tend
session runs it, in the developer's checkout, and so may the developer. It asks nothing, and takes no lock of `kiln
tend`'s. Its refusals are guard cases. Each message names its fix, so the session makes the fix, or tells the developer:

- the name is not a spec's stopped build, or is a landing of accepted amendments, which `kiln accept` lands;
- main changed no file under the spec's `"acceptanceTests"` paths since the commit the build was queued from, so nothing
  shows this build's work on main. A spec built before, and revised, holds tests from its earlier landing. The fix it
  names: commit the work and the spec's acceptance tests on main, and run it again. Or leave the work to the build,
  which goes on once the session ends.

Otherwise kiln drops the build's checkouts and branches, and records the work on main at main's tip, as `landedBefore`
and `onMain` in `internal/kiln/build.go` do for a landing a dead kiln left. The ledger's outcome is landed. It prints
`<name>: landed on main at <commit>, and waits for UAT`, as a build's landing does. kiln sends no notice, and reads no
gauge, as for any landing it did not make. No audit reads work landed this way: UAT is where the developer judges it.
`kiln accept <name>` gates main as it stands before it retires the spec, as today. The `kiln tend <name>` that waits on
the session then finds the spec waits on no repair, and says where it stands.

### The next step

`nextStep`, `fixStep`, `kilnFix`, `gateFix` and `resumeStep` in `internal/kiln/stats.go`, and `standing.step` and
`fixNext` in `internal/kiln/monitor_show.go`, name `kiln tend <name>` for each state the table above takes. Each such
line holds `run kiln tend <name>`. A stopped build's also names `kiln queue <name>`, which builds anew. A stopped
landing's also names `kiln accept <name>` and `kiln reject <name>`, as today. No next step prints `claude --resume` any
more. A refused monitor, and one that couldn't run, gain a next step. `kiln fire`'s summary, `kiln status`, `kiln
monitor show` and the notice's `next` read those steps. `kiln tui` shows no next step, and gains none.

The change makes checks of four folded scenarios false. The build session changes each, as `.kiln/agents/build.md` says
of a folded scenario the spec changes, and changes no other check in them. The audit reads each change:

| Scenario, under `uat/testdata/` | The check that goes                                 | What holds it now          |
|---------------------------------|-----------------------------------------------------|----------------------------|
| `build/stops.txtar`             | the step's `claude --resume`, and its stand-in run  | acceptance tests 6 and 11  |
| `build/split-stops.txtar`       | the step's two `claude --resume` lines              | acceptance test 8          |
| `build/hangs-stop.txtar`        | the step's wording, "which builds anew"             | the step names `kiln tend` |
| `monitors/gauge-watched.txtar`  | the step's wording, in `kiln status` and the notice | the step names `kiln tend` |

`uat/testdata/review/accept-stops-then-lands.txtar` still holds, since a stopped landing's step keeps both commands. In
`internal/kiln/rules_test.go`, the build rewrites the rows of `TestNextStep`, `TestFixStep` and `TestSumUpPlaces` that
expect `claude --resume`, or a step that ends in `kiln monitor run`.

### Docs

- The package doc of `internal/kiln`: `Tend` in the sample that opens it, the `stopped` rows of both diagrams under
  Specs, a section on `kiln tend`, which is the rule's home, the healthy run that drops a stopped fix under Monitors,
  `tend-<name>.lock` under Files, and the sample summary under Notifications. `TestHelp` holds `internal/kiln/help.txt`
  equal to that doc: `go test ./internal/kiln -run TestHelp -write` writes it again.
- `internal/kiln/state.go`: the table above `may`.
- `cmd/kiln/main.go`: its package doc, which repeats `usage`.
- `docs/reference.md`: the list under "Commands", which `TestCommands` holds equal to `usage`, the line on `kiln monitor
  show` that names the next command, and the files under `sessions/`.
- `README.md`: the `stopped` row of the table under "When a build needs you", and the Monitors section, within the size
  its test holds.
- `docs/one-page.txt`, where it names what the developer runs on a stopped build, rendered again as `CLAUDE.md` says.
- `/shape`'s default in `internal/kiln/defaults/`: "Revising a spec" says a stopped build is queued anew, which stays
  true, and names `kiln tend` beside it. `.kiln/` is out of a build's reach, so the build names kiln's own copy in its
  last reply, for the developer to bring up to date with `kiln init`.

### Tests

- `internal/kiln`: `may`'s table gains the column, in `rules_test.go`, and `runsFor`'s gains its case. One table over
  the first message: each row gives a ledger and a book, and reads the message. It covers session failed, a stopped fix,
  couldn't run, a watching monitor's needs you, and a stopped landing of amendments, which the acceptance tests leave
  out. One row each for `--landed`'s two refusals, and for a second `kiln tend` of a name. One test cancels the context
  `Tend` got while a stand-in session runs, and reads that the build still goes on. One reads that a split build whose
  turns had both ended sends the owed message to both sessions, and that `passOn` sends none of it.
- `internal/claude`: the interactive launch's arguments, directory and environment, beside `launch_test.go`'s.
- The scripted claude of `internal/scenariotest` speaks only as `claude -p` today, and reads its prompt on standard
  input. The acceptance tests need it to play an interactive call: one with no `-p`, whose prompt is its last argument.
  Its role is `tend`, by the file `--append-system-prompt-file` names, as for any call. It runs that role's steps from
  `claude.json`, each tool in the call's directory with no sandbox, and records the call under `.claude-calls/tend/`,
  prompt included. It prints no stream. A step writes the note at the path the first message names, and a step runs
  `kiln tend <name> --landed`. `internal/scenariotest` is a `testHelpers` path, so the verify session writes this.
- Acceptance test 7 reads the note in the build session's resumed call, from that call's record.

## Alternatives Considered

### Resume the stopped build's own session, with the developer at its keyboard

- **Pros**: the session holds all the build read and tried, and no note is needed.
- **Cons**: kiln holds that session to its worktree, by its sandbox, its hook and the gate. It cannot change main, the
  spec or `.kiln/`. Of Eidos's 28 parks with options, 24 were plumbing outside the change, as
  `git show d619d1f48596:docs/later.md` counts them. A split build has two such sessions, each blind to the other.
- **Reason for Rejection**: the developer asked for a session with the tools to correct what went wrong, whatever
  stopped. The build's session still does the build's work, once kiln goes on.

### Resume a monitor's fix session, in its worktree

- **Pros**: the session holds all the fix read and tried.
- **Cons**: its settings forbid any write under `.kiln/`, where most repairs are. A fix that ends needs you, session
  failed or couldn't run leaves no worktree and no record, as `fixEnded` in `internal/kiln/fix.go` shows.
- **Reason for Rejection**: the repair the developer asked for is one that session may not make.

### An agent's file of the project's for the session, in `.kiln/agents/`

- **Pros**: a project tailors the session, as it tailors the build, verify and audit agents.
- **Cons**: one more file `kiln init` writes and merges. What the session must know is kiln's, the same in every
  project, and the project's own rules reach a session of the developer's through `CLAUDE.md`.
- **Reason for Rejection**: no project has asked to tailor it.

### Ask before going on

- **Pros**: an exit by accident, or a look with no repair, spends nothing.
- **Cons**: one more question, which the developer expects to answer yes each time.
- **Reason for Rejection**: the developer dropped it on 2026-11-21: "less friction". A going on that was not wanted ends
  as the last did: a build stops again within its rounds, and a monitor's fix asks again.

### The developer confirms that the session landed the work

- **Pros**: a person says a spec is built before it waits for UAT.
- **Cons**: one more question, whose answer the session already knows. UAT is where the developer judges the work.
- **Reason for Rejection**: the developer chose the session's own command on 2026-11-21. This spec pushes toward
  autonomy, and a confirmation is busy work.

### Name the command `kiln talk`, or `kiln repair`

- **Pros**: `kiln repair` says the work plainly, and this spec calls the work a repair.
- **Cons**: `kiln talk` names the means, not what the developer is there to do. Neither sits with `kiln fire`.
- **Reason for Rejection**: the developer chose `kiln tend` on 2026-11-21, and wants thematic names before 1.0.

### Open the session from the notification's tab

- **Pros**: one step fewer, from the notification to the session.
- **Cons**: a session spends money before the developer has read why the monitor waits.
- **Reason for Rejection**: the tab's command is the project's own, in its `notify` program. A project that wants the
  session at once names `kiln tend` there.

### Keep what the check printed, for the first message

- **Pros**: the session starts with the check's whole output.
- **Cons**: the ledger keeps a hash of it, to release a hold. Keeping the text is a new record, of any size.
- **Reason for Rejection**: the session is on main, where it runs the check itself.
