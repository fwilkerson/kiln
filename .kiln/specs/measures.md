# Spec 12: A monitor that keeps a gauge's line

Status: Approved, 2026-11-14

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

A spec is the developer's work. A gauge is a signal: a number kiln reads after each landing, with a line the
developer set for it. A monitor is how the developer improves on a signal: a job that runs on the schedule they set,
finds a problem, and fixes it. Today nothing joins the two. A monitor's check is a script of its own, which says only
whether to fix.

After this spec, a monitor may name a gauge where it would name a check. The developer then writes the measurement
once, as the gauge's script, and kiln reads it three ways:

| Reading         | When                      | `$KILN_BASE`, where it starts          | A crossed line  |
|-----------------|---------------------------|----------------------------------------|-----------------|
| After a landing | each landing, as today    | main's tip before the landing          | shown, as today |
| A monitor's run | on the monitor's schedule | git's empty tree: the whole repository | starts a fix    |
| A fix's gate    | each gate of that fix     | the commit where the fix left main     | fails the gate  |

```text
kiln monitor run ─▶ a due monitor that names a gauge
  │
  ▼
the gauge, at main's tip, over the whole repository
  │ command fails, or no number ─▶ couldn't run: no fix, still due
  │ on its line ─────────────────▶ healthy: no fix
  │ crossed
  ▼
held? ─▶ the fix, as a check's: the agent reads what the gauge printed above its number
           │
           ▼
         gate, then the gauge over what the fix changed ── crossed ─▶ back to the agent, with what it printed
           │ on its line, or no number      (a command that fails goes back to the agent too)
           ▼
         audit ─▶ lands ─▶ the gauge after the landing, which the gate already held to its line
```

A script that measures the files changed since `$KILN_BASE` needs no second mode. A diff against git's empty tree
names every file, so the same command reads one landing, one fix, or the whole repository.

What showed the need: on 2026-11-14, the developer set up the monitor `code-health`, which each day refactors the
three Go files that `go tool health` scores lowest. kiln then had the gauge `code health` too, CodeScene's score of
the files a landing changed, with a line of 10. Three seams showed that day:

- **A check with two modes.** kiln runs one check command at main and in the fix's gate.
  `.kiln/monitors/code-health` tells them apart by `git diff main...HEAD`. At main, it lists the lowest files in the
  repository. In the gate, it fails while a file the fix changed scores below 10, which is a gauge's reading held to
  a line. At `6563ac5`, the check still has both modes.
- **Nothing followed a crossed line.** The ledger holds two crossed readings of `code health`: 7.8 at `c518769` on
  2026-11-12, and 9.11 at `144d20f` on 2026-11-14. Each waited for the developer to read `kiln status`. The second
  was fixed only once the developer set up the monitor by hand.
- **A check has no number.** The ledger holds the check's summary of 2026-11-14 as prose: "10 of 76 files score
  below 10.0". kiln shows a monitor's last summary, and nothing of how its runs moved.

`b784746` then dropped CodeScene, so `go tool health` is kiln's one score. At `6563ac5`, `kiln.json` names no gauge,
and nothing shows that score after a landing.

What shows that one script serves: on 2026-11-14, at `76bf3f7`, a draft of such a gauge for `go tool health` read 8
over the whole repository, and listed each of the 12 files below 10. It read 9 over the commits since `3d10131`, and
printed no number over `76bf3f7` alone, which changed no Go file.

## Goals

- A monitor names a gauge `kiln.json` holds, in place of a check. The developer writes one script, the gauge's.
- On the monitor's schedule, kiln reads the gauge at main's tip, over the whole repository. A reading that crossed
  the gauge's line starts a fix. A reading on its line is healthy. A reading kiln could not take couldn't run, and
  starts no fix.
- The fix's agent reads what the gauge printed above its number, and the reading beside its line.
- The fix's gate reads the gauge over what the fix changed, and fails while that reading crosses the line. So the
  reading kiln takes after the fix lands never crosses, and the gauge's script needs no mode for the gate.
- A reading that names no number never crossed, in a fix's gate as after a landing. So a fix that deletes the files
  that crossed can land.
- A monitor that names a gauge is held as one with a check is, by what the gauge printed. It is held, too, after a
  fix of it landed, until its entry or what the gauge prints changes: a fix that left the finding as it was runs
  once.
- `kiln tui` shows how the signal moved: the gauge's readings of the whole repository, over the monitor's runs, in
  its gauges panel. `kiln status` gains nothing: it shows a monitor's summary, as today, which is the last reading.
