# Spec 17: One isolated workflow, side by side or in turn, for specs and monitors alike

Status: Approved, 2026-11-22

<!-- Drafted on 2026-11-22 from a review of 125 audit findings across kiln's builds and monitor fixes, and shaped
with the developer the same day. -->

## Overview

kiln has one isolated workflow: a verify session writes the tests, blind to the code, and a build session writes the
code. Today it exists twice, on separate code. A spec takes `build-and-verify`, which runs the two sessions at once,
and proves nothing about the tests. A monitor's fix takes `verify-first`, which runs them in turn, and proves that
the tests fail on main before the fix session starts.

After this spec they are one workflow, and its two names say only when the build session starts. Both are open to a
spec, in `.kiln/kiln.json`, and to a monitor's fix, in its `monitor.json`:

| `"workflow"`       | Sessions                                                                                 |
|--------------------|------------------------------------------------------------------------------------------|
| `build`            | one session writes the code and its tests (the default)                                  |
| `build-and-verify` | the verify and build sessions start at once, which saves time                            |
| `verify-first`     | the build session starts once the tests are proved, which saves a build no test asks for |

```text
"verify-first"       verify ──▶ proof ──▶ join ──▶ build ──▶ gate ──▶ audit ──▶ land

"build-and-verify"   verify ──▶ proof ──┐
                     build ─────────────┴▶ join ──▶ gate ──▶ audit ──▶ land
```

- **verify**: the verify session writes the tests in a clone of its own. A spec's writes its acceptance tests, from
  the spec alone. A fix's writes test files, from what the monitor's check printed.
- **proof**: kiln gates the verify session's commit, which holds no code of the build's. The gate must fail, and on
  tests alone. Otherwise its report goes back to the verify session, as a round.
- **join**: kiln merges the proved tests into the build session's branch. From there the build session reads them,
  runs them, and changes none.
- **run**: one start or resume of a session, until the session ends it. kiln commits, joins and gates only between
  runs.
- **gate**: a failure goes to the build session, as a round. A test it cannot meet goes back to the verify session.

What showed the need:

- The developer's reading of the two workflows, on 2026-11-22: they are one workflow that runs side by side or in
  turn. One saves time, and the other money, so one design should do both by a parameter.
- Spec 10 left out "a verify session's tests that must fail at main", and `NOTES.md` keeps it under "Limits on
  amendments". Its reason was a spec revised after UAT, whose landed work already passes its unchanged Thens. The
  audit review of 2026-11-22 showed the need. Of the bugs monitor's 64 findings, 13 were weak tests, some of which
  passed on main's code too. Specs drew 5 weak tests in 55 findings. The proof that landed for monitors on
  2026-11-21 is a gate that fails on tests alone. It lets an unchanged Then pass, so a revised spec still builds.
- odh's trial of 2026-11-14, which `git show 3bc35fd43673:.kiln/specs/stack-gate.md` records. The verify session
  ruled on a failing stack by reading the gate's report. It blamed working code in two rounds of three, and the
  build stopped.
- What the order costs, measured in kiln's ledger over the six `build-and-verify` builds from 2026-11-14 to
  2026-11-22. The verify session's first run took 3 to 12 minutes, in builds of 14 to 59 minutes. In turn, a build
  takes about that much longer.

## Goals

- `kiln.json` and `monitor.json` take the same workflows: `build`, `build-and-verify` and `verify-first`.
- In both isolated workflows, kiln proves that the verify session's tests fail on main before they join the build
  session's work.
- Under `verify-first`, no build session runs until the tests are proved.
- Under `build-and-verify`, both sessions start at once.
- The verify session never reads the build session's work. It alone changes a test, and a spec's alone amends the
  spec.
- The build session reads and runs the proved tests, and changes none.
- One loop runs a spec's build and a monitor's fix under each workflow: its sessions, gate, rounds and audit. Its
  order is one value. A spec and a fix each supply what differs between them, and how the run ends.

## Non-goals

- A workflow a project defines itself. `NOTES.md` keeps "A project's own workflow".
- A way for a fix's session to send a test back to its verify session. A fix whose tests are wrong asks for the
  developer, as today. What would show the need: a fix that asks because a proved test is wrong.
