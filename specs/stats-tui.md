# Spec 3: A view of kiln's builds, live and past

Status: Approved, 2026-11-08

## Overview

The developer runs `kiln tui`, or `kiln tui <name>` to open on one spec, and gets one full-screen view of kiln. It
shows what the running session is doing now, every spec kiln has built and each build of it, how each build went run
by run, the spec as it was approved, and the trend of a few numbers across builds. It reads what `kiln run` writes,
which gains a few fields for it, and changes nothing; `kiln run` goes on in its own terminal.

```text
                 ┌──────────────── side panel: specs, newest first, / filters by name ────────────────┐
                 ▼                                                                                     │
home ──enter──▶ spec ──enter──▶ build ──enter──▶ run ──s──▶ spec text, as the build's approval held it │
  │                                                ▲                                                   │
  └──────────── the "now" line, enter ─────────────┘   esc goes back one screen at every step ─────────┘
```

A run that has not ended and one that ended are the same screen. The running one grows as its session writes.

What showed the need: the developer asked for this view. `docs/later.md` had deferred it until plain text no longer
showed what the developer looks for, which overstated the bar; that entry is gone. Plain text can't show what a
running session is doing: `kiln status` prints a spec's state, and `kiln stats` prints a run only once it ends. The
developer asked for the trend on the home screen too, before the ledger holds builds, so that it is there as the
builds come.

### Home

```text
╭ specs ─────────────────╮╭ now ─────────────────────────────────────────────────────────────────────╮
│ / filter               ││ build-stats · build role · round 1 of 3 · running 14m                    │
│                        ││ idle 0:42, in a call: go test ./...  ·  3 refused  ·  context 340k        │
│ ● build-stats  running ││                                                                          │
│ ◌ notifications queued │╰──────────────────────────────────────────────────────────────────────────╯
│ ◆ gate-flakes  UAT     │╭ last 10 builds ──────────── median ── oldest → newest ────────────────────╮
│ ✓ status-line  accepted││ cost per build             $14.20    ▃▄▂▅█▃▂▂▁▂   previous 10: $16.90    │
│ ✗ amendments   stopped ││ minutes per build              31    ▂▃▂▄▆▃▂▂▂▁   previous 10: 38        │
│                        ││ failed calls per build         14    ▅▆▄█▇▃▃▂▂▂   previous 10: 21        │
│                        ││ rounds per build              1.4    ▁▁▃▁█▁▁▃▁▁   previous 10: 1.6       │
│                        ││ size, in tests                  6    ▃▅▃▆█▅▄▃▃▄   previous 10: 7         │
│                        │╰──────────────────────────────────────────────────────────────────────────╯
│                        │╭ recent builds ───────────────────────────────────────────────────────────╮
│                        ││ Nov 08 14:02  build-stats    running   6 tests  14m     cost unknown     │
│                        ││ Nov 07 09:00  gate-flakes    landed    3 tests  14m     $6.10            │
│                        ││ Nov 06 10:12  notifications  stopped   5 tests  26m     $11.40           │
╰────────────────────────╯╰──────────────────────────────────────────────────────────────────────────╯
 tab panel · / filter · enter open · q quit
```

### A spec, and its builds

```text
gate-flakes · UAT · approved 3 times
                                                                      s spec as approved · enter build
  #  started       approved at  outcome    time   cost     turns  failed  rounds
  1  Nov 04 13:10  1a2b3c4      stopped    52m    $22.80     260      20       3
  2  Nov 05 08:30  9f8e7d6      dropped    12m     $4.10      44       3       1
  3  Nov 07 09:00  4c5d6e7      landed     14m     $6.10      71       4       1
```

### A build, run by run

