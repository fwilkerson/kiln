# Spec 9: Gauges

Status: Approved, 2026-11-12

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

The developer curates a codebase kiln builds. A gauge is a command the developer names in `kiln.json`, which prints
a number, and a line they set for that number, such as mutation score, a CodeScene score or test time. kiln reads
each gauge right after each build lands on main, so the developer sees, spec by spec, how the codebase moves against
each line. `kiln run` prints the readings once they are taken, and `kiln status` and `kiln tui` show them. A gauge
never blocks a landing: a line that must hold is a gate command, which `kiln.json` already has.

What a gauge measures is the project's: kiln runs the command and compares its number with its line, and knows
nothing else about it. kiln ships no gauge.

What showed the need: the developer chose it on 2026-11-09. Monitors, which this spec's starter held too, moved to
`.kiln/specs/monitors.md` on 2026-11-12.

## Goals

- The developer names any number of gauges in `kiln.json`, each a command that prints a number, and a line, with the
  side of it the number should stay on.
- kiln reads each gauge right after each build's work lands on main, at the landed commit, and keeps each reading in
  its ledger. A landing by `kiln accept` is read too.
- A gauge's command can measure one build's work alone, since kiln tells it the commit main stood at before the
  landing.
- `kiln run` notifies the developer of a landing, and sums up its build, before it reads the gauges, since UAT needs
  no reading. It prints the readings beneath once it has taken them, marks each that crossed its line, and only then
  starts the next build.
- `kiln status` shows each gauge `kiln.json` names, with its latest reading beside the line it was read against, and
  says whether it crossed.
- `kiln tui` shows each gauge's readings over the landings, each beside the spec that landed it.
- A reading kiln could not take shows as unknown, never as zero, as the ledger's other numbers do. An unknown reading
  never counts as crossed.

## Non-goals

- A gauge kiln ships: a mutator, or any gauge in a preset's `kiln.json`. Which gauges a project runs, and with which
  tools, is the developer's to set up once this lands.
- Any meaning kiln gives a number. A command that wants a ratio over several landings, or over one build's diff,
  computes it itself.
- A gauge that blocks a landing, or sends work back to the session. A gate command does that.
- Flaky tests in the gate: comparing each failure with main, and a quarantine list. `docs/later.md` "Flaky tests in
  the gate" holds it, and its trigger is a flaky-test gauge rising.

## Open Questions

## Gauges

A gauge is a number over the landings, with a line the developer sets. Gauges the developer named, and others
the starter listed, each a command a project could name:

| Gauge              | Source today                                                                                                                   |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------|
| Mutation score     | a mutation tool, run on the Go lines added since `$KILN_BASE`. Eidos's `scripts/mutate.sh` is one                              |
| CodeScene score    | CodeScene's Code Health of the files the landing changed                                                                       |
| Test time          | the suite's wall time, against a ceiling                                                                                       |
| Flaky tests        | Eidos's `scripts/gate.sh` `suite_again` notes each test that passed on a second run alone. A gauge counts those notes per gate |
| Comment share      | odh's `tools/size`: comment lines per 100 lines, by directory                                                                  |
| Agent friction     | kiln's ledger, `.git/kiln/ledger.jsonl`: failed calls and turns per build, and context per run, which `kiln tui` already shows |
| GoLand inspections | odh's `tools/inspect`. It needs GoLand open on the machine, so it can't gauge an unattended landing                            |

- A gauge's command and its line sit in `kiln.json`, beside the gate. kiln reads `kiln.json` from main's tip, and the
  guard refuses a build's write under `.kiln/`, so a build can't move its own line, as it can't change the gate's
  commands.
- A script that a gauge's command runs from the landed tree is the build's to change, as a script the gate runs is
  today. A script kept under `.kiln/`, such as `.kiln/gauges/`, is not, since the guard and the gate refuse a build's
  write there. The README says a gauge's script belongs there.
- **What one gauge can cost.** On 2026-11-12, Eidos's mutator at `07992d10` ran on kiln's landing `813905f` in 46 s:
  152 mutants, 53 killed by the mutated package's own tests. kiln's acceptance tests build their own kiln binary, so
  no mutant reached them, and a run that did would take minutes to tens of minutes. Reading on landing holds the next
  queued build for that long.

## Acceptance tests

Each test runs kiln on a scratch repository whose `kiln.json` names a gauge with a short shell command, such as one
that prints a fixed number. `kiln run` already says when it starts building a spec, at `internal/kiln/build.go:393`,
so its output shows which came first: a landing's readings, or the next build.

### 1. A landing gets a reading

- **Given**: `kiln.json` names a gauge whose command prints a number, and a spec is queued.
- **When**: `kiln run` lands the spec.
- **Then**: `kiln run`'s output shows the gauge's reading for that landing, and `kiln status` shows it beside the
  gauge's line.