- A way to build a spec whose tests cannot fail on main. Such a spec asks for nothing that main lacks. A refactor
  changes no behavior, so it is no spec. Under either isolated workflow its build stops, as test 11 says. Under `build`
  kiln proves nothing, and it builds as
  today.
- A proof while the build session's first run still goes on. Side by side, kiln proves once both first runs have
  ended, so it acts only between runs. What would show the need: builds that wait on a verify session's round.
- A proof by a test that does not compile on main. New code is new behavior, and an acceptance test reaches
  behavior from outside the program, such as by its command line. So it compiles before the code exists. A test
  that calls new code directly never proves, and its project builds with `build`.
- Choosing the workflow for each spec.
- A proof of each acceptance test by its number. kiln reads the tests a gate names as failed, and no list of the
  tests that passed.

## Risks

Each is a way the work can go wrong, which this spec names and leaves open.

- **The build session reads the tests.** It may write code that meets a test's letter, and not the spec. The audit
  reads each test against the spec, as today.
- **One failing test proves the gate.** A test that passes on main rides along beside one that fails. The audit is
  told which tests failed at the proof, and reads the rest for a Then main did not already hold.
- **Replies are free text.** The build session's `cannot hold:` reply, and the audit's findings, may quote the code
  to the verify session.
- **Side by side, a build session may work for nothing.** Tests the verify session never proves stop the build,
  after the build session's first run.
- **Any failing test proves the gate.** A flaky test elsewhere, or an older test that a changed helper breaks, counts
  as proof though no new test fails. The audit is told which tests failed at the proof.
- **The build session may change a helper the proved tests call.** The audit reads each `testHelpers` file it
  changed. A project that lists those paths under `reaudit` has each later change read too.
- **The build rewrites fixtures of the scenarios that judge it.** The scenarios whose verify session's test calls
  `scratch.X()` need a test that compiles on main. Those fixtures decide which gate fails in each scenario.
- **`verify-first` is three fixes old.** The bugs monitor ran its first three on 2026-11-22, and one has landed.

## Open Questions

## Acceptance tests

A test that names no workflow holds under `build-and-verify` and under `verify-first`. Its code runs it under each.

### 1. In turn, the build session starts once the tests are proved

- **Given**: a project whose kiln.json says `"workflow": "verify-first"`, and a queued spec.
- **When**: `kiln fire` builds it.
- **Then**: the ledger shows a proof gate that failed before the build session's first run, and the spec waits for
  UAT with both sessions' files on main.

### 2. Side by side, the tests are proved before they join the build's work

- **Given**: a project whose kiln.json says `"workflow": "build-and-verify"`, and a queued spec.
- **When**: `kiln fire` builds it.
- **Then**: each session's first run starts before the other's ends, a proof gate failed before the first gate of
  both sessions' work, and the spec waits for UAT.

### 3. Tests that pass on main go back to the verify session

- **Given**: a queued spec, whose verify session first writes a test that passes on main's code.
- **When**: `kiln fire` builds it.
- **Then**: the verify session gets the proof gate's report as a round, and no gate of both sessions' work runs
  before a proof gate fails.

### 4. The build session works from the proved tests

- **Given**: a queued spec.
- **When**: `kiln fire` builds it.
- **Then**: under `verify-first`, the build session's checkout holds the verify session's tests at its first run,
  and its first message names their files and holds the proof gate's report. Under `build-and-verify`, its checkout
  holds them at its next run after the proof.

### 5. The build session cannot change a proved test

- **Given**: a spec whose build session changes a file under the spec's acceptance path, and the spec's amendment
  file.
- **When**: the session writes each, or a shell command does and the gate runs.
- **Then**: kiln refuses each write, the gate fails on each change, and main never holds either.

### 6. The verify session never reads the build's work

- **Given**: a spec whose audit sends findings, so the verify session works again once the build session has.
- **When**: `kiln fire` builds it, and the verify session lists the files and commits its checkout holds.
- **Then**: the list holds none of the build session's, and the verify session's settings deny it reads of the
  build's worktree and of the build session's transcripts.

