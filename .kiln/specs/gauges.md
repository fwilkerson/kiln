# Spec N: Gauges and monitors

Status: Proposed

<!-- A starter, taken from docs/later.md on 2026-11-09. Run /shape gauges to take it up: the skill resumes it from its
Open Questions, then writes the acceptance tests and the Technical Plan. Open Question 1 may split it in two. -->

## Overview

The developer curates a codebase kiln builds. A gauge shows them a number over time against a line they set, such as
mutation score or flaky tests, and says when it crosses. A monitor watches the running software, and turns what it
sees into a failing test that kiln builds a fix for.

What showed the need: the developer chose it on 2026-11-09. Neither trigger in `docs/later.md` has fired as the
repository shows: kiln has built only itself, and kiln itself runs nowhere but the developer's terminal.

## Gauges

A gauge is a number over time, with a line the developer sets.

| Gauge              | Source today                                                                                                                   |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------|
| Flaky tests        | Eidos's `scripts/gate.sh` `suite_again` notes each test that passed on a second run alone. A gauge counts those notes per gate |
| Slow tests         | Eidos's `scripts/health.py`: the slowest tests, suite CPU and wall time                                                        |
| Comment share      | odh's `tools/size`: comment lines per 100 lines, by directory                                                                  |
| Agent friction     | kiln's ledger, `.git/kiln/ledger.jsonl`: failed calls, turns and context per build, which `kiln tui` already shows             |
| Mutation score     | Eidos's `scripts/mutate.sh`                                                                                                    |
| GoLand inspections | odh's `tools/inspect`. It needs GoLand open on the machine, so it can't gate an unattended build                               |

- **Testable code.** The developer cares that a build writes testable code, not in what order. Mutation score is the
  lean for its gauge: it falls when tests can't catch a change, and extra tests that catch nothing don't raise it.
  Coverage would reward tests written for their own sake.
- A gauge's command and its line sit in `kiln.json`, beside the gate. kiln reads `kiln.json` from main's tip, so a
  build can't move its own line, as it can't weaken its own gate. Settled by that rule in `README.md`; the draft's
  `gauges.json` would be a second file read the same way.

## Monitors

A monitor watches running software (tests, logs, metrics, traces) and hands what it finds to kiln.

```text
monitor ─▶ observation, keyed by signature, counted
               │ new, or over its threshold?
               ▼
         reproducer: write a test that fails, citing the Given, When, Then or rule it breaks
          │ fails, with a citation         │ can't, or cites nothing
          ▼                                ▼
   a spec of one acceptance test     stays an observation; a gauge shows its count
          │
          ▼
        kiln
```

- The store holds observations: signature, first and last seen, count, and state.
- No judge weighs findings in English. A failing test is the only way in.
- The developer approves no one-test spec. Its citation ties it to intent already approved.
- Cap how often one signature reaches kiln, so a partial fix can't loop.

## Non-goals

- Flaky tests in the gate: comparing each failure with main, and a quarantine list. `docs/later.md` "Flaky tests in
  the gate" holds it, and its trigger is the flaky-test gauge rising.

## Open Questions

1. **One spec or two?** Lean two, gauges first. A gauge needs only a command and a store. A monitor needs software
   running somewhere, a new role (the reproducer), and a new way into kiln's queue: a spec no developer approved.
   Either way gauges come first, since a monitor's unreproduced observations show only as a gauge's count.

2. **What does the first monitor watch?** No project kiln builds runs anywhere today. The one log kiln has is its own
   session streams, where `kiln tui` already groups failed calls by message, which is a signature and a count. But
   those are sandbox and tool failures that break no Given, When, Then, so the reproducer would cite nothing and each
   would stay an observation. Lean: wait for a running project, and name it here.

3. **What happens when a gauge crosses its line?** Lean: `kiln status` and `kiln tui` show it, and nothing else. A
   gauge serves the developer's curating, and a line that blocks a landing is a gate, which `kiln.json` already holds.

4. **Which gauge comes first?** Lean mutation score, since it measures the testable code the developer cares about,
   and Eidos's `scripts/mutate.sh` mutates the Go lines a build added. It is report-only there, and run by hand.

5. **When does a gauge run?** Lean: once per landing, on main's new tip, and stored in kiln's ledger beside the
   build's other numbers. A mutation run may take minutes, so it should not hold up the next queued build.
