# Later: what spec 1 leaves out

Status: notes, not a spec. Each topic below was discussed while drafting spec 1 (`specs/kiln.md`), then left out. Each
says what it is, why it came up, and what would show it is needed. A later spec starts from here.

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

## Gauges

A gauge is a number over time, with a line the developer sets. Gauges serve the developer's job of curating the
codebase.

| Gauge              | Source today                                                                                                                   |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------|
| Flaky tests        | Eidos's `scripts/gate.sh` `suite_again` notes each test that passed on a second run alone. A gauge counts those notes per gate |
| Slow tests         | Eidos's `scripts/health.py`: the slowest tests, suite CPU and wall time                                                        |
| Comment share      | odh's `tools/size`: comment lines per 100 lines, by directory                                                                  |
| Agent friction     | Eidos's runner record: refusals, context used before the first edit, failed gates per build                                    |
| Mutation score     | Eidos's `scripts/mutate.sh`                                                                                                    |
| GoLand inspections | odh's `tools/inspect`. It needs GoLand open on the machine, so it can't gate an unattended build                               |

- **Testable code.** The developer cares that a build writes testable code, not in what order. Mutation score is the
  lean for its gauge: it falls when tests can't catch a change, and extra tests that catch nothing don't raise it.
  Coverage would reward tests written for their own sake.
- **Build it when:** a suite has grown enough that flaky or slow tests appear, or the developer wants a trend to curate
  against.
- **Open:** where each line is set. The draft had a checked-in `gauges.json`.

## Build stats

What one build cost and where it struggled, so the developer can compare builds. kiln keeps each session's stream at
`.git/kiln/sessions/<id>/stream.jsonl`, but totals none of it. Spec 1's hand run, counted from its stream:

```text
hand run, spec 1:  42 min · 201 turns · $18.77 · 7-day limit 13% → 14%
context:           25k → 237k → 343k → 411k → 457k tokens, never compacted
tool calls:        135 Bash · 51 Read · 50 Write · 12 Edit · 1 audit subagent
failed calls:      33 = 16 permission refusals + 11 style-checker loops + 6 other
```

- Each row comes from the stream. The `result` event holds time, turns and cost. Each `rate_limit_event` holds the
  7-day use. Each assistant turn's usage holds its context. A failed call is a tool result marked an error, and a
  refusal is a `permission_denied` event.
- A build is several sessions: the build, its audit, and their resumes. Its stats sum them.
- The failed calls' causes were read from each call's output. A cause kiln can name needs a rule per cause.
- It overlaps the agent friction gauge under "Gauges".
- **Build it when:** the developer wants a build's numbers beside the hand run's.

## Build stats in a TUI

Spec 2 prints the ledger as plain text, one line per build. The developer may want an interactive view: drilling from
the trend into a build, a role and a failure, and charts of a number across builds.

- **Build it when:** plain text no longer shows the developer what they look for.

## Flaky tests in the gate

Spec 1's acceptance test 4 lets a build land past a test that already fails on main. The fuller design:

- The gate compares each failure with main. A test that fails, or fails and then passes, on main isn't the build's
  doing.
- A test stable on main that fails on the build fails the gate, even if it passes on a rerun. Today's `suite_again`
  passes the gate on a rerun; that hides a race the build added.
- A quarantine list holds tests known to flake, and a session is started to fix each.
- **Build it when:** a flaky test blocks a build, or the flaky-test gauge rises.

## Monitors

A monitor watches running software (tests, logs, metrics, traces) and hands what it finds to the engine.

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
       engine
```

- The store holds observations: signature, first and last seen, count, and state.
- No judge weighs findings in English. A failing test is the only way in.
- The developer approves no one-test spec. Its citation ties it to intent already approved.
- Cap how often one signature reaches the engine, so a partial fix can't loop.
- **Build it when:** a project the engine builds runs somewhere and produces logs or metrics.

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

- **Open:** each designer needs its own worktree, and a session can't write outside its checkout. The builder's
  subagents may need the engine to make those worktrees.
- **Build it when:** a builder wants a design for a shared API or a critical piece of code, and can't get one inside
  its checkout.

## Notifications

A notification says when a spec stops or waits for UAT, and should open what to act on.

- **Ghostty first.** The developer wants to try Ghostty's notifications. Ghostty is believed to show one when a
  program prints an OSC 9 or OSC 777 escape sequence; this is not tried.
- `terminal-notifier` can open a target on a click; `osascript`'s notification cannot. This is believed, not tried.

## A project's own tools, agents and workflows

kiln gives every session the same tools and permissions (`claude.Launch`'s `Tools`, `Deny`, `Forbidden`, `Writable`,
`Domains`, set in `internal/kiln/build.go`). A project may need more: spec 3's view needs tui-test's CLI to explore a
TUI, and auto mode refuses a program from outside until a rule allows it.

What a project can set today, and what it can't:

- **Permissions, per project: yes.** kiln's sessions read the project's `.claude/settings.json`
  (`--setting-sources project`, `internal/claude/claude.go`), so an allow rule there reaches every role.
- **Tools and prompt, per role: no.** kiln fixes each role's tools in its own code (`internal/kiln/build.go`, the
  audit's `Tools`) and compiles the audit's prompt into its binary (`guidance.Audit`). The `tools:` line in
  `guidance/agents/audit.md` is never read.

The options, from least to most:

- A project names the tools and permission rules each role gets, beside the gate in `kiln.json`.
- A project defines its own agents, and kiln runs `.claude/agents/<role>.md` from the project's main, prompt and
  `tools:` alike, falling back to its own. This is the lean: it is Claude Code's own format, so one file says what a
  role is told and what it may use, where a list in `kiln.json` would be a second place for the same fact.
- A project defines the workflow itself: which roles run, in what order, and what a failure sends back.
- **Build it when:** a build fails or stalls for a tool or a permission its project needs and kiln does not give.

## Repositories in other languages

Spec 1 names a test that already failed on main by its Go test name, and ties a Given, When, Then to its code by a Go
test's name. Another language needs its own way to name a test and to read which failed.

- **Build it when:** the developer points kiln at a project that isn't Go.

## Progress while a build runs

`kiln status` shows a spec's state, not how many of its acceptance tests pass yet. A count was drafted, read from each
gate's results.

- **Build it when:** the developer wants to see how far a long build has got.

## odh practices not yet in spec 1

- **Blind weigher.** When the interview's lean matches the developer's, a fresh agent weighs the options without
  knowing whose is whose.
- **Research reports** before a decision about the world, as in odh's `docs/research/`.
- **`prose`** on each comment, in the session that writes it.
- **`audit` scoring.** odh's audit gives at most five findings, each with a design or a patch, and a failing test where
  one can fail today.

## What Eidos leaves behind

The backlog (9,174 lines of code), the runner's stage machine (13,271), the viewer (11,432) and `internal/archtest`
(4,197) are built around tickets. They stay in Eidos at `07992d10` for reference. The runner's design,
`docs/design/runner.md`, holds the rules for landing, usage limits and the lines no session crosses. A later spec can
lift a rule from it by name.
