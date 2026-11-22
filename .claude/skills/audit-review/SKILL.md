---
name: audit-review
description: Reviews the audit findings kiln recorded since the last review, finds the defects that recur, and coordinates the fixes that make each kind impossible, by design or with a mechanical check. The developer starts it in a session of their own.
argument-hint: "[since <date or commit>]"
disable-model-invocation: true
---

# Review kiln's audit findings

An audit reads each build's work, and kiln records each finding in its ledger. One finding is fixed in its own
build. A kind of finding that recurs is fixed here, once, so no later build repeats it.

The work runs in five steps. The developer decides at each question; you settle the rest from the ledger, the code
and a measurement.

## 1. Read the findings since the last review

The last review is the newest commit on main whose message holds an `Audit-Review-Through:` trailer. Its value is
the `at` of the last finding that review read. With no such commit, or an argument, start where the developer says.

```sh
since=$(git log -1 --format='%(trailers:key=Audit-Review-Through,valueonly)' --grep='^Audit-Review-Through:' main)
kiln ledger --kind finding | jq -c --arg s "$since" 'select(.at > $s)' > "$SCRATCH/findings.jsonl"
kiln ledger --kind run | jq -c --arg s "$since" 'select(.role == "audit" and .at > $s)' > "$SCRATCH/audits.jsonl"
kiln ledger --kind build | jq -c --arg s "$since" 'select(.at > $s)' > "$SCRATCH/builds.jsonl"
```

`$SCRATCH` is your session's scratchpad. A finding with `parsed: false` holds the whole reply in `reply`: read it
by hand, and count it as one.

## 2. Measure, then find what recurs

Report these numbers first, each by source: specs together, and each monitor by name.

- audits, findings, and findings per audit
- findings by category
- findings per changed line, from `git show --shortstat` of each landed build's commit
- audits whose `found` exceeded `reported`, so the cap cut a finding. The cap stays at five until this count says
  it costs real defects; the developer decided so on 2026-11-22.

Then group the findings by mechanism, not by file or package: what had to go wrong in the code for each to happen.
A group of three or more is a recurring kind. For each, name the findings in it by path and line.

Where one source draws more than its share, read why before you guess. Compare its agent file, its workflow, and
what the audit is told for it: `.kiln/agents/`, `.kiln/monitors/<name>/`, and the contract files in
`internal/kiln/contract/`.

## 3. Propose a remedy for each kind

The guide's "Make misuse impossible" ranks the remedies. Prefer, in order:

1. a design that makes the defect impossible to write, such as a type, a sealed interface or one funnel
2. a mechanical check: an arch test, an invariant test over every screen or every step, or a lint
3. a change to an agent's file or kiln's contract
4. a sentence in a doc

Name what each remedy would have caught, by finding. A remedy that shapes how kiln runs a build is a framework
change: say so.

Tell the developer the numbers, the kinds and the remedies, in a few lines each. Then ask about one remedy at a
time, in the order its answer changes the most, each with its options and your lean.

## 4. Coordinate the fixes

Give each approved remedy that needs no further decision to an agent of its own, in a worktree, with
[`brief.md`](brief.md) first in its prompt. Run independent remedies at once. Hold a remedy that changes the
files another one changes until that one lands.

- A framework change gets two agents that design it apart from one brief, which leaves your own design out. A
  fresh judge then scores both by the guide's "Judging two API designs", under neutral labels. Bring the verdict to
  the developer, and port what the losing design does better into the winner.
- Work the developer defers becomes a starter spec in `.kiln/specs/`, Status Proposed, for `/shape`.

To land a branch, wait until main's index is clean, then cherry-pick it onto main and run `go test ./...`. Another
session may hold main: `ListAgents` names the sessions, and `SendMessage` asks one to say when it is done. Abort a
cherry-pick that conflicts, and send the branch back to its agent to rebase. Remove each worktree and branch once it
lands.

## 5. Audit what landed, then record the review

Run one fresh agent over the packages the fixes changed, as the guide's "Working with agents on an API" says. Bring
each finding to the developer. Don't audit the fix for one of its findings.

Then commit, on main, whatever the review itself changed, with a trailer that records the last finding it read.
With nothing else to commit, make an empty commit:

```text
chore(audit): review the audit findings through <date>

<the numbers, each recurring kind, and the remedy that landed for it>

Audit-Review-Through: <the at of the last finding read>
```
