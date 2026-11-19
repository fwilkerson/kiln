# Spec N: an accepted fix that main moved past still lands

Status: Proposed

## Overview

A monitor with `"accept": true` leaves its fix waiting on its branch, `kiln/<name>`, until the developer runs
`kiln accept <name>`. Main moves meanwhile. When the landing then fails, no session runs to bring the fix up to
date, and the developer's only way on is to drop the fix, or to do the work by hand.

```text
today    kiln accept docs ── rebase: conflict ─────────────▶ stopped: run kiln reject docs
         kiln accept uat ─── rebase ── gate: check fails ──▶ stopped: fix main, and accept again
with     kiln accept docs ── rebase: conflict ───────────── the fix's session merges main ──── gate ──▶ lands
         kiln accept uat ─── rebase ── gate: check fails ── the fix's session fixes the rest ── gate ──▶ lands
```

After this spec, an accepted fix whose landing meets a setback goes back to the session that made it, as a build's
landing does. The session is the one the monitor's agent ran to make the fix. kiln resumes it, with the files in
conflict or the gate's report. The session merges main, or fixes what the gate found, and ends its turn. kiln gates
the result, and lands it.

What showed the need: on 2026-11-17 the uat and docs monitors each left a fix waiting. The bugs monitor then
landed 35 fixes, and main moved 32 commits past both. On 2026-11-18:

- `kiln accept docs` stopped on conflicts in `docs/reference.md`, `internal/git/git.go` and
  `internal/kiln/kiln.go`. kiln named `kiln reject docs`, which drops the fix and its cost, and lets the next run
  start over from main.
- `kiln accept uat` rebased cleanly, but the gate failed: the uat check still asked for a fix, since main had
  gained scenarios to fold. kiln named "fix main, and run kiln accept uat to land it again". The fix landed 78
  minutes later, once a session the developer ran had folded those scenarios on main by hand.

Each fix's session was there to resume. kiln's record of a waiting fix keeps its session, and its worktree. The docs
session ended at 58k tokens of context, and the uat session at 111k.

## Goals

- `kiln accept <name>` lands a fix whose branch conflicts with main, once the fix's session merges main and the
  gate passes.
- It lands a fix that the gate fails on main as it moved, once the fix's session fixes what the gate found.
- The developer never resolves a conflict by hand, or rejects an agent's fix only because main moved.
- A landing that still fails keeps the fix. It stops, and takes `kiln accept` or `kiln reject`.
- `kiln status` and `kiln tui` show the session at work on a landing, and what stopped a landing that failed.

## Non-goals

- A spec build's conflict. It goes back to the build's own session, as it does today.
- Keeping a waiting fix up to date while it waits, by rebasing it as main moves.
- A fix that a monitor's command made alone. It has no session. Its conflict stops the landing and names
  `kiln reject`, as today, and the monitor's next run makes the fix anew from main, for the cost of the command.
- An accepted fix whose work main already holds. Its landing stops as today.
- A new agent whose only work is to resolve a conflict. `.kiln/specs/lanes.md` lists it as a non-goal too, and
  Alternatives Considered says why.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

1. Does the audit read what the session changed in the landing? Today the audit reads a fix once, and reads it
   again only for a path `kiln.json`'s `reaudit` names. Lean: yes, over the files the session changed since the
   developer accepted, since a wrong merge is the error the gate is likeliest to miss.

## Acceptance tests

### 1. An accepted fix that conflicts with main lands

- **Given**: a monitor whose fixes wait for `kiln accept`, and a fix its agent made, waiting on its branch. Main
  changed since, in a line the fix changed.
- **When**: the developer runs `kiln accept` with the monitor's name, and the fix's session merges main.
- **Then**: kiln resumes the session that made the fix, and tells it each file in conflict. After the gate passes,
  main holds both main's change and the fix's.

### 2. An accepted fix that the gate fails on main as it moved lands

- **Given**: such a fix, waiting on its branch. Main changed since, so that the fix rebases with no conflict, and
  the monitor's check asks for a fix again.
- **When**: the developer runs `kiln accept` with the monitor's name, and the fix's session fixes what the check
  found.
- **Then**: kiln resumes the session that made the fix, with the gate's report. After the gate passes, main holds
  the session's work.

