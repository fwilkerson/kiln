# Spec 6: Reading a build in kiln tui

Status: Approved, 2026-11-09

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

The developer reads each build in `kiln tui` newest first, reads in full what each session answered, reads a spec's
text as legibly as its source, and learns every key a screen takes from the screen itself, on a MacBook's keyboard.

```text
bk · build 2 · run 3: audit, try 1 · audit role · session 8a10426e
took 6m · context 197k · 41 calls · 0 refused · 52 turns · $1.92

  I found five bugs. The most likely is that the mouse wheel on the home screen moves the
  spec list's selection past ...                    ◀ the run's whole answer, as Markdown
  1. internal/tui/model.go:412  bug / reaches ...

  14:31:08  said   I found five bugs. The most likely is ...
  14:30:51  Bash   go test ./internal/tui/                   4.2s   ◀ calls, newest first
  14:30:12  Read   internal/tui/model.go                     0.1s
 j/k scroll · space/b page · g/G top/end · s spec as approved · ? keys · esc back
```

What showed the need: the developer used spec 3's `kiln tui` on its own second build, and noted each point below
(`fc2fad0`, "docs(later): note what the first use of kiln tui showed").

## Goals

- **Newest first.** The run screen lists a run's calls newest first, so a running session's newest call is at the
  top. The spec screen lists its builds newest first. Today both list oldest first.
- **What a session answered.** A run's answer is the text of its stream's `result` event: what the audit found, or
  what the build session says it did. The 15 answers in this repository's sessions run from 9 to 79 lines, in
  Markdown. The run screen shows none of it, only the model's text blocks as `said` lines cut to one row. The
  developer reads a run's whole answer, drawn as the spec text is, at the top of the run screen, above its calls.
  The answer is the last thing a run writes, so newest first puts it there, and an ended run opens on it with no key
  pressed. A running run has no answer yet, so its newest call is at the top.
- **Keys every keyboard has.** Each footer names keys a MacBook has: `j`/`k`, space/`b` and `g`/`G`, in place of the
  arrows, pgup, pgdn and end (`internal/tui/draw.go:156-162`). The view has five screens: home, a spec's (its
  builds), a build's (its runs), a run's (its calls) and the spec text. The spec's and the build's are the list
  screens, where a key moves a selection. The run's and the spec text are the text screens, which scroll. Today the
  text screens take `g`, `G`, home and end in kiln's own code, and hand every other key to Bubbles' viewport, which
  reads space, `f`, `b`, `d`, `u`, `j` and `k` (`internal/tui/model.go:239-244`). The list screens take only `j`,
  `k`, the arrows, pgup and pgdn (`internal/tui/model.go:343-348`), and gain space, `b`, `g` and `G`. Home keeps its
  keys, and its footer names `j`/`k` and `?` too. Every key a screen takes today it still takes.
- **A key list on `?`.** Each footer also names `?`, which opens a popover listing every key the screen takes. `?` for
  help is the convention of Bubbles' `help` component, lazygit and k9s.
- **"context peak" is "context".** The build screen's column header says "context peak"
  (`internal/tui/draw.go:553`), and shows the run's peak, while the run screen and the now line say "context" and
  show its last figure. Peak and last differ only after the session compacts its context, and they are equal in all
  8 runs of this repository's ledger. The column says "context" and shows the last figure, so one word means one
  number.
- **One mark for what kiln does not know: `?`.** Spec 2's rule holds, that a number kiln could not read is shown as
  unknown, never as zero. Today the view shows it two ways. `count` answers `?` (`internal/tui/numbers.go:252`), as
  do the build screen's cells and the spec screen's time, turns, failed and rounds cells. `dollars` answers "unknown"
  (`internal/tui/numbers.go:236-240`), so the spec screen's cost cell and the build's header show the bare word. The
  build's header says "time unknown" (`internal/tui/draw.go:643`). The run's header says "turns unknown" and "cost
  unknown", or "cost unknown until the run ends" while it runs (`internal/tui/draw.go:747-757`). The recent builds say
  "cost unknown", and a run started before the ledger kept its reason says "why unknown". Each shows `?`.
