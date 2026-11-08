# Spec 1: kiln

Status: Approved on 2026-11-08. kiln's first spec, built by hand with one `claude -p` session.

## Overview

The developer approves a spec and walks away. They come back to it built, with its acceptance tests passing, having
been asked nothing but about the spec. Eidos showed the opposite: 24 of 28 questions it parked were about its own
plumbing, and the developer took the agent's pick on all 5 they answered.

## Goal and proof

| Proof                              | Measured by                                                                   |
|------------------------------------|-------------------------------------------------------------------------------|
| The spec is met                    | Every acceptance test passes on main                                          |
| Nothing pulled the developer away  | No build stops. The only thing a build sends the developer is an amendment    |
| The developer knows what was built | They accept from the spec, its amendments and its tests, without reading code |
| It is worth it                     | Keeping kiln running costs less time than doing the work by hand              |

This spec is the first proof. One `claude -p` session builds it from this file, and reports each amendment and stop.

## Words

| Eidos today         | Here                                                                                           |
|---------------------|------------------------------------------------------------------------------------------------|
| Scenario, criterion | **Acceptance test**: a Given, When, Then the developer approves, and the test code under it    |
| Item, run, stage    | **Build**: one session's work on one spec, on its own branch                                   |
| Park                | **Amendment**: a Given, When, Then the build changed because it couldn't hold, with the reason |

A build never asks the developer anything. Where a Given, When, Then can't hold, it amends it and finishes the work.
The Given, When, Then text is the developer's. The test code under it is the build's, as Cucumber splits a feature
file from its step definitions. A Given, When, Then names only what the intent names: never a timeout, a length or a
message's wording.

## Goals

- **Write a spec.** odh's `/slice` skill, copied in, runs in an interactive Claude session. It drafts
  `specs/<name>.md` first, ranks questions with a lean, then runs `zoom-out`, `comprehension` and `critic` once.
  Revising a spec is editing it with the same skill.
- **Tie each acceptance test to its code.** A spec numbers its acceptance tests. Test code for test N of spec S is a Go
  test in `acceptance/<S>/` whose name starts `TestAT<N>`.
- **`kiln init`** writes a starting `kiln.json` and installs a pre-commit hook. On a commit to main, the hook runs
  the gate's commands and refuses the commit if any fails.
- **`kiln approve <name>`** records the spec's commit and queues it.
- **`kiln run`** builds the queued specs one at a time, until the queue is empty.
    1. A session starts on a branch made from main. It writes testable code, and the tests its acceptance tests and
       rules need, as "How a build writes tests" says.
    2. The gate runs the commands in `kiln.json` at the branch's tip and at main. It passes when every command exits 0,
       but for Go tests that fail at main too. Those are named, and don't block.
    3. A failing gate goes back to the same session, at most `rounds` times, set in `kiln.json`. Past that the build
       stops, and `kiln status` shows why.
    4. `audit`, from odh, reads the changed packages once. It also reads each acceptance test's code beside its Given,
       When, Then, and reports code that asserts less than its Then. Findings go back to the session, and the gate runs
       again.
    5. With no amendment, the build lands: rebased onto main, gated again, then main moves to it.
    6. With an amendment, the finished work waits on its branch.
- **Amendments** are written to `specs/<name>.amend.md`: each Given, When, Then's old and new wording, and the reason.
  It is the one file under `specs/` a build may write.
- **`kiln status`** shows each spec's state: queued, building, stopped, waiting for UAT, or waiting on its branch with
  amendments. It names the tests that failed at main before the build.
- **`kiln accept <name>`** ends UAT. A spec waiting on its branch first folds its amendments into the spec and lands as
  in step 5. Then the spec is deleted, and `PRODUCT.md` gains its line, naming the commit that last held it.
- **Claude guidance.** A build may write `CLAUDE.md`. It writes files meant for `.claude/agents/` or
  `.claude/skills/` under `guidance/`, and landing copies them in. Claude Code refuses a headless session's write
  under `.claude/` even when an allow rule or a hook allows it. `.claude/settings*` stays the developer's.

kiln keeps each spec's state under the git common directory. The gate's commands, odh's `tools/style`, `arch_test.go`
and `style_test.go` hold kiln's own repository; a repository kiln builds brings its own `kiln.json`.

## How a build writes tests

Each test costs time on every gate and care on every change. A test is held to the same standard as the code it
tests.

- An acceptance test's code tests what its Then says, through the program's command line or HTTP.
- Below that, a test is written for a domain rule or for how the code is wired, as `docs/practices.md` "Tests" says. A
  rule's test is not repeated at a higher layer.
- A test that only restates the code, pins a name no caller sees, or exists to raise coverage is not written.

## Non-goals

Each comes back only when a build shows the need. `docs/later.md` holds a brief on each.

- Tickets, a backlog, a viewer.
- Gauges: flaky tests, slow tests, comment share. They come in spec 2.
- Several builds at once, or per spec, and a planner to split one.
- Monitors that turn logs or metrics into work.
- Repair sessions, an exit-code contract for tests, limits on amendments.
- Repositories in languages other than Go.
- Notifications. Ghostty's are the first to try.

## Open Questions

None.

