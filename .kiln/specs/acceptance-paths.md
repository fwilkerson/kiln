# Spec 13: A list of acceptance paths

Status: Approved, 2026-11-16

## Overview

After this spec, a project keeps a spec's acceptance tests in more than one place. `kiln.json`'s `acceptance` takes a
list of paths, each with `{spec}` for the spec's name, such as `["uat/{spec}/", "integration/{spec}/"]`. It still
takes one path as a string. A split build's verify session writes under any of the spec's paths, and its build
session writes under none. Each thing kiln does with the one path today, it does with each path of the list.

What showed the need: odh is splitting its tests into two suites. A UAT runs the built program, as its user does. An
integration test boots odh in the test's own process, with a clock the test moves and outside systems the test makes
fail. The verify session writes each numbered test of a spec, of both kinds. Today odh names one path,
`uat/{spec}/`. So a split build's verify session cannot write a test that needs time to pass, and a build session or
a monitor's fix may write under `integration/` unguarded.

kiln cannot say which test goes in which path. The project's verify agent, `.kiln/agents/verify.md`, gives that rule.

## Goals

- `acceptance` takes a list of paths, or one path as a string. kiln reads `acceptance` at the commit a build was
  queued at, and an old commit holds a string. So a build queued before the project changed to a list builds as it
  was queued.
- Each place that uses the one path today uses each path of the list:
    - The guard: a split build's verify session writes under any of the spec's paths, and its build session under
      none.
    - A split build starts without what main holds under any of the spec's paths, and its build side's gate runs none
      of it.
    - A monitor's fix writes under no path's part before `{spec}`, unless its monitor says `"tests": true`.
    - kiln's contracts and the forbidden actions name each path.
    - `kiln queue` refuses a build agent's file with a line that names any path's part before `{spec}`, such as
      `integration/`. A line names the part where the part starts a path, so `internal/integration/` does not name
      it. That file says nothing of the acceptance tests. So it names a lasting suite in words, never by its folder.
- `kiln queue` refuses a `kiln.json` whose `acceptance` names no path, a path without `{spec}`, or a path that starts
  with `{spec}`.
- `kiln queue` refuses a list in which one path's part before `{spec}` starts another's, such as `uat/` and
  `uat/integration/`, or the same part twice. Then no two specs' paths are one path, or one inside the other.
- `kiln queue` refuses a spec whose name puts one of its paths on a harness path: the same path, inside it, or
  around it. kiln reads whatever main holds under a spec's paths as that spec's old tests. When a split build starts,
  kiln removes it from the build session's checkout, gates that session's work without it, and refuses the session's
  writes there. So a spec named `uattest`, beside the harness `uat/uattest/`, would build without the harness. Main
  keeps the folder: kiln puts it back when it stacks the verify session's work on the build session's.

## Non-goals

- Saying which test goes in which path. The project's verify agent says it.
- A harness, a gate or a round of its own for each path. The paths share `kiln.json`'s harness and gate.
- Checking that each test N exists once across the paths. The test's name is the project's, as its verify agent
  writes it. odh checks this in its own tests.
- Guarding a project's lasting suites, outside every `{spec}` path, from a split build's build session. Today only a
  sentence in the project's instructions keeps the session out of them, as kiln's `CLAUDE.md` does for
  `uat/testdata/`.
- Refusing a spec whose name lands on a folder that is no harness, such as kiln's own `uat/testdata/`. Decision 3
  says why.
- Folding an accepted spec's tests into a lasting suite. A fold stays a project's monitor, as kiln's own `uat`
  monitor is.

## Open Questions

None. Each decision below keeps the option it rejected.

## Decisions

### Decision 1: a list of paths, over a third directory

The developer chose the list on 2026-11-15, while planning odh's two suites. The other option kept one path, under a
third directory named `acceptance`, beside `uat/` and `integration/`.

Decided: `acceptance` takes a list. Each spec's tests of each kind sit beside the suite they fold into, and run on
that suite's harness.

### Decision 2: each path keeps `{spec}`

`{spec}` is how kiln tells one spec's tests from the rest. A path without it names the whole suite:

- When a split build starts, kiln removes what main holds under the path from the build session's checkout, as a
  revised spec's old tests, and gates that session's work without it. That would be the whole suite.
- The guard would give the verify session the whole suite, and bar the build session from all of it.
- Two specs' tests would share one folder, so their names collide. Two `TestAT1` in one Go package fail to build.

