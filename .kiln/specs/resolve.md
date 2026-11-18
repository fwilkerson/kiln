# Spec N: an accepted fix that main moved past still lands

Status: Proposed

<!-- A starter, drafted on 2026-11-18, when the uat and docs monitors' accepted fixes could not land after the bugs
monitor landed 35 fixes. Run /shape resolve to take it up: the skill resumes it from its Open Questions, then writes
the acceptance tests and the Technical Plan. -->

## Overview

A monitor with `"accept": true` leaves its fix waiting on its branch, `kiln/<name>`, until the developer runs
`kiln accept <name>`. Main moves meanwhile. When the landing then fails, no session is there to bring the fix up to
date, and the developer's only way on is to drop the fix.

```text
today    kiln accept docs ── rebase: conflict ─────────────▶ stopped: run kiln reject docs
         kiln accept uat ─── rebase ── gate: check fails ──▶ stopped: fix main, and accept again
with     kiln accept docs ── rebase: conflict ── resolve session merges main ── gate ──▶ lands
```

After this spec, an accepted fix whose landing stops because main moved goes to a session that resolves it: a new
agent, `resolve`, whose only work is to bring a branch onto main. It merges main, resolves each conflict so that
both main's change and the fix's intent hold, and ends its turn. kiln gates the result as it gates any fix.

What showed the need: on 2026-11-17 the uat and docs monitors each left a fix waiting. The bugs monitor then
landed 35 fixes, and main moved 32 commits past both. On 2026-11-18:

- `kiln accept docs` stopped on conflicts in `docs/reference.md`, `internal/git/git.go` and
  `internal/kiln/kiln.go`. kiln named `kiln reject docs`, which drops the fix and its cost, and lets the next run
  start over from main.
- `kiln accept uat` rebased cleanly, but the gate failed: the uat check still asked for a fix, since main had
  gained scenarios to fold. kiln named "fix main, and run kiln accept uat to land it again". The developer has
  nothing on main to fix, and each accept stops the same way.

## Goals

- `kiln accept <name>` lands a fix whose branch conflicts with main, once the resolve session and the gate pass.
- The developer never has to resolve a conflict by hand, or reject a fix only because main moved.
- `kiln init` writes the `resolve` agent for any project, with kiln's contract after its body, as it writes the
  others.
- `kiln status` and `kiln tui` show a fix being resolved, and what stopped a resolve that failed.

## Non-goals

- A spec build's conflict. It goes back to the build's own session, which knows the spec, as it does today.
- Keeping a waiting fix up to date while it waits, by rebasing it as main moves.
- A resolve for a branch nobody accepted. A fix that conflicts while its monitor runs goes back to its own
  session, as today.

`.kiln/specs/lanes.md` lists "a new agent that resolves a conflict" as a non-goal. This spec takes that agent up
for accepted fixes only, so the lanes spec's non-goal still holds for builds.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

1. Which session takes a landing whose rebase was clean but whose gate failed, as uat's did? The conflict is the
   resolve agent's work, but folding what main added is the monitor's own. Lean: a conflict goes to the resolve
   agent, and a failed gate after a clean rebase goes to the monitor's agent, in a new session of the fix, with the
   gate's report. A monitor with only a command stops as today.
2. Does a resolved fix land at once, or wait for `kiln accept` again? The developer accepted a diff, and the
   resolution changes it. Waiting may never end while another monitor lands each hour. Lean: it lands once its
   gate passes, check included. The landing commit's body names each file the resolution changed beyond the
   accepted fix, so `git show` shows what the developer did not read.
3. Which kiln runs the resolve session? Today the kiln that lands an accepted fix is `kiln accept`, or the next
   `kiln monitor run` for an accept that came while one ran. Lean: that same kiln, as a build's landing sends its
   conflict back to its session in the kiln that lands it.
4. How many tries does it get? Lean: the rounds of the gate, as `kiln.json` gives a fix. Past them it stops, and
   `kiln status` names `kiln reject <name>`, as today.
5. Does the audit read the resolution? It reads each fix whose agent ran. Lean: yes, over the files the resolution
   changed, since a wrong merge is the error the gate is likeliest to miss.