### 2. A reading past the line shows it crossed, and the build still lands

- **Given**: `kiln.json` names a gauge whose command prints a number on the wrong side of its line.
- **When**: `kiln run` lands a spec.
- **Then**: the spec waits for UAT, its work is on main, and both `kiln run`'s output and `kiln status` show that the
  gauge crossed its line.

### 3. A gauge measures the landing's own work

- **Given**: `kiln.json` names a gauge whose command counts the files changed since the commit kiln gives it as the
  base.
- **When**: `kiln run` lands a spec whose build changed two files.
- **Then**: `kiln status` shows the reading 2.

### 4. A reading kiln could not take is unknown

- **Given**: `kiln.json` names a gauge whose command fails.
- **When**: `kiln run` lands a spec.
- **Then**: the spec waits for UAT, and `kiln status` shows the gauge's reading as unknown, not as zero.

### 5. A landing by kiln accept is read

- **Given**: a spec waiting on its branch with amendments, whose build changed two files, and `kiln.json` names a
  gauge whose command counts the files changed since the commit kiln gives it as the base, outside `.kiln/` and
  `PRODUCT.md`.
- **When**: the developer runs `kiln accept` on it, and it lands.
- **Then**: `kiln accept`'s output and `kiln status` show the reading 2.

### 6. Each gauge reads every landing in turn

- **Given**: `kiln.json` names two gauges, and two specs are queued.
- **When**: `kiln run` lands both.
- **Then**: `kiln run`'s output shows both gauges' readings after each landing, before the next spec's build starts.

### 7. kiln tui shows each gauge's readings over the landings

- **Given**: two specs landed, each with a reading of the same gauge.
- **When**: the developer opens `kiln tui`.
- **Then**: `kiln tui` shows the gauge's line, and both readings, each beside the spec that landed it.

## Technical Plan

### `kiln.json`

```json
{
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "gauges": [
    {"name": "mutation", "command": ".kiln/gauges/mutation-score \"$KILN_BASE\"", "min": 80},
    {"name": "test time", "command": ".kiln/gauges/suite-seconds", "max": 120}
  ]
}
```

- `config` in `internal/kiln/gate.go` gains `Gauges []gauge`. A gauge holds `name`, `command`, and exactly one of
  `min` or `max`: a reading below `min`, or above `max`, crossed. `parseConfig` refuses a gauge with neither, both,
  no name, or a name two gauges share. Its `DisallowUnknownFields` stays.
- `gauges` is no shared key: `sharedKeys` in `internal/kiln/init.go` is unchanged, and neither preset's `kiln.json`
  names one. `TestInitKeeps` holds as it is.

### Reading the gauges

- A new file, `internal/kiln/gauge.go`, reads the gauges of `kiln.json` at main's tip for one landing. It puts a
  worktree of its own, `.gauges` beside `.main` under the cache directory, at the landed commit with `git.Detached`,
  and runs each command there with `sh -c`, one at a time, as the gate runs its commands. It takes no `mainLock`, so
  a `kiln accept` of a spec waiting for UAT, whose gate at main takes that lock, doesn't wait on a slow gauge. Only a
  kiln that holds the run lock reads gauges: `kiln run`, or `kiln accept` through `run`.
- `$KILN_BASE` is main's tip before the landing: `onto` in `landOnce`, in `internal/kiln/build.go`, which the build
  keeps once main moves. It is not the landed commit's parent. A landing by `kiln accept` of an amended spec is two
  commits, the work and then the spec's retirement, as `prepare` and `retire` make them and
  `acceptance/kiln/testdata/at3.txtar:21-24` checks.
- The command's last line of stdout, trimmed, is its reading, parsed as a decimal number. A command that fails, or
  whose last line is no number, gives a reading with no value: unknown. An interrupt of kiln is not the command's
  failure: kiln writes no reading for it, and returns the context's error.
- Today `onMain` sets `b.landed` only for a landing that waits for UAT. It sets it for an accepted landing too, so
  `kiln accept`'s landing, which runs through `run`, is read the same way. A landed spec's retirement by `kiln accept`
  carries no build's work, goes through `acceptLanded`, and is not read.
- `run` in `internal/kiln/build.go` reads the gauges after `b.waits(ctx)`, for a build whose `landed` commit is set,
  and prints one line per gauge: its name, its reading or unknown, its line and side, and a mark when it crossed. The
  build picks the words.
- A landing whose readings kiln was interrupted before writing stays unread. Nothing reads a past landing, so a gauge
  added to `kiln.json` reads only the landings after it.

### The ledger

- A new kind, `gauge`, one line per gauge per landing: the build, the gauge's name, the landed commit, the value or
  that it is unknown, and the line and its side as `kiln.json` held them. The doc comment above `ledgerFile` gains it.
