# Spec N: Tell the developer when a spec waits on them

Status: Proposed

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

The developer approves a spec and walks away. When the spec needs them again, a macOS notification says which spec
and whether it finished or stopped. A click on it brings forward the terminal that runs `kiln run`. There, the build's
last lines sum it up from what kiln recorded, and say what to run next. For a stopped build, they say why it stopped,
and give the command that opens a Claude Code session on it.

What showed the need: the developer wants to try Ghostty's notifications. Ghostty 1.4.1 is installed, and its
`desktop-notifications` setting is on, so it shows a notification that a program asks for with OSC 9. OSC 9 is an
escape sequence a program prints like text, which a terminal reads as "notify" and doesn't show. The developer tried
it: `printf '\e]9;kiln: notify stopped\a'` showed a notification, and a click on it brought forward the Ghostty tab
that printed it.

## Goals

- This spec builds on `specs/commands.md`: its commands are `kiln queue`, `kiln accept` and `kiln reject`.

- A build that ends stopped, waiting for UAT, or waiting on its branch with amendments notifies the developer. These
  are the three states in `internal/kiln/state.go` that wait on the developer.
- The notification names the spec, and the state `kiln status` shows for it.
- Under the line `kiln run` prints today when a build ends, a summary of the build follows. Every line of it comes
  from kiln's state, its ledger and git, never from a session's words, so the same build always prints the same
  summary.
- The summary ends with the next command for the developer to run. For a stopped build, that command resumes the
  build's own session, in its worktree, to ask it why the build stopped. A fix the session makes on `kiln/<name>` is
  dropped by `kiln queue <name>`, which builds anew from main, so the fix goes to main or the spec.
- The summary holds these lines, for spec `x`. The first line is what `kiln run` prints today, colon included. The
  second is the line a landed build's commit already holds. The Technical Plan gives the rule for each other line.

  ```text
  x: landed on main at 1a2b3c4d5e6f, and waits for UAT
    12 min · $3.40 · 41 turns · 2 failed calls
    1 of 3 rounds · the audit sent findings back
    7 files changed, 312 insertions, 40 deletions
    next: read it with git show 1a2b3c4d5e6f, then run kiln accept x

  x: waits on kiln/x, with amendments for UAT
    12 min · $3.40 · 41 turns · 2 failed calls
    0 of 3 rounds · the audit found nothing
    7 files changed, 312 insertions, 40 deletions
    <the amendments, as kiln status shows them>
    next: read the amendments, then run kiln accept x, or kiln reject x

  x: stopped: the gate still failed after its last round:
    12 min · $3.40 · 41 turns · 2 failed calls
    3 of 3 rounds · the audit had not run
    failing: TestAT2 (example.com/scratch)
    the work is on kiln/x, in ~/Library/Caches/kiln/kiln-1f2e3d4c/x
    next: cd ~/Library/Caches/kiln/kiln-1f2e3d4c/x && claude --resume <session>, to ask the session why;
          then fix main or the spec, and run kiln queue x
  ```

## Non-goals

- A notification for a state that needs nothing from the developer: queued, building, or paused for a usage limit.
- A notification when `kiln run` ends. Each spec it built has notified by then.
- A setting in kiln to turn notifications off. The terminal has its own, such as Ghostty's `desktop-notifications`.
- A change to `kiln status`. It already shows why a build stopped, and the amendments.
- A stopped build that goes on in its session once the developer has talked with it, instead of building anew.
  `specs/talk.md` takes it up.

## Open Questions

None. `specs/commands.md`, built first, settles what a stopped accepted landing does.

## Acceptance tests

Each test runs `kiln` on a scratch repository with the scripted claude, and reads the output of `kiln run`.

### 1. A stopped spec notifies, and says why

- **Given**: an approved spec whose build fails its gate past its last round.
- **When**: the developer runs `kiln run`.
- **Then**: its output holds one notification, which names the spec and the state `kiln status` shows for it,
  stopped. After the line that says the build stopped, the output names the failing test, the branch and the
  worktree that hold the work, and a command that opens a Claude Code session on the stopped build.