- A gauge is still read after each landing, a spec's or a fix's, whether or not a monitor names it.

## Non-goals

- A gauge that blocks a spec's landing. A line that must hold for each build is a gate command, as the README says.
  Only the fix of a monitor that names the gauge is held to it.
- One list in `kiln.json` in place of `gauges` and `monitors`. A monitor names a gauge, and each keeps its entry.
- A monitor with both a check and a gauge. No monitor the developer runs needs a check that finds and a gauge that
  judges: `uat` has a check alone, and `code-health` needs the gauge alone.
- A landing's crossed reading that runs the monitor before its interval. The developer sets a monitor's schedule,
  and its run reads the whole repository, so it finds what a landing left. `docs/later.md` holds the brief, as "A
  crossing that runs a monitor early".
- A new line in `kiln status`. The developer reads it for what is queued and what waits on them, and reads
  `kiln tui` for how the codebase is doing.
- A schedule at a time of day, which spec 11 left out.
- kiln's own `.kiln/`. A build writes nothing there, so the developer commits kiln's own gauge and monitor, as
  "kiln's own monitor" in the Technical Plan lists them.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

## A fix in which the gauge reads no number

The gate reads the gauge over what the fix changed, so it can find nothing there to measure, in two ways:

- The fix deletes the files that crossed. The gauge has no file left to score, and the fix is a good one.
- The fix changes only files the gauge never scores, such as a doc. It fixed nothing.

A gate that failed on no number would stop the first to catch the second. So both pass the gauge, and the hold
catches the second: a landed fix that left the gauge printing the same holds the monitor. kiln's own check fails such
a fix today, with "the fix changed no Go file", so this is a change for `code-health`. A gate that read the whole
repository would always get a number, but one fix would then have to clear every file below the line.

## Acceptance tests

Each test runs kiln on a scratch repository whose `kiln.json` names a gauge and a monitor that names the gauge, with
the scripted claude for the monitor's agent. The gauge's command is a short shell script, kept under
`.kiln/gauges/`, with a `max` of 0. It reads each `.txt` file changed since `$KILN_BASE` that the commit still
holds, as `git diff --name-only --diff-filter=d "$KILN_BASE" HEAD` lists them. It prints a line that names each one
that holds the word `low`, then the count of those files. Where the list holds no `.txt` file, it prints no number.
Each repository holds a `.txt` file without the word, so a reading of the whole repository names a number.

No test here drives `kiln tui`. Its row of a monitor's readings is checked through the model's `Update` and `View`,
as `CLAUDE.md` says of the terminal UI's monitors.

### 1. A gauge on its line starts no fix

- **Given**: a monitor that names a gauge, and a main in which the gauge reads on its line over the whole
  repository.
- **When**: `kiln monitor run` runs.
- **Then**: no fix starts. `kiln status` shows the monitor healthy, with its reading beside the gauge's line.

### 2. A crossed gauge starts a fix, and the agent reads what the gauge printed

- **Given**: a monitor that names a gauge, and a main that holds two files the gauge counts against its line. Main's
  last commit changed neither file.
- **When**: `kiln monitor run` runs.
- **Then**: a fix starts. The agent's first message holds what the gauge printed of each of the two files, and the
  reading beside the gauge's line. `kiln status` shows that reading as crossed.

### 3. The gauge holds the fix to its line, over what the fix changed

- **Given**: test 2's repository. The agent's first turn changes one of the two files, and leaves it counting
  against the line. Its second turn changes that file so that it no longer counts. No turn changes the other file.
- **When**: `kiln monitor run` runs.
- **Then**: the gate fails after the first turn, and the agent's next message holds what the gauge printed of the
  file it changed, and nothing of the other file. The fix lands after the second turn, though the other file still
  counts against the line. `kiln status` shows the gauge's reading of that landing, which did not cross.

### 4. A reading kiln could not take starts no fix

- **Given**: a monitor that names a gauge whose command fails, and a second monitor that names a gauge whose command
  prints no number.
- **When**: `kiln monitor run` runs.
- **Then**: no fix starts. `kiln status` shows that each monitor couldn't run, with why, and that each runs at the
  next `kiln monitor run`.

### 5. A fix that deletes what crossed lands

- **Given**: a monitor that names a gauge, and a main that holds one file the gauge counts against its line. The
  agent's turn deletes that file.
- **When**: `kiln monitor run` runs. Then the monitor is due again, and `kiln monitor run` runs.
- **Then**: the fix lands, though the gauge reads no number over what the fix changed. After the second run,
  `kiln status` shows the monitor healthy.

