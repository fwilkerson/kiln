# Spec N: One way to run a workflow, for specs and monitors alike

Status: Proposed

<!-- A starter, drafted on 2026-11-22 from a review of 125 audit findings across kiln's builds and monitor fixes. Run
/shape workflows to take it up: the skill resumes it from its Open Questions, then settles the acceptance tests and
the Technical Plan. -->

## Overview

A project picks how kiln builds a spec, and a monitor picks how kiln builds its fix, with one key, `"workflow"`, in
`.kiln/kiln.json` and in `monitor.json`. Each workflow is open to both:

| `"workflow"`       | Sessions                                                                                |
|--------------------|-----------------------------------------------------------------------------------------|
| `build`            | one build session writes the code and its tests (the default)                           |
| `build-and-verify` | a build session and a verify session work side by side; verify never sees the code      |
| `verify-first`     | the verify session writes the tests, kiln proves they fail at main, then the build runs |

Today the key and its three names exist, but each workflow runs on its own code, and each is open to one side only:
`build-and-verify` to specs, `verify-first` to monitors. A spec's build cannot prove its acceptance tests fail before
the code exists, and a monitor's fix cannot run its verify session beside the fix.

What showed the need: the audit review of 2026-11-22. Of the bugs monitor's 64 findings, 13 were weak tests, written
by the session that wrote the fix; `verify-first` was built for monitors to stop that. Specs drew 5 weak tests in 55
findings. A proof gate catches a test that passes at main, though not one that checks only part of its Then. And
`build-and-verify` and `verify-first` already share their clones, their blind settings and their guard sides, in
separate code.

## Goals

- Each workflow runs a spec's build and a monitor's fix alike, from one description of its steps.
- `verify-first` on a spec proves each acceptance test fails at main before the build session starts.
- A step the workflows share, such as a session's clone, its guard, a proof gate or the audit's routing, is written
  once.

## Non-goals

- A workflow a project defines itself, in a file of its own. kiln's three come first; a fourth waits for a need.
- A change to what a build, verify or audit agent's file says, beyond naming the workflow it runs under.
- The verify session running its tests on the build's code in its own turn. `.kiln/specs/stack-gate.md` holds that.

## Open Questions

- Should `verify-first` replace `build-and-verify` for specs, leaving one isolated workflow? It proves each test fails
  first, but costs the wall time the side-by-side sessions save.
- Do the verify and build sessions share kiln.json's rounds, or does each get its own? Today `verify-first` resets
  the count when the build session starts.
- Under `verify-first` on a spec, who may amend the spec: the verify session alone, as under `build-and-verify`?
- Does a spec's verify session under `verify-first` write only the acceptance tests, or any file `testFiles` matches?
- Should `kiln tui` and `kiln status` mark the proof gate as a step of its own?
- Does `build-and-verify` on a monitor's fix need a spec-like input beyond the check's finding?

## Acceptance tests

### 1. A spec's verify-first build proves its tests before the code

- **Given**: a project whose kiln.json says `"workflow": "verify-first"`, and a queued spec.
- **When**: `kiln fire` builds it.
- **Then**: the ledger shows a gate that failed on the spec's acceptance tests at main before the build session's
  first run, and the work lands.

### 2. A test that passes at main goes back to the verify session

- **Given**: the same project, and a spec whose verify session writes a test that passes on main's code.
- **When**: `kiln fire` builds it.
- **Then**: the verify session gets the proof gate's report as a round, and no build session has run.

### 3. A monitor's fix runs build-and-verify

- **Given**: a monitor whose monitor.json says `"workflow": "build-and-verify"`, and a check that finds a problem.
- **When**: `kiln monitor run` fixes it.
- **Then**: a verify session and a build session both ran for the fix, and the fix lands.

### 4. The build session cannot weaken a proved test

- **Given**: a spec or a monitor under `verify-first`, whose build session changes a proved test file.
- **When**: the build session writes it, or a shell command does and the gate runs.
- **Then**: the write is refused, or the gate fails, and main never holds the change.

## Technical Plan

<!-- To settle in /shape. The guide's "Working with agents on an API" applies: this shapes how kiln runs every build,
so two agents design it apart from the same brief, and a fresh judge scores both. -->

- A workflow is a value kiln declares, an ordered list of steps: sessions with their sides, a proof gate, a stack, the
  gate, the audit's routing, and the landing. `build.go`, `split.go` and `testfirst.go` fold into it.
- `config.Workflow` is one type, read from kiln.json for a spec and from monitor.json for a monitor's fix, with one
  refusal list.
- The guard reads a session's side from the workflow, not from a flag each caller sets.

## Alternatives Considered

### Keep `verifySession: true` as a boolean, and `test-first` as a monitor's own key

- **Pros**: no rename.
- **Cons**: two keys for one choice, and a third workflow would need a third key.
- **Reason for Rejection**: the developer chose one `workflow` key with three names on 2026-11-22. The 2026-11-21
  rename of `"workflow": "split"` to a boolean held while one workflow besides the default existed.