### 2. A spec waiting for UAT notifies, and sums up its build

- **Given**: an approved spec whose build passes its gate with no amendment.
- **When**: the developer runs `kiln run`.
- **Then**: its output holds one notification, which names the spec and the state `kiln status` shows for it,
  waiting for UAT. After the line that says the build landed, the output shows the build's time, cost, turns and
  failed calls, its rounds, the files it changed, and the command that accepts the spec.

### 3. A spec waiting on its branch notifies, and shows its amendments

- **Given**: an approved spec whose build amends it, and passes its gate.
- **When**: the developer runs `kiln run`.
- **Then**: its output holds one notification, which names the spec and the state `kiln status` shows for it,
  waiting on its branch with amendments. After the line that says the build waits, the output shows the build's
  time, cost, turns and failed calls, the amendments, and the command that accepts the spec.

## Technical Plan

- `internal/kiln/build.go`: `stop` and `after` print the line they print today. Then `run`'s loop, once `b.run`
  returns and any stop is recorded, reads the spec's record as stored. If its state is stopped, waiting for UAT, or
  waiting on its branch, the loop prints one OSC 9 sequence to the same writer, then the summary. The sequence is
  `ESC ] 9 ; kiln: <name> <state> BEL`, with the stored state. A build that set one waiting state and then stopped,
  as `after` does when `git show` fails, so notifies once, with the state `kiln status` shows. The sequence goes first
  so that no failed read in the summary can keep it from printing.
  The body starts with `kiln:` so that a spec named, say, `4` can't read as ConEmu's OSC 9;4 progress bar, which
  Ghostty also draws. The body holds no control character: `Queue` takes only a name that `specName` matches, and
  the state is one of the constants in `state.go`.
- Print the sequence whether or not the output is a terminal. A terminal that doesn't know OSC 9 ignores it, as
  xterm does an unknown OSC. Output sent to a file holds the sequence, and `tail -f` of the file in Ghostty still
  notifies.
- `internal/kiln/stats.go`: the summary, which holds these rules.
    - The time, cost, turns and failed calls are `r.summary(b.rec.Build)`, the line the landing commit holds.
    - The rounds are the record's `Rounds`, of the rounds `kiln.json` gave the build's last gate. The record keeps
      that limit, so the summary reads nothing from main, and a `kiln.json` changed during the build can't change it.
    - A line the summary can't read, such as the ledger's, shows as unknown, as the ledger's numbers do. It never
      stops the summary's other lines.
    - The audit's phrase comes from the record's `Audited` and the build's runs in the ledger:

      | `Audited` | The build's runs                         | Phrase                                 |
      |-----------|------------------------------------------|----------------------------------------|
      | false     | No audit run                             | the audit had not run                  |
      | false     | An audit run, as when all 3 tries failed | the audit ended with no result         |
      | true      | A run started for `whyFindings`          | the audit sent findings back           |
      | true      | An audit run, and none for `whyFindings` | the audit found nothing                |
      | true      | No audit run                             | no Go package changed, so no audit ran |

    - The landed commit is the one `after` moved main to, which the build keeps for the loop.
    - Files changed: a new method on `internal/git`'s `Git` runs `git diff --shortstat`. A landed build compares its
      one commit on main with its parent. A build on its branch compares `main...kiln/<name>`: the branch with its
      merge base with main. The merge base stays where the build left main however far main moves, since main never
      holds the branch's commits.
    - The amendments, for a build waiting on its branch, are the record's `Amendments`, indented as `kiln status`
      indents them.
