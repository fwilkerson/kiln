# Spec 2: Build stats

Status: Approved, 2026-11-08

## Overview

kiln keeps a ledger of every build it runs, and the developer reads it at three depths:

```text
kiln stats                one line per build, oldest first: the trend
kiln stats <name>         each build of one spec, by role and session, with its waits and failures by cause
the session's stream      .git/kiln/sessions/<id>/stream.jsonl, which kiln already keeps, for why
```

The hand run, as `kiln stats <name>` would show it:

```text
spec 1, hand run     size: 10 acceptance tests · landed
time:    42 min · 1.3 min waiting on tools · 0 min in gates · 6.5 min in subagents · never paused
build:   1 run · 201 turns · $18.77 · context 25k → 457k peak → 457k · 0 compactions · 7-day limit 13% → 14%
tools:   135 Bash · 51 Read · 50 Write · 12 Edit · 1 Agent
slowest: 6s  go test ./acceptance/kiln/ -run 'TestAT3$' -v ...
failed:  33 calls
  11  a sentence in this comment has more than 20 words / the line is wider than 120 columns
   4  refused: A variable in this command can't be checked before it runs
   4  refused: This Bash command contains multiple operations ...
  ...
```

What showed the need, per `docs/later.md` "Build stats": the developer wants a build's numbers beside the hand run's,
and wants to tell whether a rise comes from the codebase's health or from a larger spec. The hand run never ran under
kiln, so it is not in the ledger. Its numbers stay in `docs/later.md`, and a unit test reproduces them from its stream.

### Terms

- **Build**: everything kiln does for one approval of a spec, from `kiln approve` until it lands, stops, or a new
  approval drops it. Each approval gets its own build ID. Its cost is the cost of all its sessions.
- **Session**: one Claude Code conversation, with its own ID. A build has one build session and one audit session per
  audit try.
- **Role**: the kind of session, as `claude.Launch.Role` names it: `build` or `audit`.
- **Session run**: one start or resume of a session. kiln resumes a build's session with `claude --resume <id>` after
  a failed gate, a conflict with main, the audit's findings, a usage limit, or kiln itself being interrupted.
- **Gate**: the commands in `kiln.json` that kiln runs on a build's commit, outside any session.
- **Waiting on tools**: time in which at least one of a session's tool calls had not yet returned, as the stream's
  timestamps show, not counting subagents.
- **Ledger**: `.git/kiln/ledger.jsonl`, kiln's record of every build, session run and gate.
- **Size**: the number of acceptance tests in the spec the build was approved at.
- **Cause**: what a failed call is grouped by: a refusal's reason, or an error's first line of output.

## Goals

- For each build: its time, the part of it spent waiting on tools, in subagents and in gates, and whether a usage
  limit paused it; turns; cost; context at the start, peak and end; compactions; 7-day use; tool calls by tool; the
  slowest calls; and failed calls by cause. Each is split by role and by session.
- A build's size beside its numbers, so a rise can be read against a larger spec.
- Every build kiln ran, however it ended, stays in the ledger after the developer accepts its spec.
- Each failure keeps enough to act on without opening the stream: its tool, its input's first line, and its cause. A
  refused call is how the developer finds a permission to add, or a hook to write that tells the agent what to do
  instead.
- A landed build's commit carries its summary, so `git log` shows what each landed build cost.
- A number kiln could not read is shown as unknown, never as zero.

## Non-goals

- Lines a number may not cross, and alarms: `docs/later.md` "Gauges".
- An interactive view of the ledger: spec 3, `specs/stats-tui.md`.
- Counts while a build runs: `docs/later.md` "Progress while a build runs".
- How long a usage limit paused a build. `kiln stats` says that it paused, not for how long.
- Deleting old session files. kiln keeps them all today, and this spec changes nothing there.

## Open Questions

None.

## Acceptance tests

### 1. A build's stats sum all its sessions, by role

- **Given**: a build whose session failed the gate once, went back to the same session, passed, was audited, took the
  audit's findings in its session, and landed.
- **When**: the developer runs `kiln stats <name>`.
- **Then**: it shows the time, turns, cost, context, compactions, 7-day use, tool calls by tool, and failed calls, for
  each role and for the build as a whole.

### 2. Waiting is shown apart from working

- **Given**: a build whose session waited on a slow tool call, whose gate ran, and that a usage limit paused and
  resumed.
- **When**: the developer runs `kiln stats <name>`.
- **Then**: it shows the time spent waiting on tools and the time spent in gates, apart from the rest of the build's
  time, names the slowest call, and says the build paused for a usage limit.