Decided: each path keeps `{spec}`, and `kiln queue` refuses a path without it. Today kiln takes such a path, and
refuses only one that starts with `{spec}`. A review said a list does not need this rule, since one path has the
same problem today. The developer kept it on 2026-11-16: a project edits `acceptance` to add a path, which is when
it would leave `{spec}` out.

### Decision 3: kiln refuses a name that lands on a harness, and no other

kiln reads whatever main holds under a spec's paths as that spec's old tests. A spec whose name lands on another
folder gets one wrong build. Its build session works, and is gated, without the folder. Its verify session may
rewrite all of it. Main loses nothing, since kiln puts the folder back at the stack. Today kiln refuses no name for
this.

The developer chose among three rules on 2026-11-16:

- Refuse no name, and keep today's behavior.
- Refuse a name whose path meets a harness path. `kiln.json` at the queued commit and the name decide it.
- Also refuse a name whose path main already holds, unless a landing of that spec put it there. kiln would tell
  that by the landing's commit on main, since its ledger dies with the clone.

Decided: the second. In odh each folder beside a `{spec}` folder is a harness, `uat/uattest/` and
`integration/integrationtest/`, so the rule covers each. kiln's own `uat/testdata/` is no harness, so a spec named
`testdata` still queues there. A review said a list does not need this rule either, and the developer kept it on
2026-11-16.

The rule reads `harness` and the spec's name alone, so it holds whatever the workflow.

### Decision 4: no path's part before `{spec}` starts another's

A list adds a way for two specs' paths to meet, which one path never had. Under `uat/{spec}/` and
`uat/integration/{spec}/`, a spec named `integration` has the path `uat/integration/`, which holds every other
spec's second path. Its build would start without those tests, and its verify session could rewrite them. Under
`test_{spec}.py` and `test_{spec}-it.py`, the specs `a` and `a-it` share the file `test_a-it.py`.

Decided: `kiln queue` refuses a list in which one path's part before `{spec}` starts another's, or equals it. Two
paths whose parts differ before either ends can never be one path, or one inside the other, whatever names fill
them. The rule costs a layout that puts `{spec}` before the kind, such as `tests/{spec}/uat/` and
`tests/{spec}/integration/`. No project has one.

kiln compares the parts as `kiln.json` writes them, and every other place compares clean paths. So `kiln queue` also
refuses a path that is not in its clean form, such as `./uat/{spec}/`, or one that starts with `/`.

## Acceptance tests

<!-- A build writes test N's code, as .kiln/agents/verify.md says. A Then names a state the test can read through
the program, as its user reaches it. kiln refuses to queue a spec whose sections leave this file's order, or whose
tests are not numbered from 1, each with a Given, a When and a Then. Name only what the intent names: never a
timeout, a length or a message's wording. -->

### 1. The verify session writes under each path, and the build session under none

- **Given**: a project in the split workflow, whose `kiln.json` names the acceptance paths `uat/{spec}/` and
  `integration/{spec}/`, and an approved spec `x`.
- **When**: the developer queues `x` and runs `kiln fire`. The build session writes a file under each path, and the
  verify session writes a test under each.
- **Then**: kiln refuses each of the build session's two writes, and `x` lands on main with the verify session's
  tests under `uat/x/` and `integration/x/`.

### 2. A build queued under one path builds with that path

- **Given**: a project in the split workflow, and a spec `x` queued while `kiln.json` names its acceptance path as the
  string `"uat/{spec}/"`. Main's `kiln.json` then changes to the list `["uat/{spec}/", "integration/{spec}/"]`.
- **When**: `kiln fire` builds `x`, and its build session writes a file under `uat/x/` and one under
  `integration/x/`.
- **Then**: kiln refuses the write under `uat/x/`, and `x` lands on main with the build session's file under
  `integration/x/`.

### 3. A revised spec's build starts without its old tests in every path

- **Given**: a project in the split workflow, whose `kiln.json` names the acceptance paths `uat/{spec}/` and
  `integration/{spec}/`. A spec landed, with its tests under both paths on main. The developer revised a Then and
  queued it again.
- **When**: `kiln fire` builds it.
- **Then**: the build session's checkout holds no file under either of the spec's paths, its gate runs none of the
  old tests, and the spec lands with the verify session's new tests under both paths.

### 4. kiln refuses a spec named for a harness folder

- **Given**: `kiln.json` names the acceptance paths `uat/{spec}/` and `integration/{spec}/` and the harness
  `uat/uattest/`, and the developer approved a spec named `uattest`.