### 3. A landing that still fails keeps the fix

- **Given**: a fix as in test 1, whose session leaves the gate failing in each round `kiln.json` gives it.
- **When**: the developer runs `kiln accept` with the monitor's name.
- **Then**: `kiln accept` fails, and main is unchanged. `kiln status` shows the fix stopped, with why, and names
  both `kiln accept` and `kiln reject`. A second `kiln accept` resumes the same session.

### 4. A session that asks for the developer keeps the fix

- **Given**: a fix as in test 1, whose session ends its turn asking for the developer.
- **When**: the developer runs `kiln accept` with the monitor's name.
- **Then**: the landing stops, and `kiln status` shows the session's report. The branch still holds the fix, and
  both `kiln accept` and `kiln reject` take it.

### 5. kiln status shows the session at work on a landing

- **Given**: a fix as in test 1.
- **When**: `kiln status` runs while the fix's session merges main.
- **Then**: it shows the monitor's fix landing, and that its session is at work on a conflict with main.

### 6. The landing's commit names what the session changed

- **Given**: a fix as in test 1, which changed two files. Main changed one of them since.
- **When**: the fix lands, after its session merged main.
- **Then**: the commit that moved main names the file the session changed, and not the file that lands as the
  developer accepted it.

### 7. The audit reads what the session changed

- **Given**: a fix as in test 6.
- **When**: the fix's session merges main, and the gate passes.
- **Then**: before main moves, the audit runs, and kiln tells it the file the session changed. The audit's
  findings go back to the fix's session.

## Technical Plan

All of it is in `internal/kiln`. No command, flag or key of `kiln.json` is new, and `kiln init` writes nothing
new.

### The landing

`landFix`, in `fix.go`, lands an accepted fix. Today each setback of `land` stops it. After this spec:

| Setback of `land`                  | The fix has a session           | No session      |
|------------------------------------|---------------------------------|-----------------|
| a conflict with main               | back to the session, as a round | stops, as today |
| the gate fails on the rebased work | back to the session, as a round | stops, as today |
| the work changes nothing on main   | stops, as today                 | stops, as today |

- A fix has a session when its record's `Session` is set: the monitor's agent ran. `fixLand` draws the same line
  today for a fix that never waited.
- "Back to the session" is `build.round`, then `fixLaunch` with resume, then the loop `fixAgent` runs: `talk`,
  `heard`, `titleOf`, `fixGate`. Share that loop between `fixAgent` and `landFix`, rather than copy it.
- The kiln that lands the fix runs the session: `kiln accept`, or the next `kiln monitor run` for an accept that
  came while another kiln held the run lock. `Repo.Accept` already runs a build's session so, for accepted
  amendments. A landing that `kiln monitor run` takes up opens its tab when it ends, as today.
- The rounds are `kiln.json`'s. `acceptFix` sets the record's rounds to 0 today, and so does each later
  `kiln accept`. Past the last round the landing stops.
- The session's first message in a landing says the developer accepted the fix, and that main moved since. A
  conflict's message names each file in conflict, one on a line, and asks the session to run `git merge main`,
  keep both main's change and the fix's, and end its turn. A failed gate's message holds the gate's report.
- Once the session's work passes the gate, `fixAfter` lands it. It never leaves an accepted fix waiting again:
  today `fixAfter` ends each fix of a monitor with `"accept": true` as waiting. The developer reads what the
  session changed after the landing, from the commit's `Changed since kiln accept:` line.

### What never drops an accepted fix

`fixEnded` drops the record, the branch and the worktree of each fix that ends other than stopped or waiting. In
a landing, each of these stops the landing instead, with the text as why:

- the session asks for the developer, with `{"ask": true, "summary": "..."}`: its summary and its report
- the session fails: its error
- a usage limit ends the session: `landingStops` already stops the landing for it

`contract/monitor.md` says of an ask that "kiln then drops what you changed". Add that in the landing of an
accepted fix, kiln keeps the fix, and the developer reads the report. Add to "How the fix goes" that a fix the
developer accepted may come back once more, after main moved.

### What the session changed

The record gains the commit the developer accepted: the branch's tip when `acceptFix` ran. A later `kiln accept`
of a stopped landing keeps it.

