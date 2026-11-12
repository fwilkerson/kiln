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

## Repairs

A failure that isn't the build's doing still needs fixing: a broken script, a test harness, a setting.

- A repair is a session with no Given, When, Then. Its goal is that one failing command passes.
- It may change tests, scripts and the code under other specs' acceptance tests, never a Given, When, Then.
- A failure that needs a settings change, a login or a tool is outside the repository, and reaches the developer as
  the exact action.
- **Build it when:** a build stalls on a failure it didn't cause.

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
`docs/practices.md`. Spec 1 leaves this to the builder: it may spawn its own subagents.

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

## A project's own workflow

Spec 7 lets a project set each role's prompt, tools, model and effort. Spec 10 added a second fixed workflow, split,
whose verify role writes the tests apart from the build. A project could name its own roles, their order, and which
role a role's findings go back to.

- **Build it when:** a project needs a role other than build, verify and audit, or another order of them.