```text
gate-flakes · build 3 · approved at 4c5d6e7 · 3 tests · landed
time 14m: 2m in gates, 3m waiting on tools · $6.10 · 71 turns · 4 failed, 1 refused

   run                              took   turns   cost    failed  context peak
1  build    first run                8m      48   $4.20         3          210k
   gate     ✗                        1m
2  build    round 1: gate failed     2m      12   $0.90         0          225k
   gate     ✓                        1m
3  audit    try 1                    2m       9   $0.70         0           64k
4  build    audit's findings         1m       2   $0.30         1          231k
   gate     ✓ landing                1m
   landed

by cause
✗  2  a sentence in this comment has more than 20 words      Write internal/gate/flaky.go
✗  1  refused: This Bash command contains multiple ...      go vet ./... && go test ./...
                                                               s spec as approved · enter run · esc back
```

### A run, live or past

```text
build-stats · build 1 · run 2: round 1, gate failed · build role · session 2105a756
running 14m12s · idle 0:42 · context 340k · 61 calls · 3 refused · cost when the run ends

  14:02:11  Read   internal/kiln/stats.go                    0.1s
  14:02:15  Edit   internal/kiln/stats.go                    0.2s
  14:02:30  Bash   go test ./internal/kiln/                  6.3s  ✗ --- FAIL: TestCollect
  14:03:02  said   The collect test fails because the gate line has no ...
  14:03:40  Bash   cd $dir && go test                           —  refused: A variable in this command ...
▶ 14:04:12  Bash   go test ./...                             0:38  running
```

### The spec as approved

The spec is read with `git show <approved>:specs/<name>.md`, so an accepted spec deleted from main shows as its build
saw it. Its amendment, where the build wrote one, follows it. It is drawn as Markdown with its code highlighted, and
scrolls.

### Terms

Spec 2's terms hold: build, session, role, session run, gate, ledger, size, cause. And:

- **Round**: as `kiln run` counts it (`record.Rounds`, `build.round`). Round N is the Nth time kiln sent the work back
  to the build session, after a failed gate, a conflict with main, or a failed gate on the rebased work, out of
  `kiln.json`'s `rounds`. `round 1 of 3` is what `kiln run` prints at the first send-back. The first run is in no
  round.
- **Idle**: the time since the running session last wrote to its stream. Beside it, the view says whether a tool
  call is open, since a long idle inside `go test` is a slow tool and a long idle outside one is a stuck model.
- **Why a run started**: one of the reasons below, as the ledger's start line names it. kiln knows each at the
  moment it starts the run.

  | `why`         | The run                                                           | Shown as            |
  |---------------|-------------------------------------------------------------------|---------------------|
  | `first`       | the build session's first run                                     | first run           |
  | `gate`        | the build session, after a failed gate; with `round` and `rounds` | round 1 of 3        |
  | `audit`       | an audit session's run; with `try`, which counts its tries from 1 | audit, try 1        |
  | `findings`    | the build session, with the audit's findings                      | audit's findings    |
  | `conflict`    | the same, after main moved and its work conflicts with it         | conflict with main  |
  | `landing`     | the same, after the gate on its rebased work failed               | landing gate failed |
  | `limit`       | the same session again, after a usage limit reset                 | after a usage limit |
  | `interrupted` | the build session, after kiln itself was stopped mid-build        | after kiln stopped  |

## Goals

- See what the running session is doing now: its spec, role, round, how long it has run, how long it has been idle,
  the call it is in, its refusals, and its calls so far.
- See each past build run by run: each run's role, why it started, its round, its time and numbers, and each gate
  between them.
- Group builds by spec, and open any build of a spec.
- Read the spec a build was approved at, with its amendment, even after the spec left main, drawn as Markdown with
  its code highlighted, and scrolled.
- On the home screen, a few numbers across the recent builds, each as a median and a trend: cost, minutes, failed
  calls and rounds per build, and size. Per build, not per acceptance test: part of each build's cost is fixed (the
  session's start, the audit, the gates), and dividing it by 3 tests weighs it three times as heavily as by 10, so a
  small spec would read as a worse build. Size beside them says whether a rise came from larger specs.
- Find a spec by typing part of its name.
- Color draws the developer's eye to what needs it: what failed, what is running or stuck, and a number that got
  worse. A glyph says the same beside each color, so nothing is told by color alone.