### 7. A test the build session cannot meet goes to the verify session

- **Given**: a spec whose proved test expects what the spec does not say, and whose build session says the test
  cannot hold.
- **When**: `kiln fire` goes on.
- **Then**: the verify session gets the build session's reply, kiln proves its changed tests again, the build
  session's checkout holds them, and the work lands.

### 8. The audit's findings go to the verify session first

- **Given**: a spec whose audit sends findings.
- **When**: `kiln fire` goes on.
- **Then**: the verify session gets the findings before the build session does, and the build session's checkout
  holds the verify session's answer when its next run starts.

### 9. An amendment waits for the developer

- **Given**: a spec whose verify session amends one of its acceptance tests.
- **When**: `kiln fire` builds it, and the developer runs `kiln accept`.
- **Then**: the build waited on its branch, and the work lands with the amendment folded into the spec.

### 10. A revised spec proves what changed

- **Given**: a spec that waits for UAT, with its tests on main, and a revision on main that changes one Then.
- **When**: the developer queues it again, and `kiln fire` builds it.
- **Then**: the proof gate fails on the changed test, and the work lands.

### 11. A spec that main already meets stops

- **Given**: a queued spec, and main's code already does what each Then says.
- **When**: `kiln fire` builds it, and the verify session, sent back once, changes no test.
- **Then**: the build stops, and `kiln status` says why. Under `verify-first`, no build session has run.

### 12. A monitor's fix runs side by side

- **Given**: a monitor whose monitor.json says `"workflow": "build-and-verify"`, and a check that finds a problem.
- **When**: `kiln monitor run` fixes it.
- **Then**: the verify session's first run and the fix session's each start before the other's ends, a proof gate
  failed before the first gate of both sessions' work, and the fix lands.

## Technical Plan

### What a test reads

- `.git/kiln/ledger.jsonl`, and `kiln ledger --kind gate`: a proof gate's line holds `"proof":true`. The key
  replaces `before_fix`, for a spec's proof gate and a fix's alike. A proof gate that failed on tests alone holds
  `"passed":false`. Each run's line holds its `"role"`, `"verify"`, `"build"` or `"fix"`, with its `"at"` and
  `"end"`.
- `kiln fire` prints `<name>: landed on main`, and `  N of M rounds` for a build that used any.
- `kiln fire` and `kiln status` print `<name>: stopped: the verify session changed no test after a proof that did
  not stand`, for test 11.
- The verify session works in `<trees>/.verify-<name>`, on `kiln/<name>.verify`. The build session, or a fix's,
  works in `<trees>/<name>`, on `kiln/<name>`. The verify session's settings hold a `Read(...)` denial of
  `<trees>/<name>/**`, a `denyRead` of `<trees>`, and an `allowRead` of its clone alone. The build session's hold
  the same three for `<trees>/.verify-<name>` and its own worktree. `uat/testdata/build/split-sessions-apart.txtar`
  reads that form today, for the `.build-<name>` clone that goes.
- The build session says a test cannot hold as today: its reply ends with one line, `cannot hold: ` and the numbers
  of those tests.
- `.git/kiln/ledger.jsonl` holds each run's start line, with why the run started. A verify session's run after a
  proof that did not stand holds `"why":"no test"`.

### What a spec and a fix each supply

| The workflow needs        | a spec                                            | a monitor's fix                    |
|---------------------------|---------------------------------------------------|------------------------------------|
| the verify agent          | `.kiln/agents/verify.md`                          | the monitor's `verify.md`          |
| its first message         | write the spec's acceptance tests                 | what the check printed             |
| what it alone writes      | its acceptance paths, and its amendment file      | each test file, by `testFiles`     |
| what both write           | the paths of `testHelpers`, which the audit reads | nothing                            |
| the build session's agent | `.kiln/agents/build.md`                           | the monitor's `agent.md`           |
| its first message         | build the spec                                    | what the check printed             |
| a session ends the work   | never                                             | by asking for the developer        |
| a test that cannot hold   | goes to the verify session                        | the fix session asks the developer |
| the audit's findings      | the verify session first, then the build session  | the same, until `kiln accept`      |