- **The spec text reads as legibly as its source.** Today Glamour draws it, with two faults the developer saw on
  spec 4's text. Inline code gets a background and a space on each side, so a sentence dense with code reads as a
  row of blocks. And inside a list item, Glamour drops the blank line between a code block, a table and the
  paragraph after it, while it keeps it at the top level. Tried with `charm.land/glamour/v2` v2.0.1, the latest:
  no style setting restores that blank line, since an item's blocks are joined before a style applies. Every spec the
  ledger has a build of puts code blocks or tables inside list items: spec 3 on 10 lines, spec 4 on 15, spec 5 on 8.
  The view shows the Markdown source as written, colored by its syntax. The source is already laid out for
  reading: its prose is written to 120 columns, and GoLand's formatter aligns each table, so the spacing and
  alignment the developer reads in GoLand are what the view shows. `**`, backticks and table pipes show, as in the
  source.
- **Keep** `j` and `k` moving the selection on the list screens.

## Non-goals

- The build screen stays in run order, oldest first. A build is a sequence of runs with gates between them, which
  spec 3's acceptance test 2 reads "in order", and no build in the ledger has more than 3 runs.
- `?` in kiln's plain text. The summary `kiln run` prints and a landed commit's message read as sentences, where a
  sum shows its known part, as "at least $4.20, 1 of 3 runs unknown". `?` is for the view, where a number is read at
  a glance.
- The `said` lines stay one row each. They are what the model said between calls, and the answer is the one text the
  developer asked to read in full.
- The audit answering in JSON. Alternatives Considered says why.
- A count of passing acceptance tests while a build runs. `docs/later.md` "Progress while a build runs" holds it.
  The gate is the only place kiln learns which tests pass, and a count read from it would show no progress: in the
  last build of each spec in the ledger, the first gate ran only after the build session's first run ended, 11 to 39
  minutes in, and every acceptance test passed at it. A count that moves needs kiln to run the tests while the session
  works, which is a change to building, not to the view.

## Open Questions

None.

## Acceptance tests

Each test drives `kiln tui` in a pseudo-terminal, by keys, and reads the screen, as spec 3's do.

### 1. A run's calls, newest first

- **Given**: a build whose session has made several calls and is inside another.
- **When**: the developer opens the running run, and the session makes another call.
- **Then**: the calls are listed newest first, and the new call appears at the top with no key pressed.

### 2. A spec's builds, newest first

- **Given**: a spec approved twice, so it has two builds.
- **When**: the developer opens the spec.
- **Then**: the second build is listed above the first, and is the one selected.

### 3. A run's whole answer

- **Given**: a build whose audit answered in several paragraphs, longer than the screen.
- **When**: the developer opens the audit's run, and scrolls to the end of its answer.
- **Then**: every line of the answer is shown, with its headings and its inline code colored.

### 4. Keys a MacBook has

- **Given**: a build long enough that its run screen, its spec's screen and its spec text each scroll.
- **When**: the developer reads the footer of each of the five screens, and on each screen that scrolls, presses the
  keys its footer names to page down, page up, go to the end and go to the top.
- **Then**: no footer names a key a MacBook lacks, such as End or Page Down, and each key does what its footer says.

### 5. Every key, on `?`

- **Given**: a build that has ended.
- **When**: on each screen of that build, the developer presses `?`, then closes what it opened.
- **Then**: a list of every key that screen takes shows over it, and once closed, the screen is as it was.

### 6. One mark for what kiln does not know

- **Given**: a build whose session is running, so kiln does not yet know its cost, its turns or its time.
- **When**: the developer opens `kiln tui`, the spec's screen, the build, and the running run.
- **Then**: each number kiln does not know shows as `?`, and no screen shows the word "unknown".

### 7. A spec's text, legible

- **Given**: a spec with a code block and a table inside a list item, and a sentence with inline code.
- **When**: the developer opens its spec text.
- **Then**: a blank line separates the code block and the table from the text after each, as in the spec's source,
  and inline code has no background color.

## Technical Plan