- The keyboard and the mouse both work: a click selects and opens a row, and the wheel or a touchpad scrolls.
- A number kiln could not read is shown as unknown, never as zero, as spec 2 holds.
- No agent opens the view by accident: it opens only by its own command, and refuses with a message when its output
  is not a terminal.
- The audit drives a TUI it reviews, as a developer would, and the developer sees in the ledger whether it changed
  anything. Give it the tools first, and take them back if the ledger shows it misuses them. Its Edit and Write
  counts would miss a file written by a shell command, so kiln compares the build's tree across each audit run.

## Non-goals

- Changing anything from the view: approving, accepting, stopping a build. The view only reads.
- Lines a number may not cross, and alarms: `docs/later.md` "Gauges".
- Numbers kiln's ledger does not hold, such as flaky tests or mutation score: `docs/later.md` "Gauges".
- Which tests failed a gate. The ledger holds only whether it passed.

## Open Questions

None.

## Acceptance tests

Each test drives `kiln tui` in a pseudo-terminal, by keys unless it says the mouse, and reads the screen.

### 1. The running session, live

- **Given**: a build whose session went back after a failed gate, has had a call refused, and is inside a tool call.
- **When**: the developer opens `kiln tui`, then the running run, and the session makes another call.
- **Then**: it shows the spec, the role, the round, how long the run has gone, how long since the session last wrote,
  the open call, and the refusal, with the run's cost as unknown. The new call appears with no key pressed.

### 2. A past build, run by run

- **Given**: a build whose session failed the gate once, went back to the same session, passed, was audited, took
  the audit's findings in its session, and landed.
- **When**: the developer opens that build.
- **Then**: it shows each run in order, with its role, why it started, its round, its time, turns, cost and failed
  calls, and each gate between the runs, passed or failed.

### 3. Builds are grouped by spec

- **Given**: two specs, one of them approved twice, so it has two builds.
- **When**: the developer opens the spec approved twice, then each of its builds.
- **Then**: the spec shows both builds, each with how it ended, and each build shows only its own runs.

### 4. The spec as its build saw it

- **Given**: a spec the developer accepted, which main no longer holds, and another spec waiting on its branch with an
  amendment.
- **When**: the developer opens each one's spec text, and scrolls to its end.
- **Then**: the first shows the text it was approved at. The second shows its text and its amendment.

### 5. The trend on the home screen

- **Given**: more builds than the home screen's recent window holds.
- **When**: the developer opens `kiln tui`.
- **Then**: it shows cost, minutes, failed calls and rounds per build, and size, each for the recent builds beside the
  builds before them.

### 6. Finding a spec by name

- **Given**: several specs.
- **When**: the developer types part of one spec's name into the filter.
- **Then**: the side panel holds only the specs whose names hold that text.

### 7. The mouse

- **Given**: a spec with a build long enough to scroll.
- **When**: the developer clicks the spec, then clicks its build, then scrolls the wheel.
- **Then**: each click opens what it clicked, and the wheel moves the build's view.

### 8. Color marks what failed

- **Given**: a build with a failed gate, a refused call and a passed gate.
- **When**: the developer opens that build.
- **Then**: the failed gate and the refusal are drawn in one color, and the passed gate in another, each beside a
  glyph that says the same.

### 9. No agent opens it by accident

- **Given**: `kiln tui`'s output is not a terminal.
- **When**: something runs `kiln tui`.
- **Then**: it draws nothing, says why, and exits with an error.

### 10. An audit that changed the tree shows it

- **Given**: a build whose audit wrote a file in the build's tree.
- **When**: the developer opens that build.
- **Then**: the audit's run is marked as having changed the tree, and names the file, and the file is in no commit
  of the build.

## Technical Plan

- **Library.** Bubble Tea v2 and Lip Gloss v2, with Bubbles' viewport for scrolling, and Glamour
  (`charm.land/glamour/v2`, v2.0.1) to draw the spec's Markdown with its code highlighted. A click finds its row
  through Lip Gloss layers, so the cells drawn and the cells clicked come from one layout. Each layer's ID has one
  constructor and one parser beside it, the weak point the research found (`docs/research/tui/report.md`).
