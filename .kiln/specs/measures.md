# Spec N: Join gauges and monitors

Status: Proposed

<!-- A starter, drafted on 2026-11-14 while setting up kiln's code-health monitor. Run /shape measures to take it
up: the skill resumes it from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

A gauge and a monitor's check each measure the repository with a script a build can't change. The developer writes
the measurement once, and says in `kiln.json` what kiln does with it: show it after each landing, hold a monitor's
fix to it, or start a fix when it crosses its line.

Today kiln has three ways to state what the repository should hold, and each stands alone:

| What    | Runs when            | Reads              | Its last line                | What follows          |
|---------|----------------------|--------------------|------------------------------|-----------------------|
| Gate    | each turn of a build | the build's commit | none: its exit code          | blocks the landing    |
| Gauge   | after each landing   | one landing's diff | a number                     | shown beside its line |
| Monitor | on its schedule      | main as it stands  | `{"fix": …, "summary": "…"}` | a fix                 |

What showed the need: on 2026-11-14, the developer set up the monitor `code-health`, which each day refactors the
three files with the lowest CodeScene Code Health. kiln already had the gauge `code health`, with a line of 10. At
`3b65d94`, setting it up showed three seams:

- **One measurement, written twice.** `.kiln/gauges/code-health` prints the lowest score among the files a landing
  changed. `.kiln/monitors/code-health` has two modes, since kiln runs one check command at main and in the fix's
  gate. At main, it lists the lowest files in the repository. In the gate, it fails while a file the fix changed
  scores below 10.0, which is the gauge's reading held as a line that must hold. Both scripts repeat the lookup of
  the CodeScene CLI, the token and the loop that scores each file.
- **Nothing follows a crossed line.** `kiln status` showed `gauge code health: 9.11, line ≥ 10, crossed, at
  monitors's landing 144d20f43378`. A crossing waits for the developer to read it, and what would follow it is a
  monitor's fix.
- **A check has no number.** The check's summary read "10 of 76 files score below 10.0", as prose. `kiln status`
  shows a monitor's last summary, and no trend over its runs.

## Goals

- The developer writes one script for a measurement, and a gauge and a monitor both use it.
- A monitor can hold its fix to a gauge's line, so its check needs no second mode for the gate.
- A crossed gauge can lead to a fix without the developer reading `kiln status` first.
- `kiln status` shows how what a monitor measures moved over its runs, as it shows a gauge's readings.

## Non-goals

- A gauge that blocks a landing. A line that must hold for each build is a gate command, as the README says.
- A schedule at a time of day, which spec 11 left out.

## Open Questions

1. **One kind of entry, or two that are joined?** Lean two. A gauge and a check read different things: 9.11 was the
   lowest file one landing touched, and 7.8 was the lowest file in the repository on the same day. One merged entry,
   such as a `measures` list with `on` and `fix` keys, would have to say which it reads, and would be two kinds
   again. What decides: whether any measurement the developer runs wants both readings from one entry.

2. **May a monitor name a gauge as its gate?** Lean yes, such as `"gates": "code health"`. kiln would read the
   gauge at the fix's commit, with the fix's merge base in `$KILN_BASE`, and fail the gate while the reading crosses
   the gauge's line. A gauge that kiln could not read would end the fix `couldn't run`, as a gated check does.
   Open under it: whether the monitor's check still joins the gate too, and what the agent is told when the gauge
   fails, since a gauge prints a number and no findings.

3. **May a crossed gauge start a monitor?** Lean yes, as a rule for when a monitor is due, not a run inside
   `kiln run`. A gauge is read by the kiln that landed the build, which holds the run lock. A monitor that names
   the gauge would be due at the next `kiln monitor run` once a reading crossed since its last run. Open under it:
   what the agent reads as the finding. `.kiln/gauges/code-health` prints each file's score to stderr, which kiln
   drops today.

4. **One contract for the last line?** A gauge's last line is a decimal number, by `readingOf` in
   `internal/kiln/gauge.go`. A check's is a JSON object, by `runMonitorCheck` in `internal/kiln/monitor.go`. Lean:
   a check's object may carry `value`, a number, which kiln records in the monitor's line of the ledger and
   `kiln status` shows over the runs. A gauge's bare number stays as it is. The alternative is that a gauge may print
   the object too, so one script serves both, at the cost of two forms for a gauge.

5. **One directory?** `.kiln/gauges/` and `.kiln/monitors/` are both out of a build's and a fix's reach. The
   repeated lines need no change to kiln: a third script that both call would hold them today. Lean: leave the
   directories, unless question 1 ends in one kind of entry.

6. **What is the spec named?** `measures` assumes the word kiln would use for what a gauge and a check share. Lean:
   settle it with question 1.