### The loop

Two agents designed the loop apart, and a judge kept this one. Its names may change in the build. Its rules may not.

```go
// job is what a build works for: a spec, or a monitor's fix. Each supplies the rows of the table above.
type job interface {
	supply(rec record) supply                             // its sessions, from the files main held at rec.Approved
	heard(s seat, ended claude.Ended) reply               // how a run ended: over, "cannot hold:", a commit's title
	passed(ctx, b *build, head git.Rev) (setback, error)  // its end: it lands or waits, or says what kept it back
	settle(ctx, b *build, err error) error                // records how the build ended
}

func (b *build) work(ctx) error {                      // the one loop, for a spec and a fix, under each workflow
	for {
		proved := b.prove(ctx)                         // commit the verify session's work, prove it, join it
		b.tell(ctx, b.due()...)                        // run each session kiln owes a message, at once, and wait
		if !proved || b.owesAny() { continue }         // a session answers before any gate of the joined work
		if done := b.judge(ctx); done { return nil }   // gate, audit, then the job's own end
	}
}

func (b *build) due() []seat // the sessions that run now; the one place that reads the order
```

A seat is one of a build's two sessions: the one that writes the code, or the verify session. For each seat, the
build's record in `state.json` keeps the session's ID, its rounds, and each message kiln owes it, oldest first. A
first message, a round, a `cannot hold:` reply, the audit's findings and `kiln tend`'s note are each such a message.
The loop reads only that record and git. So a first start, a killed kiln and `kiln tend` all enter `work` the same
way.

The rules the compiler does not check, and what holds each:

- Two functions alone write what kiln owes a seat: one adds a message, and one removes the message a run just told.
  An arch test fails on any other write.
- kiln removes a message in the one write that follows its run's end, with what the reply asked. `build.set` in
  `build.go` runs its change again on the build's copy when the store fails, so a change that adds a message must
  hold when run twice.
- No session runs while kiln commits, proves, joins or gates. An arch test holds one call site for each.
- `due` alone reads the order. A table test of it names each order, with what kiln owes and whether a proof stands.
- A spec's guard carries the proved commit, as a fix's does today in `guard.tested`. A session's hook, each gate of
  its work and the landing ask that one guard, as `treeGuard` gives it today.
- The audit's findings are a message kiln owes, written with the commit the audit read. Written apart, as today, a
  kill between the two lands the work with its findings lost, since the loop reads only the record.
- What a join brought is no message. kiln works it out at each run of the build session, from the proved commit
  that session was last told of. So a kill after the merge loses nothing.
- kiln proves and joins only while it owes the verify session nothing. So a run that a kill cut short is told again
  before kiln judges its half-done work.
- Once a proof stands, the build session runs only while kiln owes the verify session nothing. So it answers the
  audit after the verify session's answer is proved and joined.
- Whether the work is joined is read from git: kiln joins while the proved commit is no ancestor of `kiln/<name>`.
  So a kill between the record's write and the merge joins again.
- `TestKilledAtEachStep` checks that each message kiln owed was told, in each flow. This build adds no row to its
  `defects`.

What stays as today: a fix the developer accepted lands as `landFix` does, with one gate, and a failed landing gate
goes back with main merged in. A session cut short is told its message again, after a line that says kiln stopped.

### Rounds and conflicts

Each seat has kiln.json's rounds of its own, and `kiln fire` prints the most that one seat used. `kiln tend` puts
both at 0.

| What goes back                                  | To                 | A round |
|-------------------------------------------------|--------------------|---------|
| a proof that did not stand                      | the verify session | yes     |
| a failed gate, or work that changes nothing     | the build session  | yes     |
| a path the audit must read again                | the build session  | yes     |
| the audit's findings, or a `cannot hold:` reply | as the table says  | no      |
| a first message, or what a join brought         | its session        | no      |

| The merge                                  | A conflict in                          | Goes to                        |
|--------------------------------------------|----------------------------------------|--------------------------------|
| the join                                   | a `testHelpers` file                   | the build session, as a round  |
| main into the work, at a landing or a tend | a file the verify session alone writes | a spec: the verify session, as |
|                                            |                                        | a round. A fix: it stops       |
| main into the work, at a landing or a tend | any other file                         | the build session, as a round  |

