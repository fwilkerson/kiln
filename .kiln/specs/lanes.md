# Spec N: spec builds and monitor fixes run side by side

Status: Proposed

<!-- A starter, drafted on 2026-11-17, when the bugs monitor drained a queue of 31 bugs and no spec could build
meanwhile. Run /shape lanes to take it up: the skill resumes it from its Open Questions, then writes the acceptance
tests and the Technical Plan. -->

## Overview

One kiln works in a repository at a time. `kiln fire` and `kiln monitor run` each take the run lock, and each
refuses while the other holds it. A monitor may hold it all day: one that drains a queue runs fix after fix. A spec
the developer queues meanwhile waits until that run ends.

```text
today    kiln monitor run ── fix ── fix ── fix ── fix ──▶          kiln fire: refused, another kiln builds
with     kiln monitor run ── fix ── fix ── fix ── fix ──▶          the monitors' lane
         kiln fire ───────── build x ───────── build y ──▶         the specs' lane
                                  each lands on main in its turn
```

After this spec, the repository has two lanes. `kiln fire` builds queued specs in one, and `kiln monitor run` runs
monitors in the other, at the same time. Two kilns in one lane still refuse.

What showed the need: on 2026-11-17 the developer filed 31 bugs, and the bugs monitor drained them one fix at a
time, for hours. No `kiln fire` could start until it ended.

Most of what two lanes need is there. Each build and each fix works in a checkout of its own. A landing already
expects main to move under it: it rebases, and gates again. When the rebase conflicts, the conflict goes back to
the build's own session, which merges main, resolves it, and is gated again.

## Goals

- `kiln fire` builds specs while `kiln monitor run` runs monitors, and each lands on main in its turn.
- A second `kiln fire`, or a second `kiln monitor run`, still refuses while the first runs.
- A kiln that died in one lane is recovered without touching what the other lane is building.
- `kiln status` and `kiln tui` show both lanes' work as they show one today.

## Non-goals

- Two spec builds at once, or two fixes at once.
- A new agent that resolves a conflict. The build's session, or the fix's, resolves it today.
- A share of the usage limit for each lane. Both draw on one account, as they do today in turn.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

1. Do two gates run at once? Each gate runs the project's tests, and two at once compete for the machine. kiln's own
   terminal scenarios wait on a screen, so a slow machine could fail them. Lean: no. A lock lets one gate run at a
   time, and the sessions, where a build's time goes, still run side by side.
2. Which kiln recovers what a dead kiln left? Today the next `kiln fire` or `kiln monitor run` requeues every build
   marked building, and `kiln monitor run` drops every fix marked building. Neither can tell a dead kiln's record
   from the other lane's live one. Lean: each lane recovers only its own records, and tells a live one by its
   process, as `kiln tui` does.
3. Which lane lands what `kiln accept` queued? Today the kiln that holds the run lock lands it, or the next
   `kiln monitor run` does. Lean: the lane the record belongs to. A spec's landing waits for the specs' lane, and a
   fix's for the monitors' lane.
4. Does a round spent on main moving count? A fix that lands under a build sends the build back to rebase and gate
   again, and a conflict costs it a round. A monitor that lands often could spend a build's rounds. Lean: as today,
   until a build shows it stopped that way.
5. Do the lanes share a scratch checkout? The monitor's check, the gauges' read and the gate's baseline of main each
   use one checkout for the repository. Lean: with one gate at a time, the gate's and the gauges' stay shared, and
   the monitor's check keeps its own.