### 6. A fix that left the gauge as it was runs once

- **Given**: a monitor that names a gauge that crossed. The agent's turn changes only a file the gauge never reads.
- **When**: `kiln monitor run` runs. The monitor is due again, and `kiln monitor run` runs. Then a commit to main
  changes what the gauge prints, the monitor is due once more, and `kiln monitor run` runs.
- **Then**: the first run's fix lands. The second run starts no fix, and `kiln status` shows the monitor held by its
  landed fix. The third run starts a fix.

## Technical Plan

### kiln.json

- `monitor` in `internal/kiln/monitor.go` gains `Gauge`, the key `gauge`: the name of a gauge that `kiln.json`'s
  `gauges` holds, such as `"gauge": "health"`. It is empty for a monitor that names none.
- `config.monitorRefusal` refuses a monitor whose `gauge` names no gauge of `gauges`. It refuses one that names both
  `check` and `gauge`, and one that names `gauge` and `gates`, since `gates` says whether a check joins the gate.
  Each is a row in the table that tests `parseConfig`, and needs no acceptance test: `kiln monitor run` already
  prints what `parseConfig` refuses.

### Where a reading starts

- kiln gives each reading's command `$KILN_BASE`, as `readGauge` in `internal/kiln/gauge.go` does today.

  | Reading         | `$KILN_BASE`                                      | Worktree, under kiln's cache directory |
  |-----------------|---------------------------------------------------|----------------------------------------|
  | After a landing | main's tip before the landing, as today           | `.gauges`, as today                    |
  | A monitor's run | the ID of git's empty tree                        | `.monitor-check`, at main's tip        |
  | A fix's gate    | the merge base of main's tip and the fix's commit | `.monitor-check`, at the fix's commit  |

- A fix's gate never reads the gauge in the fix's worktree. A gauge's script is written for a tree kiln throws away,
  and may leave a file behind, such as a coverage profile. kiln commits what a fix's worktree holds, so that file
  would land on main. `.monitor-check` is the tree `Repo.firstCheck` uses, and `kiln monitor run` holds the run lock
  through both readings.
- kiln asks git for the empty tree's ID, as `git hash-object -t tree /dev/null` prints it, through a new func of
  `internal/git`. It never writes the ID as a constant, since a SHA-256 repository has another.
- The merge base is the one `build.fixChanged` in `internal/kiln/fix.go` computes. At the landing's gate, the fix is
  rebased onto main, so the merge base is main's tip: the base the reading after the landing gets.
- The README's gauge bullet and the package doc of `internal/kiln` say that `$KILN_BASE` may be the empty tree. They
  say a command that names a commit's parents, such as `git log "$KILN_BASE"..HEAD`, fails there, and that
  `git diff "$KILN_BASE" HEAD` names every file.
- They say, too, that a script reads only the files the commit still holds, as `--diff-filter=d` lists them. A
  change that only deletes files then reads no number, and a deleted file never fails the command.

### A monitor's run

- `Repo.firstCheck` in `internal/kiln/monitor.go` reads the gauge of a monitor that names one, and treats its
  reading as a check's verdict. On its line is healthy. Crossed asks for a fix. No reading couldn't run.
- The monitor's summary is the reading, as `shownReading` in `internal/kiln/gauge.go` words one: the gauge's name,
  the number, then the line, such as `count: 2, line ≤ 0, crossed`, or `count: 0, line ≤ 0` on its line.
- A reading kiln could not take has one of two summaries. Where the command failed, it is the last line the command
  wrote, to stdout or stderr, as a check that couldn't run keeps it. Where the command printed nothing, it names the
  exit status. Where the command ended well and its last line is no number, it is
  `count: its command's last line is no number`. `readGauge` drops stderr today, so it changes to keep that line. A
  landing's reading that kiln missed gains the same why.
- A monitor's run reads no number as couldn't run, though a landing and a fix's gate read it as unknown. Over the
  whole repository, a gauge that measures nothing has nothing to keep, and the developer should see it.
- `kiln status` words each result as it does today, in `standing` of `internal/kiln/monitor_show.go`: the monitor's
  name, then `healthy`, `couldn't run`, `held: landed` or a fix's outcome, then when it ran. Its summary follows on
  the next line. A monitor that couldn't run `runs at the next kiln monitor run`.
- The monitor line of the ledger gains the gauge's name, the reading and the line it was read against. `entry` in
  `internal/kiln/ledger.go` already has the fields for a gauge line. `stats.go` reads a landing's readings by
  `gaugeKind`, so a monitor line never shows as a landing's reading. The ledger's doc comment names the new fields.