- The files the session changed are those whose own change differs between that commit and the work that lands.
  `ownChange`, in `reaudit.go`, already reads a work's own change since it left main, so that the same change
  reads the same on any base. Compare each file the two changed.
- The landing commit's body gains one line when a session ran in the landing, before its `Build:` line:
  `Changed since kiln accept: <path>, <path>`. With no such file, the line is absent.
- The audit runs once the session's work passes the gate, before the landing, when that list holds a file outside
  `.kiln/`. Its prompt is `auditingFix`'s, over those files and their directories. It counts as a round, as
  `reauditRound` counts an audit past the first. Its findings go back to the session with `fixAudited`.

### What the developer sees

- `kiln accept <name>` prints each step as a fix's run does, and `<name>: landed` last. When the landing stops,
  it exits non-zero, and its error names `kiln accept <name>` and `kiln reject <name>`.
- While the session runs, `kiln status` shows the monitor's line as `<name>: accepted, landing: ` then why the
  session runs, in the words `reason.shown` gives `kiln tui`: `conflict with main, round 1 of 3`, or
  `landing gate failed, round 1 of 3`. `kiln tui` shows the run so today, from its start line in the ledger.
- A stopped landing of a fix with a session shows, under the monitor's line in `kiln status`, why it stopped, then
  `next: cd <worktree> && claude --resume <session>, to ask the session why;` and
  `then run kiln accept <name> to land it again, or kiln reject <name>`. `fixStep` and `landAgain`, in
  `stats.go` and `kiln.go`, name `kiln reject` alone only for a conflicted fix with no session.
- The scripted claude records each call under `.claude-calls/<role>/<attempt>/<call>/`. A resumed call's
  `call.json` holds `"resume": true` and the session's ID, and its `prompt` file holds the message.

### Tests and docs the plan changes

- `uat/testdata/monitors/waits-for-accept.txtar` stays true: its fix is a command's, with no session.
- `TestFixStep`, in `rules_test.go`: its conflicted row is a fix with no session. Add the row with one.
- The package doc of `internal/kiln`: the fix's line "stops ──▶ accept or reject again", the Monitors section, and
  `Repo.Accept`'s doc, which says a conflicted landing waits for `kiln reject`.
- `docs/reference.md`, under monitors: "kiln then drops what it changed", and the diagram's "waits for accept".

## Alternatives Considered

### A new agent, `resolve`, in a fresh session

The starter of this spec proposed it: an agent `kiln init` writes, whose only work is to merge main into a branch.

- **Pros**: A fresh session reads no old context. It covers a fix a command made alone, which has no session.
- **Cons**: It holds none of the rules the fix is judged by, which the monitor's agent file holds, such as
  `.kiln/agents/fold.md`'s "Keep each check". It knows the fix only from its diff. A failed gate after a clean
  rebase is the monitor's own work, so it would need a second path, to the monitor's agent.
- **Reason for Rejection**: The developer chose the fix's own session on 2026-11-18. A build's landing, and the
  landing of a fix that never waited, already go back to their own session, so one path serves each setback.

### The fix waits for `kiln accept` again, once the session changed it

- **Pros**: Nothing of a monitor with `"accept": true` lands unread. That holds for new work after a failed gate
  too, such as scenarios the uat session folds beyond those the developer accepted.
- **Cons**: The developer accepts twice. Main may move between the two, and a new conflict starts another round.
- **Reason for Rejection**: The developer chose the landing on 2026-11-18. A spec's build lands before the
  developer reads it too, and waits for UAT on main. The landing is one commit, which `git revert` undoes, and its
  body names each file the session changed.

### Drop the fix, and run the monitor again from main

- **Pros**: No merge. The new fix is made against main as it stands.
- **Cons**: It pays for the whole fix again, and the developer reads a whole new diff. The uat fix of 2026-11-17
  cost $3.39 and 16 minutes. Main may move again before the developer accepts the new one.
- **Reason for Rejection**: It is what `kiln reject` gives today, and what showed the need.

### Rebase a waiting fix each time main moves

- **Pros**: The fix the developer reads is always up to date.
- **Cons**: Each landing on main would start a session for each waiting fix, read or not. The diff the developer
  is reading changes under them.
- **Reason for Rejection**: Most landings touch no waiting fix. The landing is the one moment the work is needed.