Whenever kiln merges main into the work, it merges main into the verify session's clone too. So a join cannot
conflict in a file the verify session alone writes: the build session writes none, and both sides hold main's.

### The rules

- `internal/kiln/config.go`: `workflow` keeps its three names, and both files take each. The two sets of
  `workflows`, and the refusal "open to specs only", go. The order is one value, read from the workflow in one place.
- `internal/kiln/monitor.go`: a monitor that names either isolated workflow needs what `verify-first` needs today: a
  check, `verify.md`, `agent.md` and kiln.json's `testFiles`, with no command and no `writesTests`.
- The proof is a gate, at the verify session's commit, that fails on tests alone. A test that does not compile
  names no failed test, so it is no proof. The contracts in `internal/kiln/contract/` tell each verify session so. While
  it passes, or fails on more
  than tests, its report goes back to the verify session as a round. Past the last round the build stops.
- After a proof that did not stand, a run of the verify session that changes nothing stops the build at once. It
  changed nothing when it ends with the tree that proof's commit held, over every path the session may write. kiln
  decides that in the write that follows the run's end. The proof's message says so: where main already does what is
  asked, change nothing. Without that way out, a session told to make a test fail may invent one stricter than any
  Then.
- The join merges `kiln/<name>.verify` into `kiln/<name>`. In turn, the build session starts from the join. Side by
  side, it starts from main, and kiln joins once its first run has ended and the tests are proved. A build session
  whose run ends first waits for the proof. kiln never joins while a session runs.
- No gate of the build session's work runs before the join.
- Side by side, a revised spec's earlier tests are on main, and would mislead the build session's first run. kiln
  commits their removal on `kiln/<name>` before that run, as `removeOldTests` does today, and puts them back before
  the first join, as `putBack` does. In turn, neither runs: the build session starts from the join.
- The record keeps the proved commit, `Tested`, for a spec as for a fix. Each gate after the join, the landing's
  too, fails while a file the verify session alone writes differs from main's with that commit's change applied.
- After each later run of the verify session, kiln gates its commit as a proof again, and joins again. A commit
  that leaves the proved tests as they were needs no gate.
- The audit's prompt names the tests that failed at the proof. It names each `testHelpers` file that differs from
  main's with the proved commit's change applied, which is each one the build session changed.
- One loop replaces the four of `build.go`, `split.go`, `verifyfirst.go` and `fix.go`. What goes with them: `stack`,
  `gateBuild`, `gateStack`, the bugs line and its retries, and the `.build-<name>` clone.
- The build session's next message after a join opens with what the join brought: the tests' files, the proof's
  report, and each new amendment's wording. A join that needs no answer starts no run.
- `internal/kiln/contract/`: `build-split.md`, `verify.md`, `monitor.md` and `monitor-verify-first.md` say what
  each session does in the one workflow, under either order.
- `crash.Step` stays inside the functions that write. The flow named `split` in `internal/crash/flows_test.go`
  runs under both orders, and `TestKilledAtEachStep` drives each.

### What else changes

Each test below keeps its check, in the new form:

- `before_fix` becomes `proof`: `internal/kiln/ledger_print_test.go`, `internal/kiln/rules_test.go`,
  `uat/testdata/build/lands.txtar` and `uat/testdata/monitors/shipped-bugs.txtar`.
- The refusals of a workflow: `TestConfigWords` in `internal/kiln/config_test.go`, the `verify.md` rows of
  `internal/kiln/monitor_test.go`, and `internal/kiln/schema_test.go`, which names the two sets that go.
- `.build-<name>` and `kiln/<name>.build` become `<trees>/<name>` and `kiln/<name>`: `split-lands.txtar`,
  `split-stops.txtar` and `split-sessions-apart.txtar` in `uat/testdata/build/`, `uat/tend/testdata/at8.txtar`, and
  `internal/kiln/tend_test.go`.