- A held monitor's run still reads the gauge, and its monitor line holds the reading.
- The agent's first message, which `fixing.prompt` in `internal/kiln/fix.go` writes, holds the reading as the
  summary words it, then all that the gauge's command printed above its number. What the gate printed after the
  monitor's command follows, where the monitor names a command. The monitor's prompt comes last, as today.
- The hold compares all of the gauge's stdout, its number included, by the hash a monitor line holds as `Printed`,
  as it compares a check's. `holding` in `internal/kiln/hold.go` and `fixing.prompt` treat a monitor that names a
  gauge as one that has a finding to compare, where they ask today whether it has a check.
- After a fix of it landed, a monitor that names a gauge is held while its entry and what the gauge printed are both
  as they were, as a fix that changed nothing holds a monitor with a check. So a changed prompt releases it. The
  gauge in the gate reads only what the fix changed, so a landed fix does not show that the repository's reading
  moved. `holding` already holds a monitor after a landed fix, where its check stays out of the gate, and kiln
  already words that as `held: landed`. The README's paragraph on a held monitor names neither, and gains both.
- A gauge's script that a monitor names prints the same for the same finding, as a check's does: no line numbers,
  no timestamps. The README says so beside the gauge.

### The fix's gate

- `monitor.gated` is true for a monitor that names a gauge. At each gate of the fix, the landing's included,
  `build.gatedCheck` in `internal/kiln/fix.go` reads the gauge at the fix's commit.

  | The reading over what the fix changed | The gate                                                             |
  |---------------------------------------|----------------------------------------------------------------------|
  | on its line                           | as its commands left it                                              |
  | crossed                               | fails, and the report holds the reading and what the command printed |
  | no number                             | as its commands left it: an unknown reading never crossed            |
  | the command failed                    | fails, and the report holds the last lines the command wrote         |

- The report for a crossed reading holds the reading as the summary words it, then all that the command printed
  above its number.
- A gauge's command that fails in the gate goes back to the agent as a round, where a gated check's ends the fix
  couldn't run. A check may read what is outside the repository, such as a network. A gauge reads a commit, and the
  same command read main's tip when the run started, so a failure at the fix's commit is the fix's doing. A fix that
  ended couldn't run would be dropped, and its monitor would start a new session at each hourly run. Past its last
  round, the fix stops, which holds the monitor and opens a window.
- The gate takes the gauge's command and line from `kiln.json` at main's tip, where it takes its own commands. A
  waiting fix that `kiln accept` lands is then held to the line main holds now. Where main's `kiln.json` no longer
  names the gauge, the gate reads none.
- `internal/kiln/contract/monitor.md` says the gate runs `kiln.json`'s commands. It gains a sentence: the gate also
  reads the monitor's gauge, where it names one, over what the fix changed.
- What the gate trusts: the gauge's script is out of the fix's reach, but a tool that the script runs from the tree
  it reads is not. A fix that changed `go.mod`'s `tool` lines could give kiln's own gauge a lenient
  `go tool health`. Today's check has the same exposure, and so has each gate command. The audit reads the fix.

### What the developer sees

- `kiln monitor run` prints each monitor's line as today, with the reading as its summary:
  `code-health: found a fix to make: health: 8, line ≥ 10, crossed`.
- `kiln status` shows the summary beneath the monitor's line, as today, and gains no line. The gauge's own line
  there, its latest reading of a landing, stays as it is.
- `kiln tui`'s gauges panel gains one row for each gauge a monitor names, below the gauge's row of landings. It
  holds the gauge's name, the word `repository`, its newest line, then its readings of the whole repository, oldest
  first. A row too narrow keeps the newest. A crossed reading is red beside ✗, and a run kiln could not read shows
  as `?`, as in the row of landings.

  ```text
  ╭ gauges ──────────────────────────────────────────────╮
  │health ≥ 10  languages ✗ 9 · code-health 10           │
  │health, repository ≥ 10  ✗ 8 · ? · ✗ 9                │
  ╰──────────────────────────────────────────────────────╯
  ```

- The row's readings are the monitor lines of the ledger that name that gauge, in the order kiln wrote them, of
  each monitor that names the gauge in `kiln.json` now. A monitor line from before the monitor named the gauge
  names none, and is no reading. Two monitors that name one gauge share its row.
