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
- The audit reads what changed since the developer accepted, before it lands.
- After the landing, the developer can read which files changed since they accepted, in the commit that moved main.
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

None remain.

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
- **Then**: kiln resumes the session that made the fix, with the gate's report, in a worktree that holds main's
  change. After the gate passes, main holds the session's work.

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

### 8. A fix whose session leaves nothing to land is kept

- **Given**: a fix as in test 1, whose session merges main by taking main's side of each conflict.
- **When**: the developer runs `kiln accept` with the monitor's name.
- **Then**: `kiln accept` fails, and main is unchanged. `kiln status` shows the landing stopped, since the work
  changes nothing on main. The branch is still there, and both `kiln accept` and `kiln reject` take the fix.

## Technical Plan

The change is in `internal/kiln`. No command, flag or key of `kiln.json` is new, and `kiln init` writes nothing
new.

### The landing

`landFix`, in `fix.go`, lands an accepted fix. Today each setback of `land` stops it. `fixLand` lands a fix that
never waited, and sends each setback back to the fix's session. After this spec one path serves both:

| Setback of `land`                  | The fix has a session           | No session      |
|------------------------------------|---------------------------------|-----------------|
| a conflict with main               | back to the session, as a round | stops, as today |
| the gate fails on the rebased work | back to the session, as a round | stops, as today |
| the work changes nothing on main   | stops                           | stops, as today |

- A fix has a session when its record's `Session` is set: the monitor's agent ran.
- "Back to the session" is what `fixLand` does today. `build.round` counts the round and words the message, and
  `fixLaunch` resumes the session. Then the fix goes on as `fixAgent` runs it: the session's turn, a commit of
  what it left, the gate with the monitor's check, and the landing again. Share that loop, rather than copy it.
- Main never gains a merge commit. A merge of main is a commit on the fix's branch alone, the session's or kiln's.
  `land` then puts the branch's whole change on main's tip as one commit with one parent, as `Git.Rebase` makes
  it for each landing today.
- The messages are today's: `conflicts`, in `build.go`, names each file in conflict and asks for
  `git merge main`, and `gateFailed` holds the gate's report.
- Before a failed landing gate goes back, kiln merges main into the branch, with `Git.Merge`. `land` puts the
  branch back where it stood before the rebase, which is on main as it was. Without the merge, the session and its
  next gate would never see what main gained, and only the landing's gate would fail, round after round. A merge
  that conflicts goes back as a conflict instead.
- Before it lands, `landFix` commits what the worktree holds, as `fixGate` does. A stopped landing tells the
  developer to resume the session there, and `land` would otherwise reset what that session left uncommitted.
  Work that so differs from the commit last read, as "What changed since kiln accept" defines it, goes to the gate
  and the audit first, never straight to `land`.
- The record marks the session's turn as running while it runs in a landing, as `Running` does for a split build.
  An interrupt, a killed kiln or a usage limit leaves the mark. A landing that starts with the mark first resumes
  the session with `interrupted`, as `goOn` does for a build, before it commits or lands anything. So no
  half-done merge is committed as the session's work.
- A landing keeps the title of the fix the developer accepted. `fixAgent` reads `titleOf` only before the
  accept, so a turn that merges main never retitles the commit.
- The kiln that lands the fix runs the session: `kiln accept`, or the next `kiln monitor run` for an accept that
  came while another kiln held the run lock. `Repo.Accept` already runs a build's session so, for accepted
  amendments. A landing that `kiln monitor run` takes up opens its tab when it ends, as today.
- The rounds are `kiln.json`'s, as the fix's last gate read them into the record's `Limit`. Each `kiln accept`
  starts them at 0. `book.accept` rebuilds the record of a stopped landing, and drops `Limit` and `Title` today.
  It keeps both, and the commit the developer accepted. Without `Limit`, a second `kiln accept` that conflicts
  would stop before its first round.
- Once the session's work passes the gate, it lands. `fixAfter` never leaves an accepted fix waiting again, as it
  leaves each fix of a monitor with `"accept": true` today.

### What never drops an accepted fix

`fixEnded` drops the record, the branch and the worktree of each fix that ends other than stopped or waiting. In
a landing, each of these stops the landing instead, with its text as why:

- the session asks for the developer, with `{"ask": true, "summary": "..."}`: its summary and its report
- the session fails: its error
- a usage limit ends the session: `landingStops` already stops the landing for it
- the work changes nothing on main, whether `fixAfter` or `land` finds it: it never goes back to the session

A stopped landing's record is in the state stopped, with `Accepted` set, as today. `kiln monitor run` lands only
a queued landing, so it never resumes a stopped one. `fixHeld` holds the monitor from a new fix until the
developer runs `kiln accept` or `kiln reject`.

`contract/monitor.md` says of an ask that "kiln then drops what you changed". Add that in the landing of a fix
the developer accepted, kiln keeps the fix, and the developer reads the report. Add to "How the fix goes" that such
a fix comes back when main moved before it landed, with the files in conflict or the gate's report.

### What changed since kiln accept

The record gains the commit the developer accepted: the branch's tip when `acceptFix` ran.

- A file changed since `kiln accept` when its own change differs between that commit and the work that lands.
  `ownChange`, in `reaudit.go`, reads a work's own change since it left main, with no line of context, so main's
  changes never count. It compares a binary file by its content too, with `git diff --binary`.