- **When**: the developer queues it.
- **Then**: kiln refuses it, and `kiln status` does not show it queued.

### 5. kiln refuses a build agent that names any path

- **Given**: `kiln.json` names the acceptance paths `uat/{spec}/` and `integration/{spec}/`. The build agent's file
  names `integration/`, the second path's part before `{spec}`.
- **When**: the developer queues an approved spec.
- **Then**: kiln refuses it, and `kiln status` does not show it queued.

### 6. kiln refuses a path without `{spec}`

- **Given**: `kiln.json`'s `acceptance` lists `uat/{spec}/` and `integration/`.
- **When**: the developer queues an approved spec.
- **Then**: kiln refuses it, and `kiln status` does not show it queued.

### 7. A monitor's fix writes under no path

- **Given**: `kiln.json` names the acceptance paths `uat/{spec}/` and `integration/{spec}/`, and a monitor that does
  not say its fixes may write the tests. Its command changes a file under `integration/`.
- **When**: the developer runs `kiln monitor run`.
- **Then**: the monitor's fix stops, and its change is not on main.

### 8. Each session is told each path

- **Given**: a project in the split workflow, whose `kiln.json` names the acceptance paths `uat/{spec}/` and
  `integration/{spec}/`, and an approved spec `x`.
- **When**: `kiln fire` builds `x`.
- **Then**: the build session's and the verify session's instructions each name `uat/x/` and `integration/x/`.

### 9. kiln refuses a list whose paths could meet

- **Given**: `kiln.json`'s `acceptance` lists `uat/{spec}/` and `uat/integration/{spec}/`.
- **When**: the developer queues an approved spec.
- **Then**: kiln refuses it, and `kiln status` does not show it queued.

## Technical Plan

<!-- Name the commands, packages and files, and the rules each holds. Name the code to copy, and from where. Name
each output form a Then reads, such as a line a command prints: a split build's tests are written from it alone. -->

All the Go code is in `internal/kiln`. `audit.go` reads no acceptance path, and does not change.

### `acceptance` in `kiln.json`

`config.go`: `config.Acceptance` becomes a list of paths. A JSON string reads as a list of one, so both forms parse
wherever kiln reads `kiln.json`. `acceptancePath` fills `{spec}` into each path, and `fixedPart` answers each path's
part before `{spec}`.

A build takes `workflow`, `acceptance` and `harness` from the commit it was queued at, in `build.sides`. Its hooks,
its gates' guards, the paths its gates leave out and its landing all use those. A fix takes `acceptance` from
main's tip when it started, in `fixOf`. Only the gate's commands, its rounds and what a session reaches come from
main's tip at the time. So test 2's build never reads the list.

A list of one path behaves as the string does today, and each message and contract names it the same way. So each
folded scenario under `uat/testdata/`, which holds the string, passes unchanged.

### Each place that uses the path

- `guard.go`: `side.acceptance` becomes a list. `side.refusal` counts a path as a test when it is `within` any of
  them. `within` is today's func: the path itself or one under it, by whole path segments, with case folded as APFS
  folds it. A fix's side holds each part before `{spec}`, and refuses a path that starts with any. A build or
  verify side is valid with at least one path. The hook's command line holds `--acceptance` once for each path, as
  it holds `--harness`. A hook an older kiln wrote holds one, which reads as a list of one.
- `split.go`: `build.removeOldTests` removes each path main holds, in one commit. `build.gateBuild` gives the gate
  each path to leave out, through `build.gated`. `build.putBack` puts back what the build side removed under any
  path.
- `gate.go`: `Repo.gate`, `Repo.refused`, `Repo.removed` and `Repo.atMain` take the paths to leave out as a list.
  `atMain` keeps the checks it ran at main's tip, and runs them again once that list changes.
- `agent.go`: `sides.acceptance` becomes a list. `sides.contract` names each path in backticks, joined as it joins
  the harness.
- `sandbox.go`: `build.kept` is the forbidden action that keeps a build session off the verify session's paths. It
  names each path.
- `fix.go`: `newFixing` gives the fix each part before `{spec}`. `fixing.kept`, the fix's forbidden action, and
  `fixing.contract` name each.
- `monitor.go`: `config.stepsRefusal` refuses a monitor without `"tests": true` while `acceptance` names no path,
  or any path has no part before `{spec}`. Today it refuses one while the one path has none.
- `kiln.go`: `Repo.ready` refuses, each with its own reason:
    - an `acceptance` that names no path
    - a path without `{spec}`
    - a path that starts with `{spec}`
    - a path that `path.Clean` changes, but for its last slash, or one that starts with `/`
    - a list in which one path's part before `{spec}` starts another's, or equals it, with case folded