- A stopped build's lines follow from why it stopped.
    - A stop past the last round names what failed. `judge` returns the failures it counted, beside the report it
      builds from them: each test that fails and doesn't fail at main, as `check.Failed` names it,
      `TestAT2 (example.com/scratch)`; each command that failed with no test named; and each path refused under
      `specs/`. A conflict with main counts the conflicting files. `setback` and `outOfRounds` carry that list, so a
      test the gate excused as failing at main is never shown as the build's. The `failing:` line shows the list.
    - A stop for a kiln error has a `Why` of "kiln could not go on with the build:" and the error, which the stop line
      already shows. The build's session never saw it, so the next step names no session: it is to fix what the error
      names, then run `kiln queue <name>`, which builds anew from main. A build its gate and audit passed, stopped
      only because main's checkout was in the way of its landing, is built again. `specs/talk.md` takes up letting such
      a build go on instead.
    - A stop past the last round, of a build with a `Session`, shows the branch and worktree that hold its work. Its
      next step resumes the session: `cd <worktree> && claude --resume <session>`, the worktree quoted with
      `shellQuote`. `b.stop` leaves the worktree and session in place, and `kiln queue <name>` removes them.
    - A stopped accepted landing, which `kiln status` shows as landing accepted amendments, has as its next step: fix
      what stopped it, then run `kiln accept <name>`, or run `kiln reject <name>` to turn the amendments away.
    - A build with no `Session` stopped before its session began, so it has no work and no session to resume. Its next
      step is: read `kiln status`, fix main or the spec, and run `kiln queue <name>`.
- The resumed session is Claude Code's own record of the headless one. kiln runs `claude -p --session-id <id>` in the
  worktree, with Claude Code's default settings directory, so Claude Code keeps the session's transcript under that
  directory, by the worktree's path. `.git/kiln/sessions/<id>/` holds only kiln's copy of the stream, and the files
  kiln launched it with. The developer believes `claude --resume <id>` in the worktree finds it, and no session can
  try it, since the acceptance tests run the scripted claude.
- The resumed session runs without kiln's hook and settings. That is meant: the developer is at the keyboard, and
  `kiln queue <name>` drops whatever the session writes on `kiln/<name>`.
- The same code runs when `kiln accept` lands a spec that waited on its branch, since `acceptAmended` runs the
  build. A landing that stops there notifies, and sums up, too. One that lands is retired, and doesn't.
- The package doc in `internal/kiln/kiln.go`, the command's doc in `cmd/kiln/main.go`, and `README.md` say that a
  build that waits on the developer sums itself up, and prints a terminal notification.
- `acceptance/notify/`: tests 1, 2 and 3 follow `acceptance/kiln/testdata/at9.txtar`, `at1.txtar` and `at5.txtar`.
  Each matches the sequence in `stdout` with a regular expression.

## Alternatives Considered

### A fresh Claude Code session for a stopped build

- **Pros**: kiln writes its prompt, naming the spec, the branch and the gate's report. It isn't anchored on the
  build's failed attempts.
- **Cons**: It starts out knowing only what the prompt names, and kiln has a prompt to write and keep true.
- **Reason for Rejection**: The developer would rather ask the session that built it. That session already holds the
  spec, its work, every gate report and its tries.

### terminal-notifier

- **Pros**: A click can run a command, such as `kiln tui <name>`.
- **Cons**: It needs a Homebrew install, which this machine lacks, and a test reads it only through a scripted
  stand-in on the PATH.
- **Reason for Rejection**: OSC 9 was tried in Ghostty, and a click brings forward the tab that runs `kiln run`,
  where the summary is. A blind agent weighed the three ways, and picked OSC 9 for the same reason.

### osascript

- **Pros**: No install.
- **Cons**: A click on its notification opens nothing useful.
- **Reason for Rejection**: The click is the point.

### OSC 777

- **Pros**: It carries a title as well as a body, so the title can read `kiln`. Ghostty reads it.
- **Cons**: iTerm2 reads only OSC 9. Ghostty, WezTerm, foot and kitty read both.
- **Reason for Rejection**: OSC 9 reaches more terminals, and the body carries `kiln:` in place of a title.

### Print the notification only when the output is a terminal

- **Pros**: A log file of `kiln run` holds no escape sequence.
- **Cons**: A session's acceptance test can't read it, since only kiln's gate can open a pseudo-terminal.
- **Reason for Rejection**: The README runs `kiln run` in a terminal, and a file of its output still notifies under
  `tail -f`.