## Acceptance tests

The tests run the built `kiln` against a scratch repository, with Eidos's scripted claude in place of the model.

### 1. An approved spec lands

- **Given**: an approved spec whose build makes its acceptance tests pass.
- **When**: the developer runs `kiln run`.
- **Then**: main holds the build's work, its acceptance tests pass there, and `kiln status` shows it waiting for UAT.

### 2. A build can't change what the developer approved

- **Given**: a build that changes a Given, When, Then in `specs/<name>.md`, by an edit or by a shell command.
- **When**: its gate runs.
- **Then**: the gate fails, and `specs/<name>.md` on main is as approved.

### 3. Main moves only to work gated on it

- **Given**: a spec waiting on its branch with an amendment, while another spec's build lands.
- **When**: the developer accepts the waiting spec.
- **Then**: its work lands only after a gate that ran on main as the other build left it.

### 4. Main stays green

- **Given**: a change on main that fails a test.
- **When**: the developer commits it.
- **Then**: the pre-commit hook refuses the commit, naming the test.

### 5. An amended spec is delivered whole, and waits

- **Given**: a build that finds a Given, When, Then can't hold, amends it, and finishes the work under the amendment.
- **When**: its gate passes.
- **Then**: main is unchanged, the finished work waits on the spec's branch, and `kiln status` shows the amendment.

### 6. Accepting a spec retires it

- **Given**: a spec waiting for UAT, landed or on its branch with an amendment.
- **When**: the developer runs `kiln accept`.
- **Then**: main holds the spec's work and its amendments, `specs/` no longer holds it, and `PRODUCT.md` names it with
  its commit.

### 7. Revising a spec builds it again

- **Given**: a spec waiting on its branch with an amendment.
- **When**: the developer edits the spec and approves it again.
- **Then**: a new build starts from the edited spec, and the amended work never reaches main.

### 8. A usage limit only pauses

- **Given**: a build whose session hits the usage limit.
- **When**: the limit resets.
- **Then**: the same session resumes, and nothing reaches the developer.

### 9. A build that can't pass its gate stops

- **Given**: a build whose gate still fails after its last round.
- **When**: the round ends.
- **Then**: main is unchanged, and `kiln status` shows the build stopped with the gate's output.

### 10. A test that fails on main too doesn't block a build

- **Given**: a test that fails on main, such as a flaky one.
- **When**: a build's gate runs.
- **Then**: the build lands, and `kiln status` names the test as failing on main.

## Technical Plan

Copy in what solves this spec's problems, from Eidos at `07992d10` (`~/github.com/fwilkerson/eidos`) and odh at
`b978d94` (`~/github.com/fwilkerson/odh`). Eidos's packages are under `internal/runner/internal/`.

| From                                                                        | What it gives                                                             | What is new                                                                             |
|-----------------------------------------------------------------------------|---------------------------------------------------------------------------|-----------------------------------------------------------------------------------------|
| Eidos `claude`, `proc`                                                      | Headless sessions, `--resume`, orphaned processes cleaned up              | Pausing until a limit resets, as `Runner.pausedFor` in `domain/runner.go` does (test 8) |
| Eidos `git`                                                                 | Branches, worktrees, rebase, and `Git.Move`, which fails if main moved    | The order: rebase, gate, move (tests 1, 3); the pre-commit hook (test 4)                |
| Eidos `domain/policy.go`, `policy_path.go`, `guarded.go`                    | A hook refusing Edit, Write, MultiEdit and NotebookEdit on guarded paths  | `specs/` guarded, but its `.amend.md` files                                             |
| Eidos `domain/gate.go` `Run.ownChecks`                                      | The gate refuses any changed path the guard names, however it was written | The same check on `specs/` (test 2)                                                     |
| Eidos scripted claude in `internal/scenariotest`                            | Running the acceptance tests with no model calls                          | Cut its link to `internal/app` and the viewer                                           |
| odh `/slice`, `zoom-out`, `comprehension`, `critic`, `audit`, `template.md` | Writing and reviewing a spec, and the audit                               | `audit` also reads test code beside its Then                                            |
| odh `tools/style`, `arch_test.go`, `style_test.go`                          | The rules kiln's own gate holds                                           | None                                                                                    |

`claude`, `git` and the hook's files import Eidos's `domain` and `port`. Each gets small types of its own in their
place.

## Alternatives Considered

### Keep the ticket runner and add precedents

- **Pros**: Eidos's runner is built and has landed real work.
- **Cons**: A precedent learns from answers, and every answer took the agent's lean.
- **Reason for Rejection**: It would learn "take the lean", the same as not asking.

### Agent-written tests locked against change, as Eidos's scenarios were

- **Pros**: No build can weaken a test to pass.
- **Cons**: S65, S136 and S195 locked details the spec never asked for, and each became a park.
- **Reason for Rejection**: Only the developer's Given, When, Then is locked. The audit reads the code under it.

### Drop the audit, since no proof measures code quality

- **Pros**: One session round and one gate fewer per build.
- **Cons**: Nothing else reads the test code against its Then, so a weakened test passes proofs 1 and 3 on paper.
- **Reason for Rejection**: The audit is what makes the test code's freedom safe.