- `histories` in `internal/kiln/hold.go` keeps only a monitor's last line today. So `view.Monitors` in
  `internal/kiln/view.go` reads those monitor lines from the ledger, and `tui.Monitor` in `internal/tui/tui.go`
  gains them, each a `tui.Reading`. `gaugeRows` in `internal/tui/gauges.go` draws the row. The panel shows once a
  build or a monitor has a reading.
- A test of `internal/tui` checks the row through the model's `Update` and `View`. A row of `TestView` in
  `internal/kiln/view_test.go` checks that `view.Monitors` fills the readings from a ledger.

### What the plan changes beyond the code

- `README.md`: the Monitors section's step 1 and step 3, and its paragraph on a held monitor. The `.kiln/` list's
  `gauges/` and `monitors/` lines, and the bullets on a gauge and on a monitor.
- The package docs in `internal/kiln/kiln.go`, on a monitor, a gauge and the hold, and in `internal/tui/tui.go`, on
  the gauges panel.
- No scenario under `uat/testdata/` changes, and no test under `uat/monitors/`: a monitor with a check, and a gauge
  no monitor names, run as before.
- `TestGaugeRules` in `internal/kiln/rules_test.go` calls `readGauge`, and follows its new results.
  `internal/kiln/monitor_test.go` and `internal/kiln/rules_test.go` gain rows for the refusals, the gate's table and
  the hold.

### kiln's own monitor

The developer commits these once the spec is accepted, since a build writes nothing under `.kiln/`:

- `.kiln/gauges/health`: it reads each Go file changed since `$KILN_BASE` that the commit still holds. It prints
  each one that `go tool health` scores below 10, the lowest first, with what health found in it and no line
  numbers. Its last line is the lowest score, or 10. It prints no number where it read no Go file. It leaves out
  `uat/`, which a fix never writes.
- `.kiln/kiln.json` gains the key `gauges`, which it lacks at `6563ac5`, with one gauge:
  `{"name": "health", "command": ".kiln/gauges/health", "min": 10}`. The monitor `code-health` drops `check`, and
  gains `"gauge": "health"`. Its prompt names how many of the listed files one fix takes, which the check's argument
  says today.
- `.kiln/monitors/code-health` is deleted. `.kiln/agents/refactor.md`, `CLAUDE.md` and `docs/practices.md` name the
  gauge where they name the check.

## Alternatives Considered

### The gauge joins the gate only, and a check's last line carries a number

- **Pros**: a check keeps its own summary, in the developer's prose. No script learns of the empty tree.
- **Cons**: kiln's `code-health` keeps two scripts that parse `go tool health` alike. The check's last line gains a
  key, a second contract for a number beside the gauge's.
- **Reason for Rejection**: one script reads both the repository and one change, as the Overview measured, and
  kiln's `code-health` wants both readings. The developer chose the gauge in place of the check on 2026-11-14.

### A gate that fails a fix in which the gauge reads no number

- **Pros**: a fix that changed nothing the gauge scores goes back to its agent at once, as kiln's check sends it
  back today.
- **Cons**: a fix that deletes the files that crossed reads no number too, and would never land.
- **Reason for Rejection**: the hold catches the fix that changed nothing the gauge scores, and stops no good fix.

### A gauge's failed command ends the fix couldn't run, as a gated check's does

- **Pros**: one rule for a check and a gauge. A failure that is not the fix's doing, such as a missing tool, costs
  the agent no round.
- **Cons**: a turn that leaves a file the gauge can't parse loses the fix's work. The monitor is then due each
  hour, and each run starts a new session, with no window opened.
- **Reason for Rejection**: the gauge read main's tip moments before, so a failure at the fix's commit is most
  often the fix's doing, and the agent can mend it.

### The monitor's readings in the panel of the selected monitor

- **Pros**: the readings sit beside the fixes that moved them.
- **Cons**: the home screen would show each landing's reading of a gauge, and hide the repository's.
- **Reason for Rejection**: the gauges panel holds the signals, and the developer reads the repository's there.

### One list, `measures`, in place of `gauges` and `monitors`

- **Pros**: one kind of entry to learn.
- **Cons**: `uat` has a check that is no number, and a gauge no monitor names has no schedule, so the one entry
  would hold two kinds by its keys. It would also rewrite each `kiln.json` a project holds.
- **Reason for Rejection**: a monitor that names a gauge joins the two, and each keeps the keys it has.

### A third script that the gauge and the check both call

- **Pros**: no change to kiln. The repeated lines move to one file today.
- **Cons**: the check keeps its two modes, nothing follows a crossed line, and a check still has no number.
- **Reason for Rejection**: it closes the first seam of three, and only in part.