- `split-sessions-apart.txtar` keeps both halves. The build session's list is its first run's, before any join.
- `split-revised-spec.txtar` keeps its Then, since kiln still removes the earlier tests before the first run.
- Each scenario under an isolated workflow whose verify session's test calls `scratch.X()` gets a test that compiles
  on main, such as one that runs a command. They are in `uat/testdata/build/`, `uat/tend/testdata/` and
  `internal/crash/testdata/`.
- `split-bug-to-build`, `split-build-side-fails` and `split-stack-to-verify` lose only the checks the work leaves
  false: the stack, the gate of the build side alone, and the bugs line. Each check that stays true stays, such as
  a failed gate that goes to the build session alone, and an audit that runs only after a gate passed. A scenario
  with no check left goes.

The docs that state the workflows:

- The package doc of `internal/kiln`, and `internal/kiln/help.txt`, which `TestHelp` holds to it.
- `README.md`, `docs/reference.md`, and `docs/one-page.txt` with `docs/one-page.html` rendered again.
- The defaults under `internal/kiln/defaults/`: the agents' files, `/shape`'s skill and template, and both schemas.
- The build names, in its last reply, what the developer changes under `.kiln/` once it lands: `kiln init`, for
  the defaults.

## Alternatives Considered

### Replace `build-and-verify` with `verify-first`

- **Pros**: one order, so less to describe and to test.
- **Cons**: every build waits for the verify session, 3 to 12 minutes in the six builds measured.
- **Reason for Rejection**: the developer, on 2026-11-22. The two are one workflow, and its order is a parameter:
  side by side saves time, and in turn saves money.

### Keep the two as separate workflows, each open to specs and monitors

- **Pros**: no change to either flow.
- **Cons**: the clones, the proof and the routing written twice, and a proof that one order lacks.
- **Reason for Rejection**: the same.

### Share the workflow's steps, and keep a loop for a spec and one for a fix

- **Pros**: a smaller change. Each loop keeps its own end in plain sight.
- **Cons**: both orders are wired into two loops, and each later change to the gate, the rounds or the audit is made
  twice.
- **Reason for Rejection**: the developer, on 2026-11-22. Since `fix.go` landed on 2026-11-14, 21 of its 45 commits
  also changed `build.go`, so the two already change as one piece.

### Keep the build session blind to the tests

- **Pros**: no build session can write code to a test's letter.
- **Cons**: kiln stacks the tests on code its writer never ran them on. The verify session rules on a failing stack
  by reading a report, and needs a command of its own to run them, which the `stack-gate` starter proposed.
- **Reason for Rejection**: the developer, on 2026-11-22, for odh's trial. The session that holds the code runs the
  tests. Side by side, its first run is still blind, since kiln joins only between runs.

### The loop's other design

It kept one step function between runs, and seven methods on the type a spec and a fix each supply. The judge scored
both by the guide's five measures:

| # | Weight | Measure                           | Kept | Other | Margin | Points   |
|---|-------:|-----------------------------------|-----:|------:|-------:|----------|
| 1 |      3 | Hops                              |   65 |    85 |      1 | kept, 3  |
| 2 |      2 | Shallow pass-throughs             |    4 |     4 |      0 | 0        |
| 3 |      2 | Near-duplicate code               |    5 |     7 |      1 | kept, 2  |
| 4 |      3 | Lines on rules no compiler checks |  227 |   180 |      1 | other, 3 |
| 5 |      1 | Lines of code                     |  197 |   207 |      2 | kept, 2  |

- **Pros**: fewer lines rest on a rule the compiler does not check.
- **Cons**: the order acts in two places that a reader must read together, and a spec carries stub methods.
- **Reason for Rejection**: 7 points to 3. The kept design takes from it what won measure 4: the loop calls
  `settle`, each session has a queue of messages and rounds of its own, `heard` answers values, the audit's read and
  its findings are one write, and a verify commit that changes no test needs no proof.

### Keep `verifySession: true` as a boolean, and `test-first` as a monitor's own key

- **Pros**: no rename.
- **Cons**: two keys for one choice.
- **Reason for Rejection**: the developer chose one `workflow` key on 2026-11-22.
