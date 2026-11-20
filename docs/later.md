# Later: what spec 1 leaves out

Status: notes, not a spec. Each topic below was discussed while drafting spec 1 (`git show d54b9a0718f9:specs/kiln.md`),
then left out. Each says what it is, why it came up, and what would show it is needed. A later spec starts from here.

A topic the developer means to build leaves this file for a starter spec in `.kiln/specs/`, with Status Proposed,
which `/shape` takes up from its Open Questions. The specs tui-reading, notify, project-roles, gauges, monitors and
languages began here.

## The evidence behind spec 1

Counted from Eidos's backlog and runner stores on 2026-11-07.

- **Parks.** 122 blocks in all. Of the 28 with options, 24 were plumbing. Eleven were a gate failing on a test the
  change didn't touch, most of them from one flaky scenario, S195. Eight were agent-written files contradicting each
  other. Five were git steps or fix rounds. Four were about the product.
- **Answers.** The developer took the agent's lean on all 5 questions they answered, with no words added. All 4
  amendments that waited on the developer were accepted.
- **Session size.** 50 implementer sessions averaged 73k tokens of context at their end, and the largest reached 250k.
  None ended for lack of context. Each built one ticket, so none tells whether one session can build a whole spec.
- **Scenarios that broke.** S65 locked a hash length of "7 to 40" that a later decision set to 7 to 64. S195 locked a
  5-second window. S136 locked which links Home draws. None of these was in the spec's intent.

## Flaky tests in the gate

Spec 1's acceptance test 4 lets a build land past a test that already fails on main. The fuller design:

- The gate compares each failure with main. A test that fails, or fails and then passes, on main isn't the build's
  doing.
- A test stable on main that fails on the build fails the gate, even if it passes on a rerun. Today's `suite_again`
  passes the gate on a rerun; that hides a race the build added.
- A quarantine list holds tests known to flake, and a session is started to fix each.
- **Build it when:** a flaky test blocks a build, or the flaky-test gauge in `.kiln/specs/gauges.md` rises.

## Several builds per spec

Spec 1 runs one session per spec. If a spec is too large for one:

- A planner splits the spec into builds, each named by the acceptance tests it makes pass.
- The gate then holds the tests each build turns green, plus everything already on main.
- Two builds that touch the same files don't run together. This is Eidos's runner rule "Runs that meet never run
  together".
- The audit reads the packages a build changed, so its cost grows with the packages touched, not the spec.
- **Build it when:** spec 1's report shows a session ran out of context, or lost track, on a whole spec.

## Telling an assertion from a crash

A test that fails because its Then didn't hold differs from one that crashed, timed out or couldn't start. The first
may mean the spec can't be met. The second is the harness. The draft proposed an exit-code contract: 0 passes, 1 is
the Then failing, anything else is the harness.

- **Build it when:** a build mistakes one for the other.

## Limits on amendments

Spec 1 lets a build amend a Given, When, Then and finish the work with the amendment. The developer reviews it at UAT.
Two guards were proposed against a build weakening a sentence until it is already true:

- An amended acceptance test must still fail on main, before the build's code.
- One amendment per Given, When, Then; a second need stops the spec.
- **Build it when:** UAT shows an amendment that weakened the intent.

## Who may write what

Spec 1 refuses a build's write to `specs/`. A finer table was drafted:

| Path                     | Interview            | Build | Repair |
|--------------------------|----------------------|-------|--------|
| `specs/`                 | Yes, before approval | No    | No     |
| Its own spec's test code | No                   | Yes   | Yes    |
| Another spec's test code | No                   | No    | Yes    |
| `.claude/settings*`      | No                   | No    | No     |

- **Build it when:** one build changes the test code under another spec's acceptance tests to pass its gate.

## Two designers and a judge

odh has two agents design apart from the same brief, and a fresh judge scores both by the five measures in
the guide. Spec 1 leaves this to the builder: it may spawn its own subagents.

- **Open:** each designer needs its own checkout, and a session can't write outside its checkout. Spec 10, split,
  solved it for the verify session: kiln makes each session a clone of main, and denies it reads of the other's. A
  designer can reuse that clone and its read denials.
- **Build it when:** a builder wants a design for a shared API or a critical piece of code, and can't get one inside
  its checkout.

## Progress while a build runs

`kiln status` shows a spec's state, not how many of its acceptance tests pass yet. A count was drafted, read from each
gate's results.

- **Build it when:** the developer wants to see how far a long build has got.

## odh practices not yet in spec 1

- **`prose`** on each comment, in the session that writes it.

## What Eidos leaves behind

The backlog (9,174 lines of code), the runner's stage machine (13,271), the viewer (11,432) and `internal/archtest`
(4,197) are built around tickets. They stay in Eidos at `07992d10` for reference. The runner's design,
`docs/design/runner.md`, holds the rules for landing, usage limits and the lines no session crosses. A later spec can
lift a rule from it by name.

## An audit of the fixes

The audit reads a build's work once, and its findings go back to the build. Nothing but the gate reads the fixes.
Spec 10, split, took up a part: the audit reads the work again when it changed a path `kiln.json`'s `reaudit`
names, otherwise than the audit read it. The rest of the fixes the gate alone reads.
Spec 7 weighed an audit that runs again on the fixes, up to a count in `kiln.json`. Through 2026-11-10, every audit
found 2 to 5 problems, each build said it fixed them all, and the one fix that broke a test, the gate caught. Each
audit cost $3 to $9.