- **Color.** Red for a failed call, a refusal, a failed gate, a stopped build, and a number worse than the previous
  10 builds' median. Yellow for what is running, an open call, a long idle, and a pause for a usage limit. Green for
  a passed gate, a landed or accepted build, and a number that got better. Dim for times, IDs and inputs. Each sits
  beside a glyph, `✗`, `▶`, `✓`, so the meaning survives `NO_COLOR`.
- **Checking the screen.** As the research recommends: `docs/research/tui/prototypes/termtest` moves to
  `internal/`, on `creack/pty` and `charmbracelet/x/vt`, and its `Find` fails on a text that is on screen twice.
  Each check waits for a fact its input causes. The prototypes are deleted once this spec lands.
- **The acceptance tests' setup.** Each stays a testscript file run by `scenariotest.Run`, on a scratch repository
  with the scripted claude, as spec 1's and spec 2's are. A script builds its history with `kiln approve` and `kiln
  run`, as theirs do. `scenariotest` gains testscript commands over `termtest`: one starts `kiln tui` at a size, one
  sends keys or a click, and one waits until the screen holds a pattern, or a cell's color. A live run (test 1) is
  `kiln run &` in the background, whose scripted session makes a Bash call that waits until the script creates a
  file. The scripted claude already runs a Bash call's command (`internal/scenariotest/claude_tool.go`), so the call
  stays open until the script says, and the next scripted step is the new call.
- **Reading only.** The view reads `.git/kiln/state.json` (`book`), the ledger, and each session's stream. It takes
  no lock and writes nothing. `store.entries` already skips a line cut short, so a line `kiln run` is writing as the
  view reads is skipped until the next read.
- **Live.** The view reads again once a second: the ledger and the book, and the running session's stream past the
  byte length its start line holds. Its counts so far come from `claude.Recount(session, start.Streamed)`, as `kiln
  run` counts a lost run. Cost and turns come from a run's `result` event, so a running run shows them as unknown
  until it ends, as spec 2's rule holds.
- **Which run is running.** The start line gains `pid`, the process ID of the `kiln run` that started it. A run with
  no end line is running only while that process lives (`kill(pid, 0)`); otherwise the view shows it as "kiln
  stopped mid-run", as when the terminal running `kiln run` was closed, until the next `kiln run` ends it with
  `recount`.
- **A run's bytes.** The end line gains `streamed`, the stream's length after the run, written by `talk` and by
  `recount`. Every run of a session appends to one stream, so a run is the bytes from its start line's length to its
  end line's, and a running run's reach to the stream's end.
- **The call list.** A run's calls, in order, from its bytes of the stream: each `tool_use` with the first line of
  its input, its time, whether it failed or was refused, and whether it is still open. `tally`
  (`internal/claude/totals.go`) already joins each `tool_use` to its `tool_result` by ID, with its time, and each
  `permission_denied` to its call; it gains the list, and `Recount` answers it in the same pass. A call with no
  result is open only in a running run. In an ended run, it shows as cut off.
- **Why a run started.** The ledger's start line gains `why`, with the values the Terms list, and `round`, `rounds`
  and `try` where they apply. Today `build.run` gets only a prompt from `after`, which can be a failed gate's report,
  a conflict, a failed landing gate's report or the audit's findings, so `after` and `build.round` answer the reason
  and the round beside the prompt. `build.start`, the resume after `interrupted`, and `build.audit`'s loop know theirs.
  `talk` writes `limit` itself, since it resumes after a usage limit in its own loop. Deriving them from the lines
  around a run was weighed: a conflict with main leaves no line of its own, and `rounds` can change on main
  mid-build. The ledger is empty in this repository today, so no old line lacks a field; one that does shows it as
  unknown.
- **Rounds.** The view counts nothing: a run's round is its start line's, and a build's rounds are its highest
  round, or 0 when kiln never sent it back. The gate line gains `landing`, true for the gate on the rebased work
  (`build.land`), so the build screen marks it apart from the gates before it.
