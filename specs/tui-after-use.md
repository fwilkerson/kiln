# Spec N: kiln tui, after its first use

Status: Proposed

<!-- A starter, taken from docs/later.md on 2026-11-08. Run /slice tui-after-use to take it up: the skill resumes it
from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

The developer reads each build in `kiln tui` newest first, reads in full what each agent answered, and learns every
key a screen takes from the screen itself, on a MacBook's keyboard.

What showed the need: the developer used spec 3's `kiln tui` on its own second build, and noted each point below.

## Goals

- **Newest first.** The run screen lists a run's calls newest first, so a running session's newest call is at the top.
  The spec screen lists its builds newest first. Today both list oldest first.
- **What an agent answered.** A run's answer is the free text of its stream's `result` event. The run screen shows it
  only as a `said` line cut to one row. The developer can read the whole answer, as Open Question 1 settles.
- **Keys every keyboard has.** Each footer names keys a MacBook has, such as `j`/`k`, space/`b` and `g`/`G`, in place
  of end and the arrows. Today the footers name pgup, pgdn and end (`internal/tui/draw.go:158-161`), which a MacBook
  lacks. The text screens already take space, `f`, `b`, `d`, `u`, `g` and `G` through Bubbles' viewport
  (`internal/tui/model.go:239-244`); the list screens take only `j`, `k`, the arrows, pgup and pgdn
  (`internal/tui/model.go:345-348`).
- **A key list on `?`.** Each footer also names `?`, which opens a popover listing every key the screen takes. `?` for
  help is the convention of Bubbles' `help` component, lazygit and k9s.
- **"context peak" is "context".** The build screen's column header says "context peak"
  (`internal/tui/draw.go:551`), but a run records one context figure.
- **Keep** `j` and `k` moving the selection on the list screens.

## Non-goals

- The audit answering in JSON, unless Open Question 1 picks it.
- A count of passing acceptance tests while a build runs, unless Open Question 2 takes it in. `docs/later.md`
  "Progress while a build runs" holds it.

## Open Questions

1. **How does the developer read an agent's whole answer?**

   | Option                                                            | Changes the audit's contract | Each finding opens on its own |
   |-------------------------------------------------------------------|------------------------------|-------------------------------|
   | a) The run's whole answer, on the run screen or its own           | No                           | No                            |
   | b) The audit answers JSON (`claude -p --json-schema`), a row each | Yes                          | Yes                           |

   Lean a). kiln hands the audit's text to the build session as its prompt (`internal/kiln/build.go:163-170`), so b)
   changes what the build session reads as well as what the developer sees. The need noted was reading the answer,
   which a) meets with nothing new from the agent. b) comes back if the developer wants to act on one finding at a
   time.

2. **Does this spec also count passing acceptance tests while a build runs?** Lean no. The gate records only failures:
   it reads `--- FAIL:` lines from `go test ./...` (`internal/kiln/gate.go:102`), which prints no passes, so a count
   needs kiln to run the acceptance tests in a form that names each pass. Take it in if the developer wanted the count
   while watching spec 3's 49-minute build.