### 3. Failed calls are grouped by cause

- **Given**: a build whose session had two calls refused for the same reason, and two that failed with the same
  message in their output.
- **When**: the developer runs `kiln stats <name>`.
- **Then**: it shows each cause once, with its count, and the input of a call it holds.

### 4. Stats outlive the spec

- **Given**: four builds: one whose spec the developer accepted; one that stopped after its last round; one that
  waited on its branch with amendments until its spec was approved again, which dropped it; and the build of that new
  approval, which landed.
- **When**: the developer runs `kiln stats`.
- **Then**: it shows one line for each of the four builds, in the order they started, each with its size and how it
  ended.

### 5. A landed build's summary is in its commit

- **Given**: a build that landed.
- **When**: the developer reads the landing commit's message.
- **Then**: it holds the build's time, cost, turns and failed calls.

### 6. A run that wrote no result is not shown as free

- **Given**: a build whose audit session ended with no result, and whose next audit try answered.
- **When**: the developer runs `kiln stats <name>`.
- **Then**: it counts both audit runs, and shows the build's cost as unknown for the run with no result, not as zero.

## Technical Plan

- **The build ID.** `book.approve` (`internal/kiln/state.go`) gives each approval a new random ID, kept in `record` as
  `Build` and written on every ledger line. A spec approved twice at the same main tip is two builds.
- **The ledger.** `.git/kiln/ledger.jsonl`, appended to and never rewritten. Its lines:

  | Line      | Written                                                                                        | Holds                                                                                          |
  |-----------|------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------|
  | build     | when `kiln run` starts the build                                                               | build ID, spec, `record.Approved`, size, start                                                 |
  | run start | before each session run                                                                        | build ID, role, session ID, the stream's length in bytes, start                                |
  | run end   | after each session run                                                                         | build ID, session ID, end, how it ended (a result, no result, a usage limit), the run's totals |
  | gate      | after each gate                                                                                | build ID, start, end, passed                                                                   |
  | outcome   | when the build lands, stops, waits on its branch, is dropped by `kiln approve`, or is accepted | build ID, outcome                                                                              |

  A queued build has no line until `kiln run` starts it. Each line names its session, so the totals can be counted
  again from the streams if the counting rules change. The ledger lives and dies with the clone, as the streams do.
- **A run with no end line.** kiln can die mid-run, as when a closed terminal sends SIGHUP, which `cmd/kiln` does not
  catch. Before it starts or resumes a session, `kiln run` totals any run of it that has a start line but no end line,
  from the stream past the start line's byte length, and appends its end line, ended with no result.
- **A build's size.** The count of numbered acceptance tests in `specs/<name>.md` at `record.Approved`, read with
  `git.Show` as `Repo.Approve` already does.
- **Totaling a run.** A function in `internal/claude` totals the lines one run printed. `Run` already reads each line
  as it passes (`reading.line`, `internal/claude/claude.go`). Each number's source:

  | Number           | Source                                                                                                                                             |
  |------------------|----------------------------------------------------------------------------------------------------------------------------------------------------|
  | time             | the run's start and end in the ledger, so a run with no result still has a time                                                                    |
  | turns            | the run's `result` event: `num_turns`; unknown with no result                                                                                      |
  | cost             | the run's `result` event: the sum of `costUSD` over its `model_usage_since_last_result`; unknown with no result                                    |
  | 7-day use        | `rate_limit_info.unifiedWindows.seven_day.utilization`, of the first and last `rate_limit_event` that holds a `seven_day` entry; unknown with none |
  | context          | each top-level assistant message, by `message.id`: input, cache-read and cache-creation tokens, summed                                             |
  | compactions      | `system` events of subtype `compact_boundary`                                                                                                      |
  | tool calls       | each `tool_use` block, by its `id`, the subagents' included, as the hand run counted                                                               |
  | waiting on tools | the union of the intervals from each top-level `tool_use`'s event `timestamp` to its `tool_result`'s, `Agent` calls apart as time in subagents     |
  | slowest calls    | the same intervals, each on its own, longest first                                                                                                 |
  | refusals         | `system` events of subtype `permission_denied`: `tool_name`, `tool_use_id`, `message`                                                              |
  | errors           | `tool_result` blocks with `is_error`, less the refusals, joined to their `tool_use` by its ID                                                      |

  Assistant events repeat a message once per content block, hence counting by `message.id` and `tool_use.id`. Each
  source but compactions was checked against the hand run's stream; the hand run never compacted, so no real stream
  has shown a `compact_boundary` event yet. The hand run's top-level calls never overlapped, so their union equals
  their sum, 78 seconds, 40 of them in its 17 calls that ran `go test`; its one subagent ran 390 seconds.