- **Build it when:** a fix lands a defect that a second audit would have caught.

## A crossing that runs a monitor early

`.kiln/specs/measures.md` lets a monitor name a gauge. The monitor reads that gauge over the whole repository when
its interval has passed, and a crossed reading starts a fix. A landing's own reading of the gauge starts nothing. A
third trigger was weighed: a monitor that names a gauge would be due at the next `kiln monitor run` once a landing's
reading of it crossed.

- **Why it was left out:** the developer sets a monitor's schedule, and the monitor's run finds what a landing left.
  On 2026-11-14, 12 Go files scored below 10, so most landings would cross, and each would start a fix within the
  hour.
- **Build it when:** a crossing waits longer for its monitor than the developer wants.

## A project's own workflow

Spec 7 lets a project set each role's prompt, tools, model and effort. Spec 10 added a second fixed workflow, split,
whose verify role writes the tests apart from the build. Spec 11's monitors gave a project roles of its own: a
monitor's agent is one, named by its file under `.kiln/agents/`, in a workflow of one role with the audit. A project
could name a workflow of several roles, their order, and which role a role's findings go back to, such as a triage
agent that hands to a separate fix agent.

- **Build it when:** a project needs a workflow of several roles of its own, or another order of build, verify and
  audit.

## A gate's kind in the ledger

The `gate-time` monitor compares the upper quartile of the last 20 passing gates' times with the 20 before. A gate
that also runs main's checks, to excuse a test that fails at main too, takes about twice as long. The ledger's entry
for a gate does not say whether it ran them, so a run of such gates reads as a slower suite.

- **Why it was left out:** on 2026-11-18 the check read +5% against a +50% line, so the noise stayed far below it.
- **Build it when:** the monitor fires on gates that ran main's checks, rather than on slower tests.

## `kiln tui` on a light terminal

The view asks the terminal for its background, and draws the sheet's colors darkened on a light one. Three
refinements came up on 2026-11-18:

- A glyph beside the selected row. The row is marked by a tint alone, which is faint on some light terminals.
- `COLORFGBG` as a fallback. A terminal that never answers the question about its background is drawn dark.
- A heavier border on the focused panel, which reads lighter on white than on black.

- **Build it when:** the developer, or a user, works in a light terminal and finds the selection or focus hard to
  see, or the view draws dark on a light terminal.

## The one-page sheet's renderer

Two changes to `onepage`, seen on `docs/one-page.html`:

- A detail's tag, such as Detail A, is drawn as a circle, which elsewhere marks a part. A rounded square would tell
  them apart.
- The parts list reads as a table. It could read as a list of parts, each with its why.

- **Build it when:** the sheet changes again, or another repository's sheet hits the same confusion.

## Two builds, or two fixes, at once

Spec 15, lanes, lets a spec's build and a monitor's fix run side by side, each in a lane that one kiln at a time
holds. Two builds in the specs' lane, or two fixes in the monitors' lane, were left out. Each would need its own
lane, its own recovery, and a gate that waits for both.

- **Build it when:** a spec waits for another spec's build long enough that the developer runs it by hand, or a
  monitor waits for another monitor's fix to end.

## A share of the usage limit for each lane

Both lanes draw on one account's usage limit, so they reach it sooner than either alone. A build then pauses until
the limit resets, and a fix ends at once. Each lane could keep a share of the limit instead.

- **Why it was left out:** on 2026-11-19 the ledger held no build and no fix that a usage limit ended.
- **Build it when:** the ledger shows a build paused, or a fix ended, by a usage limit while the other lane worked.

## A conflict with the other lane that costs no round

A landing's conflict with main goes back to the session that made the work, as a round. With two lanes, the
monitors' landings can conflict with a build's more often, and spend its rounds. A count of conflicts apart from
the rounds would end that, at the cost of a second limit, with its own test and its own line in each doc that names
the rounds.

- **Why it was left out:** the developer chose the round on 2026-11-19. A merge of main is a turn of the session, as
  a failed gate's fix is.
- **Build it when:** a build stops out of rounds that conflicts with the other lane's landings spent.

## A ceiling on what one build or fix spends

kiln.json's `timeout` ends a hang: a command that never returns, or a session that falls silent. A session that works
on, turn after turn, is no hang. A ceiling on one build's or one fix's cost, turns or time would stop it, and say
which ceiling it met.

- **Why it was left out:** on 2026-11-20 the ledger held 65 builds and fixes that ran a session, over 192 session
  runs. The median spent $1.67. The costliest spent $31 in 291 turns, over 7 runs. The longest single run took 161
  turns, and the costliest $20. No run had ended at a usage limit. The rounds already cap how many runs a build
  makes. A session draws on the developer's plan and never bills overage, so the plan's usage limit caps what all
  the work spends. kiln pauses a build there, and ends a fix.
- **Build it when:** a build or fix spends more than twice the costliest so far, or a single run passes 500 turns. Or
  a usage limit pauses a build while one session drew most of the window.