- **The trend.** The home screen's window is the last 10 builds that ended, by any outcome, dropped and stopped
  included, since each cost what it cost. A running or queued build is outside it. Each number is the median of the
  builds that know it; a build whose number is unknown is left out of that median, and the row says how many it
  holds, as "9 of 10". With fewer than 20 ended builds, the previous window holds the rest, and shows "—" when it
  holds none.
- **Idle.** Yellow past one minute with no call open, which is where a model has stopped rather than a tool.
- **An audit that changed the tree.** Around each audit run, kiln reads `git status --porcelain` in the build's
  tree, and the audit's end line gains the paths that changed across it. Then kiln resets the tree to the commit the
  audit read, with `git.Reset`, as landing already does. The tree is clean when the audit starts, since `build.run`
  commits before the gate, so the reset takes only what the audit left. The build screen marks the run red, with
  the paths. The build session gets the audit's findings as text, as today, and nothing the audit wrote reaches a
  commit.
- **The command.** `kiln tui [<name>]` in `cmd/kiln`, beside `kiln stats`. It checks that its output is a terminal
  before it draws.
- **Before approval: the audit's tools.** The audit that reviews this build is the one compiled into the `kiln`
  that runs it: `guidance.Audit()` embeds `guidance/agents/audit.md`, and its tools are set in
  `internal/kiln/build.go`. So a hand change on main, before `kiln approve stats-tui`, gives the audit `Bash`,
  allows `Bash(tui-test *)` in `.claude/settings.json`, which kiln's sessions read (`--setting-sources project`),
  and tells the audit how to drive a TUI. tui-test's CLI is on the developer's `PATH`.
- **The specs list.** Each spec in the book, with its state, and each spec in the ledger the book no longer holds, as
  accepted. Newest build first.
- **The spec text.** As approved: `git.Show(ctx, approved, specPath(name))`, as `build.begin` reads it. Its
  amendment: the outcome line "waits on its branch" gains `amendments`, the text `after` already reads into
  `record.Amendments`. The ledger outlives the book's record and the branch, so the amendment shows for a build
  that waits, one `kiln accept` landed, and one a new approval dropped.

## Alternatives Considered

### Cost and minutes per acceptance test on the home screen

- **Pros**: `kiln stats` already prints cost per acceptance test; a build that fixes defects without adding a test
  stands out, which is a smell about the audit or the spec.
- **Cons**: each build's fixed part, its start, audit and gates, weighs three times as heavily on a 3-test spec as on
  a 10-test one, and no build has yet shown that cost grows with the count of tests.
- **Reason for Rejection**: a small spec would read as a worse build. Per build, with size beside it, assumes
  nothing.

### `kiln stats --tui`

- **Pros**: no new command to learn; the view's numbers are spec 2's.
- **Cons**: `kiln stats` would mean two things, and Go's `flag` stops at the first name, so `kiln stats build-stats
  --tui` fails where `kiln stats --tui build-stats` works.
- **Reason for Rejection**: the view shows each spec's state and the live session too. The developer will weigh
  dropping `kiln stats` once `kiln tui` exists.

### Open the view whenever `kiln stats` writes to a terminal

- **Pros**: nothing new to type.
- **Cons**: an agent that runs `kiln stats` in a terminal would open a full-screen program it cannot leave.
- **Reason for Rejection**: no agent may open the view by accident.

### Leave what the audit wrote in the tree

- **Pros**: keeps how the audit changed a file, not only which file.
- **Cons**: when the audit has findings, the next commit's `git add --all` takes what it left, under the build
  session's name, and the landing squash carries it to main; the red mark shows only after it landed.
- **Reason for Rejection**: the ledger's paths are what the developer needs to judge the audit, and a stray
  screenshot or binary must not reach main.

### The audit reads screens a test saved, and runs nothing

- **Pros**: the audit keeps Read, Grep and Glob, and its promise that it changes nothing holds by construction.
- **Cons**: it sees only the screens a test chose to save, never a click, a scroll or going back.
- **Reason for Rejection**: a model that drives the view finds what a developer would trip on, which a saved screen
  never shows. The ledger shows whether it changed anything.