- A reading keeps the line it was read against, and is judged against that line wherever it shows, so moving a line
  later doesn't rewrite past readings. An unknown reading is never crossed.
- Readings are ordered by their line's `At`, which follows the landings, since each is written right after its own.

### Showing it

- `kiln status` ends with one line per gauge that `kiln.json` names at main's tip: its name, and its latest reading
  with the line it was read against, whether it crossed, and the spec and short commit of the landing it measured; or
  that it has no reading yet. An interrupt can leave a landing unread, so naming the landing keeps an older reading
  from passing as the newest landing's. A gauge removed from
  `kiln.json` shows only in `kiln tui`'s history. `record.status` in `internal/kiln/kiln.go` is per spec, so a new
  func, which `Status` calls after the specs, writes them.
- `kiln tui`: a gauge line names its build, so `collector.add` in `internal/kiln/stats.go` puts each reading on its
  build, and `buildStats.shown` in `internal/kiln/view.go` carries it on `tui.Build`, beside the spec's name. Each
  reading carries its ledger line's `At` and the line it was read against. Home in `internal/tui` draws the gauges
  from the specs it already holds: one row per gauge, its current line, then its readings ordered by `At`, not by
  their build's start, since an amended spec accepted late lands after builds that started later. Each reading shows
  its spec's name, a mark when it crossed, its own line where that differs from the row's, and an unknown as `?`.
  Home draws no gauge panel while no build has a reading.

### Docs

- `README.md`: the `.kiln/` section and its `kiln.json` line name gauges, and say a gauge's script belongs under
  `.kiln/gauges/`, out of a build's reach. "Use" step 3 says `kiln run` prints the
  readings after a landing, and step 5 that the ledger holds them.
- The package doc in `internal/kiln/kiln.go`: what a gauge is, when kiln reads it, `$KILN_BASE`, and that it never
  blocks a landing.
- The package doc in `internal/tui/tui.go`: home's gauge panel, and that an unknown reading shows as `?`.

### What it breaks

- No test reads `parseConfig` directly, and the acceptance tests that run `kiln status` match single lines by regular
  expression, such as `acceptance/kiln/testdata/at1.txtar:23`, in repositories whose `kiln.json` names no gauge. So
  no existing test breaks, as far as a search shows.
- Home's layout is held by `acceptance/stats-tui/testdata/at5.txtar:61-63` and `TestTrend` in
  `internal/tui/tui_test.go:65-69`, which expect the trend's right border at the line's end. Neither repository holds a
  reading, so with no gauge panel while no build has a reading, both hold. A panel beside the trend once a reading
  exists narrows the trend's tiles.
- An installed kiln older than this spec refuses a `kiln.json` that names a gauge. kiln's own `kiln.json` gains a
  gauge only once this lands and kiln is reinstalled, and the developer adds it, since a build can't write there.

## Alternatives Considered

### kiln ships a mutator

- **Pros**: every Go project gets the testable-code gauge from `kiln init`.
- **Cons**: kiln would own a mutation tool, and each preset would need its own.
- **Reason for Rejection**: the developer chose gauges per repository on 2026-11-12. kiln can't know every gauge a
  project wants, and a project may use a mutation library.

### kiln pools a count and a total over landings

- **Pros**: a ratio over few mutants would swing less.
- **Cons**: kiln would give a meaning to a number it knows nothing about.
- **Reason for Rejection**: the developer ruled on 2026-11-12 that a number and its line are the command's to mean.

### Read the gauges before the landing's notification

- **Pros**: the build's summary holds its readings.
- **Cons**: the developer hears of the landing only once every gauge has run, which may take tens of minutes.
- **Reason for Rejection**: the developer chose on 2026-11-12 to be told first, since UAT needs no reading.

### Read the gauges once no spec is queued

- **Pros**: a slow gauge doesn't hold up the next build.
- **Cons**: readings come late, and a run of several specs shows no landing's reading until the last.
- **Reason for Rejection**: the developer wants to see, after each spec, how the codebase moved.

### Read the landings kiln missed at the next run

- **Pros**: a landing whose reading an interrupt cut short still gets one.
- **Cons**: kiln must mark each landing unread, and decide whether a gauge added later reads every past landing, which
  could hold the next build for hours.
- **Reason for Rejection**: no Goal asks for it. A lost reading costs one point in a gauge's history.

### `gauges.json` beside `kiln.json`

- **Pros**: `kiln.json` stays the gate's alone.
- **Cons**: a second file read from main's tip by the same rule.
- **Reason for Rejection**: `README.md` already puts what a build must not move in `kiln.json`.

### A line that blocks a landing

- **Pros**: the number could not slide.
- **Cons**: it duplicates the gate, and a noisy number would stop builds.
- **Reason for Rejection**: a line that must hold is a gate command.