- **Resumes.** A resume appends to the same stream, and ends in its own `result` event. Measured on 2026-11-08 with
  one Haiku session and one resume of it: `num_turns` and `duration_ms` count the run alone (1 and 1, 1850 ms then
  821 ms), but `total_cost_usd` counts from the session's start ($0.00286, then $0.00311). Hence a run's cost comes
  from `model_usage_since_last_result` ($0.00024 for the resume).
- **Combining runs.** Turns, cost, time, waits, tool calls, compactions and failed calls add up across runs, roles and
  the build. A build's time is its session runs' time plus its gates' time; a pause adds nothing, since no session or
  gate runs. Context is per session: its first message, its peak, and its last message. 7-day use is the build's
  earliest and latest reading. A sum that holds an unknown shows as at least the known part, with the count of runs
  it lacks.
- **Causes.** A refusal's cause is its `message`. An error's cause is the first line of its output that isn't an exit
  status, such as `Exit code 1` or `exit status 1`, or a Go package header such as `# github.com/...`, with any
  leading `path:line:` or `path:line:col:` dropped. Some errors aren't failures: a `grep` that matches nothing exits 1.
  Those stay as causes of one call each. Each cause keeps the first line of one call's input. The streams stay, so a
  later spec can refine causes against real failures.
- **The commands.** `kiln stats` and `kiln stats <name>` in `cmd/kiln`, beside `kiln status`. Both read the ledger
  only, and print plain text for a terminal. `kiln stats` prints, per build: when it started, its spec, its outcome,
  its size, its time, its waits and gate time, its cost and cost per acceptance test, its turns, and its failed calls.
  `kiln stats <name>` prints each build of that spec, oldest first, as the Overview shows.
- **The commit's summary.** The landing commit's body (`build.message`, `internal/kiln/build.go`) gains the build's
  time, cost, turns and failed calls, read from the ledger, beside the session ID it holds today. `kiln stats` never
  reads it.
- **The audit's sessions.** `build.audit` (`internal/kiln/build.go`) makes a new session for each try and records none
  of them today. All session runs go through `build.talk`, which writes their ledger lines.
- **The scripted claude.** It already writes a `timestamp` on each assistant and user event, and usage on each
  assistant event (`internal/scenariotest/claude_tool.go`). `ClaudeResult` gains cost, a scripted step may set its
  usage, and a refused call writes a `permission_denied` event, as Claude Code does.
- **A real stream as a fixture.** `.hand-run/` is the untracked directory where the hand run left its stream. A unit
  test in `internal/claude` totals a trimmed copy of it in `internal/claude/testdata/`, with each event's text and
  thinking dropped, and gets the hand run's numbers. The scripted claude prints only what a test scripts, so this is
  the one check against what Claude Code really prints.

## Alternatives Considered

### The landing commit as the only record

- **Pros**: no new store; `git log` is the trend, and it travels with the repository.
- **Cons**: a stopped build and a dropped build never land, and kiln's gate times are in no stream.
- **Reason for Rejection**: the builds that struggled most would be missing from the trend.

### Total the streams when the developer asks, with no ledger

- **Pros**: no new file; the streams are already kept.
- **Cons**: the gate times, and which build a session belonged to, are in no stream.
- **Reason for Rejection**: kiln must write down what only it knows, and then the totals cost one line more.

### Name a build by its spec and the main tip it was approved at

- **Pros**: no new ID; `record.Approved` already exists.
- **Cons**: a stopped build approved again before main moves has the same tip, and its runs merge into the old build.
- **Reason for Rejection**: approving again is how the developer retries a stopped build.

### Size as the lines a build changed

- **Pros**: measures the work, not only the intent.
- **Cons**: measures what the build did, not how large the spec was, and a dropped build's branch is gone before it
  can be counted.
- **Reason for Rejection**: the question is whether a larger spec explains a rise.

### A rule per cause in `kiln.json`

- **Pros**: names a cause in the developer's words, and can say what to do about it.
- **Cons**: names only the causes the developer has already written down.
- **Reason for Rejection**: grouping by the output's first line names the hand run's style-checker failures with no
  rule, and the kept streams show whether it is enough.

### Group the trend by week

- **Pros**: smooths one large spec out of the trend.
- **Cons**: hides which spec moved a number.
- **Reason for Rejection**: one line per build, with its size, keeps both readable.