- The landing commit's body gains one line when that list holds a file, before the `Build:` line that ends each
  landing commit: `Changed since kiln accept: <path>, <path>`.
- The audit is decided by what the work holds, not by what happened. The record's `Audited` is the commit last
  read. `acceptFix` sets it to the commit the developer accepted. Each time a landing's work passes the gate, the
  audit reads it when its own change differs from that commit's, in a file outside `.kiln/`. `Audited` then
  moves to the work read.
- One change moves `Audited` with no audit: the session's answer to the audit's findings, once it passes the
  gate. The guide says not to audit the fix for an audit finding. `build.findings` keeps its rule for a path
  `kiln.json`'s `reaudit` names, which is read again even then, as a round.
- So each other change is read before it lands: a second merge after main moved during the landing, a turn a
  usage limit cut short, and what the developer's own session left in the worktree.
- The audit runs through `build.audit`, over the files changed since `kiln accept` and their directories, and
  its prompt names them. It spends no round, as a fix's first audit spends none. Its findings go back to the
  session with `fixAudited`.
- A landing whose work is the commit the developer accepted runs no audit, and its commit gains no line.

### What the developer sees

- `kiln accept <name>` prints each step as a fix's run does, and `<name>: landed` last. When the landing stops,
  it exits non-zero, and its error names `kiln accept <name>` and `kiln reject <name>`.
- While the session runs, `kiln status` shows the monitor's line as `<name>: accepted, landing: ` then why the
  session runs, in the words `reason.shown` gives `kiln tui`: `conflict with main, round 1 of 3`, or
  `landing gate failed, round 1 of 3`. `kiln tui` shows the run so today, from its start line in the ledger.
- A stopped landing shows in `kiln status`, under the monitor's line, why it stopped and the next step.
  `kiln tui` shows the same summary and report, from the fix's stopped outcome in the ledger, as it shows any
  stopped fix today.
- For the stopped landing of an accepted fix with a session, `fixStep` gives the step `nextStep`, in
  `stats.go`, gives a stopped build with a session: the command that resumes the session in its worktree, then
  `kiln accept <name>` or `kiln reject <name>`. A stopped fix the developer never accepted keeps today's step,
  since the book refuses both commands for it. `fixStep` and `landAgain` name `kiln reject` alone only for a
  conflicted fix with no session.
- An acceptance test reads a session's calls from the scripted claude, as `go doc ./internal/scenariotest` says.
  A session is an attempt, and each resume is its next call, so the fix's resumed session is
  `.claude-calls/<role>/1/2/`. Its `call.json` holds `"resume": true`, and its `prompt` file the message. Each
  audit is an attempt of the role `audit`. The harness needs no change.

### Tests and docs the plan changes

- `uat/testdata/monitors/waits-for-accept.txtar` stays true: its fix is a command's, with no session.
- `TestFixStep`, in `rules_test.go`: its conflicted row is a fix with no session. Add the row with one, and
  run its loop over each state with a session too.
- The package doc of `internal/kiln`. The fix's diagram gains a line under "landing an accepted fix":
  "a conflict, or a failed gate, with a session ──▶ back to the session, as a round". Under Monitors, three
  sentences gain the landing's rule. "A fix that passes lands, or waits for kiln accept": a setback of an accepted
  fix's landing goes back to its session. "ends the fix needs you", "session failed" and "A usage limit ends it at
  once": in a landing, each stops it, and kiln keeps the fix. "Only a fix whose agent ran gets the audit": a
  landing's audit reads what changed since `kiln accept`. The ledger's section names the commit's new line.
- `Repo.Accept`'s doc, which says a conflicted landing waits for `kiln reject`: that holds for a fix with no
  session alone.
- `docs/reference.md`, under monitors: "kiln then drops what it changed" gains the landing's exception. A new
  bullet says what `kiln accept` does once main moved past a waiting fix, and names the commit's line.
- `docs/practices.md`, "Acceptance tests": "Only the `uat` monitor folds or merges, for the developer to accept".
  Add that what its session folds while an accepted fix lands, after main moved, lands with that fix: the audit
  reads it, and the commit's `Changed since kiln accept:` line names it. `CLAUDE.md`'s "Folded scenarios" says
  "for the developer to accept" too, and gains the same.

### Risks the plan accepts

- Work the developer never read lands. After a failed gate, the uat session may fold scenarios beyond those the
  developer accepted. The gate-time session may change the tests that judge its work, since its monitor says
  `"tests": true`. The audit reads them, and the commit names their files. Waiting for `kiln accept` again
  would make it impossible, at the cost of a second accept for each landing main moved past.
- A developer who resumes the stopped session, and leaves it open while `kiln accept` runs, gives that session two
  writers. A stopped build's step has the same risk today. `claude --resume <session> --fork-session` in the step
  would make it impossible. It would change each stopped build's step, its docs and its tests, and kiln's session
  would no longer hold what the developer told it.

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

### The gate alone judges what the session changed

- **Pros**: No second audit, and no round spent on one. A build's merge of main lands so today.
- **Cons**: A merge that drops one side's line can compile, and pass each test.
- **Reason for Rejection**: The developer chose the audit on 2026-11-18. The session's work lands before the
  developer reads it, so the audit is its only reader before main moves.

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