- **Drawing Markdown.** `markdown` (`internal/tui/model.go:479`) colors its text with Chroma's Markdown lexer
  (`github.com/alecthomas/chroma/v2`, already in `go.mod` through Glamour), which colors the code inside each fence by
  the fence's language too. It formats with Chroma's `terminal256` formatter and its `monokai` style, from which it
  drops each entry's background when it builds the style, so no token is drawn on a background. With the colors
  stripped, its output is its input: tried on spec 4's text, byte for byte. Then it wraps each line of prose wider than
  the view at the view's width, with `ansi.Wrap`, which keeps the colors. It never wraps a table row or a line inside a
  fence, since a wrapped row or diagram no longer lines up. Such a line is cut at the view's edge, and `h` and `l`
  scroll the text screen sideways to read the rest, through the viewport's `Left` and `Right` bindings, already in
  `viewport.DefaultKeyMap()`. Of specs 3, 4 and 5, 33 lines run past 120 columns, each a table row or a diagram's line,
  the widest 306. Their prose is within 120 columns, so it wraps only in a narrower terminal. A session's answer writes
  each paragraph on one line, up to 751 characters in this repository's sessions, so an answer always wraps. A wrapped
  line keeps the indent of the line it came from, and of a list item's marker, so an item's text stays under its item:
  an audit's findings are numbered items, each one long line. Glamour leaves `go.mod`, since `markdown` was its only
  use.
- **The answer.** `claude.Recounted` gains `Answer`, the text of the last `result` event in its stretch of the
  stream. `Recount` scans its stretch with `reading` (`internal/claude/claude.go:231-243`), as `Run` does, in place
  of a bare `tally`; `reading` already keeps the last result's text, which `Ended.Text` answers. A running run's
  answer is "" until its session writes a result. On the run screen, `loaded` puts the answer in the screen's `text`
  field, which a run screen holds nothing else in, and clears `drawn` whenever the answer differs from it. `refresh`
  draws it with `markdown` when `drawn` is empty or the width changed. So an answer that arrives while the developer
  watches is drawn, though the width did not change. Its lines head the run screen's body, followed by a blank row,
  then the calls.
- **Newest first, live.** The run screen draws its calls from `Recounted.Actions` in reverse. Today `follow` keeps
  the view at its bottom, where the newest call was, and a developer scrolled up reads rows that hold still, since a
  new call is added below them. Newest first adds it above them, so the view keeps that. While the developer is at
  the top, the view stays at the top. Otherwise the screen keeps the index in `Actions`, counted from the oldest, of
  the call at the top of the view, and works the offset out from it at each refresh. Any read, even one that arrives
  out of order after a slow load, then puts the same call at the top.
- **The spec screen.** It draws its builds in reverse. The `#` column keeps each build's number, 1 for the first, so
  a build keeps its number on the build screen's title. It opens with the newest build selected, as today, which is
  now the top row; `j` moves toward older builds. `screen.sel` and each row's click target stay a build's index in
  `Spec.Builds`, which never moves, since a new build is only appended (`internal/kiln/view.go:62`). Only `specPage`
  turns an index into a row, for drawing and for the line `EnsureVisible` brings into view. So a build queued while
  the developer is on the screen leaves the selection on the build it was on.
- **One table of keys per screen.** Each of the five screen kinds gets a key map of `key.Binding`s
  (`charm.land/bubbles/v2/key`). `Update` matches a key against it with `key.Matches`, the footer draws its short help,
  and `?` draws its full help. So the footer and the list cannot name a key the screen does not take, nor miss one it
  does. The run and spec text screens take their scrolling bindings from `viewport.DefaultKeyMap()`, which already holds
  space, `f`, `b`, `d`, `u`, `j` and `k`, beside their own bindings for `g`/home and `G`/end. Each binding keeps the
  keys it takes today, arrows and pgup included, and its help names the letter: `j/k`, `space/b`, `g/G`.
- **The list screens' new keys.** On the spec and build screens, `g` and `G` select the first and last row, and space
  and `b` scroll the body a page, as pgdn and pgup do today.
- **The popover.** `?` sets a flag on the model. While it is set, `layout` adds one layer above the screen, a box
  holding each binding's keys and help. `Update` checks the flag before it handles any key or mouse message. `q` and
  ctrl+c still quit. `?` and esc clear the flag, and a click anywhere clears it and opens nothing. Every other key
  and the wheel do nothing, so nothing changes under the list. On the home screen, while the filter takes text,
  every key but esc, enter and backspace is text, `?` included, as today. The popover is drawn from the same key map
  the footer draws.
- **The build screen's column.** Its header says "context", and its cell shows `Totals.Context.Last`
  (`internal/tui/draw.go:624`), as the run screen does.