- `kiln.go`: `testsText` takes each part before `{spec}`. It answers the first line of the build agent that names
  any. A line names a part where the part starts a path: at the line's start, or after a character no path holds,
  such as a space, a backtick, a quote or a parenthesis. Today it answers a line that holds the one part anywhere,
  so a part such as `it/` would refuse a line that names `internal/git/`.
- `kiln.go`: `Repo.queueable` refuses a spec whose filled-in path and a harness path are one path, or one is `within`
  the other, whatever the workflow. The package doc says each refusal.
- `contract/build.md`, `contract/build-split.md` and `contract/verify.md`: each names every path where it names the
  one today.
- `presets/go/kiln.json` and `presets/python/kiln.json` keep `acceptance` as a string, and no preset's or default
  agent's text changes. Test 7 of `languages`, a spec that waits for UAT, reads the Python preset's line as a
  string, and no build rewrites another spec's test.
- `README.md`: the bullets on `acceptance`, on the split workflow and on a monitor's fix say "each path". The bullet
  on `acceptance` shows the list form. It says that a project with more than one path says in its verify agent
  which test goes in which, and in its audit agent where each test's code is. The audit agent's "Acceptance tests"
  section is the project's own, and kiln's audit contract names no path. The bullet on a monitor's fix says what the
  guard costs: a fix cannot change a lasting suite or a harness under a path's part before `{spec}`. So a fix
  cannot change a name that such a suite calls.

### The forms a Then reads

- A refused queue prints `kiln queue: refused: ` and its reason on stderr. `kiln status` then lists no line for the
  spec.
- The hook's refusal is in the session's stream, as `PreToolUse:Write hook error:` followed by the path and that it
  is the verify session's.
- `kiln fire` prints `<name>: landed on main` for a spec that lands.
- Test 3 reads the build session's checkout through that session: its scripted command lists the checkout's files
  to a file the test reads, as `uat/testdata/build/split-revised-spec.txtar` does. That scenario shows the gate too.
  Its `kiln.json` gives the build no round, so one failed gate stops the build. The old tests fail on the revised
  code, so a build that lands ran none of them.
- Test 7's monitor names a command, and no check, gauge or agent. `kiln monitor run` runs a monitor that never ran,
  and its fix starts with the command. kiln refuses the command's change, and no agent is there to take the refusal.
  So the fix stops: `kiln status` shows the monitor's name and `stopped`.
- Each session's instructions name each of the spec's paths in backticks, in kiln's contract. A test reads them as
  `uat/testdata/agents/prompt-by-workflow.txtar` does.

### Tests

- `rules_test.go`: `TestGuard` gains rows for a side with two paths, and `TestHookArguments` a repeated
  `--acceptance`.
- `agent_test.go`: `TestQueueNeedsTestsPlace` gains rows for a list, a path without `{spec}`, a path not in its clean
  form, a list whose parts before `{spec}` start one another, a build agent that names the second path, one that
  names a path which only ends in a part, and a spec named for a harness folder.
- `monitor_test.go`: a row for a monitor without `"tests": true` under an empty list.
- A table that parses `acceptance` as a string, as a list, and as neither.
- `internal/scenariotest/seed.go` keeps seeding `acceptance` as a string, so every scenario kiln holds runs the old
  form.

## Alternatives Considered

### A third directory for each spec's tests

- **Pros**: `acceptance` stays one string, and kiln changes nothing.
- **Cons**: a spec's tests sit apart from the suite they fold into, and from that suite's harness. One folder holds
  tests of two kinds, which boot the program in two ways.
- **Reason for Rejection**: the developer chose the list on 2026-11-15, as Decision 1 says.

### Refusing a name on any path main holds

- **Pros**: it covers a lasting folder that is no harness, such as kiln's own `uat/testdata/`.
- **Cons**: kiln must tell a spec's own old tests from another folder, by the subject of a landing's commit on main.
  A project that wrote a spec's folder by hand must move or delete it before it queues the spec.
- **Reason for Rejection**: the developer chose the harness rule on 2026-11-16, as Decision 3 says.

### A key of its own for each kind of test

- **Pros**: `acceptance` stays one string, so no parser takes two forms.
- **Cons**: kiln would name a project's kinds of test, and a third kind would wait on a kiln release.
- **Reason for Rejection**: a list takes any number of kinds, under one rule.