- **The mark for unknown.** `dollars` answers `?` for an unknown cost. A fact in a sentence keeps its noun, as "cost ?",
  "time ?", "turns ?" and "why ?", so the developer knows which number it is; "? turns" and "? failed" in the build's
  header already do. A column shows `?` alone, as today, the build screen's "why" column included. A running run's
  header says "cost ?", and drops "until the run ends". The trend's "—", for a measure no build knows, and a refused
  call's "—", for a call that never ran, are not the word "unknown", and stay. The package docs that say "shows as
  unknown" (`internal/tui/tui.go:14`) say it shows as `?`.
- **Spec 3's acceptance test 1.** `acceptance/stats-tui/testdata/at1.txtar` reads `' · cost unknown'`, and reads
  `' · cost \?'` instead. Its Then, "with the run's cost as unknown", holds as written.
- **Spec 3's acceptance test 3.** `acceptance/stats-tui/testdata/at3.txtar` presses `up` to reach build 1, which it
  finds above the selected build 2. Newest first puts build 1 below, so the script presses `down` there, and `up`
  where it now presses `down`. Its Given, When and Then hold as written.
- **The acceptance tests' setup.** As spec 3's: testscript files in `acceptance/tui-reading/testdata`, run by
  `scenariotest.Run` on a scratch repository with the scripted claude, through the `term`, `keys` and `screen`
  commands. The scripted claude writes a `result` event with the text its `claude.json` gives
  (`internal/scenariotest/claude_session.go:298`), so test 3's answer is a multi-line string there.
  Test 3 and test 7 read a cell's colors with `scenariotest`'s `color` command, as spec 3's test 8 does; test 7
  needs it to read a cell's background too.
  Test 6's running run is test 1's: until its `result` event, the run's cost and turns are unknown, and so are
  the build's cost, turns and time. Test 1's live run
  is spec 3's test 1's: `kiln run &`, whose scripted Bash call waits on a file the script creates.
- **Where the terminal tests run.** As spec 3's: only in kiln's gate, since no sandboxed session can open a
  pseudo-terminal. The build session checks its screens through the model's `Update` and `View` in a test of
  `internal/tui`.

## Alternatives Considered

### Our own Markdown renderer, on goldmark

- **Pros**: the view draws Markdown, with no `**` or pipes, and kiln decides each block's spacing and each table's
  width.
- **Cons**: kiln owns a renderer for every Markdown form a spec or an answer may use, and its own table layout.
- **Reason for Rejection**: the source is already laid out for reading, by its author and GoLand's formatter, and
  showing it adds nothing to keep up.

### Keep Glamour, restyle inline code, and put no block inside a list item

- **Pros**: no change to how the view draws; a style change takes the background off inline code.
- **Cons**: no style setting restores the blank line inside a list item, so the fix is a rule on how specs are written.
  The view shows each spec as its build's approval held it, so specs 3, 4 and 5 stay cramped.
- **Reason for Rejection**: it leaves every build already in the ledger unreadable.

### The answer on a screen of its own, opened by a key from the run screen

- **Pros**: the run screen stays a list of calls, and an ended run opens on its newest calls, not below an answer of
  up to 79 lines.
- **Cons**: every reading of an answer costs a key, and a new screen kind with its own key. The run screen's body is
  already a scrolling viewport that draws Markdown as the spec text screen does, so the answer fits there.
- **Reason for Rejection**: the developer opens an ended run to read its answer. `G` reaches the calls in one key, the
  same one key this option would cost on each reading of the answer.

### The audit answers JSON, a row a finding

- **Pros**: each finding opens on its own, and the developer could act on one at a time. The scripted claude already
  accepts `--json-schema` (`internal/scenariotest/claude_call.go`).
- **Cons**: kiln hands the audit's text to the build session as its prompt (`internal/kiln/build.go:216-223`), so a
  change to fix what the developer sees changes what the build session reads.
- **Reason for Rejection**: the need was reading the answer, which the text meets with nothing new from the agent. It
  comes back if the developer wants to act on one finding at a time.

### Leave the footers as they are, and list every key only on `?`

- **Pros**: no footer changes, and the list names every key, letters included.
- **Cons**: the footer is what the developer reads without asking, and today it names pgdn and end, which a MacBook
  lacks.
- **Reason for Rejection**: the developer asked that each footer name keys every keyboard has.

### Name the keys in each footer by hand, as today

- **Pros**: no new package in the view; a footer reads exactly as written.
- **Cons**: the footer and the key handling are two lists that drift. Today the text screens take space, `b`, `d` and
  `u` through the viewport, and no footer says so.
- **Reason for Rejection**: one table that both handles a key and names it cannot drift.
