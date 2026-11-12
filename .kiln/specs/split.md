# Spec 10: Tests written apart from the code

Status: Approved, 2026-11-12

<!-- Status is Proposed or Approved, with the date it was approved. kiln deletes the spec once the developer accepts
it, and PRODUCT.md names the commit that last held it. -->

## Overview

After this spec, a project can choose a second workflow, split, beside today's build then audit. In split, two
sessions start together from main, each in a checkout of its own. The build session writes the code the spec asks
for. The verify session works as QA would with a black box: it never sees the build session's work, decides from the
spec how to show each acceptance test holds, and writes that test's code. kiln gates the build session's work alone,
then stacks the two sessions' work on the spec's branch, and gates the stack. Once the gate passes, the audit reads
it, as it reads a build today, and the build lands as one commit.

The split divides the work, and it is meant to land fewer defects: a test written from the spec alone checks what the
spec says, not what the code happens to do.

kiln removes each checkout a build made once the build leaves kiln's hands, in either workflow.

What showed the need: `docs/later.md` "A project's own workflow" waits for "a project needs a role other than build
and audit". On 2026-11-12, the developer asked for a verifier that writes the acceptance tests apart from the
builder. The same day, `git worktree list` showed seven audit worktrees left from landed specs, from `.audit-commands`
to `.audit-tui-reading`. `auditTry` makes an audit's worktree (`internal/kiln/build.go:630`), and no line removes it.

## Goals

- A project chooses the split workflow in `kiln.json`, and each build of it runs a build session and a verify session
  at once.
- kiln hands neither session a file or a commit the other wrote, sends the build session no report of the stack's
  gate, and denies each session reads of the other's checkout. What the sessions' own replies carry is in Risks.
- kiln enforces each session's side: the build session writes nothing in the spec's acceptance path or its amendment
  file. The verify session writes only there and in the test harness. Both may write the harness, and the audit
  reads each harness file the build session changed for a weakened check.
- kiln gates the build session's work alone, and sends its failures to the build session alone. Once that passes,
  kiln stacks both sessions' work on the spec's branch, and gates the stack. Its failures go to the verify session
  first, which fixes its tests, reports a bug to the build session, or both. Each gate that goes back is one round.
- An amendment the verify session writes reaches the build session as the spec's new wording.
- Once the stack's gate passes, the audit reads the stack, and its findings go to both sessions. A build with no
  amendment lands as one commit, as today. A build with one waits on the spec's branch, as today.
- Every checkout a build made is gone once the build lands, is accepted, is rejected, or is queued anew. A build that
  stopped, or waits on its branch, keeps each session's checkout, so the developer can resume it there.
- The build then audit workflow works as it does today, but that its audit's worktree is removed once the audit
  ends.
- The developer names paths in `kiln.json` that the audit must read whenever the work changes them. In either
  workflow, work that changes one of them after the audit read it goes back to the audit before it lands, even when
  the change fixes the audit's own findings.

## Non-goals

- A workflow a project defines: its own roles, their order, and where each one's findings go. `docs/later.md` "A
  project's own workflow" keeps it, and this spec builds two fixed workflows.
- Two designers and a judge. `docs/later.md` keeps it. Its open problem, a checkout per designer, is the one this
  spec solves for the verify session, so a later spec can reuse it.
- Several builds per spec, and two specs built at once. `kiln run` still builds one spec at a time.
- A verify session's tests that must fail at main before the stack. It would refuse a spec revised after UAT, whose
  landed work already passes its unchanged Thens. `docs/later.md` "Limits on amendments" keeps it.
- Choosing the workflow per spec. Nothing has asked for one project to mix the two.
- Counting a split build's time as wall time. A build's time stays its session runs' time plus its gates', as its
  cost sums every run.
- Removing the audit worktrees earlier builds left. The developer removes the seven that exist today once, by hand.

## Risks

Each is a way the split can leak or weaken, which this spec names and leaves open.

- **The build session may write the harness.** A project that lists its harness under `reaudit` sends each later
  harness change back to the audit. One that doesn't lets a harness change made after the audit reach the stack's
  and the landing's gates unread.
- **Replies are free text.** The verify session's bug report, and the build session's `cannot hold:`, may quote the
  other side's code. So may the audit's findings, which go to both.
- **A revised spec leaks.** The build session's clone holds main's history, so `git log -p` shows it the old tests.
  The verify session's clone holds the landed code.
- **The read denials are untried.** Claude Code's docs describe them, and say Grep and Glob honor a `Read` denial on
  a best effort. No scripted test can show a denial holding, since the scripted claude runs no sandbox.
- **A wrong guess spends a round.** A verify session that guesses an output form the Technical Plan does not name
  spends one of the build's rounds, which `kiln.json` sets at 3.

## Open Questions

## Acceptance tests

Each test runs kiln on a scratch repository with a scripted claude, as `internal/scenariotest` does. Its `kiln.json`
names the split workflow, but in test 8.

### 1. A split build lands the code and its tests

- **Given**: the project chooses the split workflow, and a spec is queued.
- **When**: `kiln run` builds the spec, the build session writes its code, and the verify session writes its
  acceptance tests.
- **Then**: the spec waits for UAT, and main holds both the build session's files and the verify session's.

### 2. Neither session sees the other's work

- **Given**: the project chooses the split workflow, and a spec is queued whose stack fails its gate once.
- **When**: `kiln run` builds the spec, and each session lists the files and commits its checkout holds.
- **Then**: neither list holds any of the other session's, and each session's settings deny it reads of the other's
  checkout.

### 3. A failing stack goes to the verify session first

- **Given**: the project chooses the split workflow, and the verify session's test fails against the build session's
  code.
- **When**: `kiln run` gates the stack, and the verify session fixes its test.
- **Then**: the verify session got the gate's report, the build session did not run before the next stack's gate,
  the build has used one round, and the audit ran only after the stack passed.

### 4. A bug the verify session reports goes to the build session

- **Given**: the project chooses the split workflow, and the verify session's test fails against the build session's
  code.
- **When**: `kiln run` gates the stack, and the verify session reports that the code is at fault.
- **Then**: the build session gets the verify session's report and the failing test's Given, When, Then, but not
  the gate's report, and the build has used one round.

### 5. The audit reads the stack once its gate passes

- **Given**: the project chooses the split workflow, and the stack passes its gate.
- **When**: the audit runs.
- **Then**: the audit reads the directories of both sessions' files, its prompt names each harness file the build
  session changed, and its findings go to both sessions.

### 6. Each session writes only its side

- **Given**: the project chooses the split workflow, and `kiln.json` names the acceptance path and the harness.
- **When**: the build session writes a file under the acceptance path and the spec's amendment file, and the verify
  session writes one outside its side.
- **Then**: kiln refuses each of these writes, and none of the files is on the spec's branch.

### 7. A landed build leaves no checkout

- **Given**: the project chooses the split workflow, and a spec is queued.
- **When**: `kiln run` lands the spec.
- **Then**: none of the clones and worktrees the build made remains, but the `.main` worktree every gate shares.

### 8. A landed build of today's workflow leaves no audit worktree

- **Given**: the project chooses no workflow, and a spec is queued whose build the audit reads.
- **When**: `kiln run` lands the spec.
- **Then**: `git worktree list` names no audit worktree.

### 9. A stopped build keeps what the developer resumes

- **Given**: the project chooses the split workflow, and the stack's gate fails past the last round.
- **When**: the build stops.
- **Then**: both sessions' checkouts remain, and the summary names the command that resumes each session in its
  own.

### 10. A failure on the build session's side goes to the build session alone

- **Given**: the project chooses the split workflow, and the build session's work fails a gate command on its own.
- **When**: `kiln run` gates the build session's work.
- **Then**: the build session gets the gate's report, the verify session does not run again, and the build has used
  one round.

### 11. The build session learns of an amendment

- **Given**: the project chooses the split workflow, and the verify session amends a Given, When, Then.
- **When**: the build session next runs.
- **Then**: its prompt holds the amendment's new wording.

### 12. A revised spec's build session starts without its old tests

- **Given**: a split spec landed, its acceptance tests are on main, and the developer revised a Then and queued it
  again.
- **When**: `kiln run` builds it.
- **Then**: the build session's checkout holds no file under the spec's acceptance path, its gate never runs the old
  tests, and the spec lands with the verify session's tests.

### 13. A change to a path the audit must read goes back to the audit

- **Given**: `kiln.json` names a path the audit must read whenever the work changes it, and the audit finds a problem
  whose fix changes that path.
- **When**: `kiln run` builds the spec, and the build session fixes the finding.
- **Then**: the audit reads the work again before it lands.

## Technical Plan

```text
main ──┬─▶ kiln/<name>.build    build session, its own checkout
       └─▶ kiln/<name>.verify   verify session, its own checkout
                │ both end their turns
                ▼
   gate kiln/<name>.build alone ── fails ──▶ the build session
                │ passes
                ▼
   kiln/<name>, worktree <trees>/<name>: both sessions' work, stacked. Only kiln works here.
                │
   gate the stack ── fails ──▶ the verify session: fixes its tests, reports a bug, or both
                │                                     └─ a bug ──▶ the build session
                │ passes                       then the loop starts again at the build-alone gate
                ▼
   audit, in <trees>/.audit-<name>, at the stack ── findings ──▶ both sessions at once, then the build-alone gate
                ▼
   kiln/<name> lands as one commit, or waits there with amendments, as today
```

### The workflow

- `kiln.json` gains `"workflow": "split"`, `"acceptance"`, a path in which `{spec}` stands for the spec's name, such
  as `"acceptance/{spec}/"`, and `"harness"`, a list of paths, such as `["internal/scenariotest/",
  "internal/termtest/"]`. A Python project keeps its tests elsewhere, so no path is kiln's own. Without `workflow`, a
  project builds as today.
- kiln reads the three keys from `kiln.json` at the commit the build was queued at, `record.Approved`, as it reads the
  agents (`agentLaunch`, `internal/kiln/agent.go:144`). A change to main's `kiln.json` reaches only a build queued
  after it. The gate's commands are still read at main's tip.
- `kiln queue` refuses a split spec whose `kiln.json` at that commit names no acceptance path, or no
  `.kiln/agents/verify.md`.
- Neither preset names the workflow, but each names its acceptance and harness paths. `kiln init`, run again, adds
  only `rounds` (`sharedKeys`, `internal/kiln/init.go`), so an existing project adds the new keys by hand. The README
  says so.

### The checkouts

- Each session works in a clone of its own, under the cache's `<trees>/`, named `.build-<name>` and
  `.verify-<name>`. Worktrees share one object store, and kiln's hook sees only writes (`writeTools`,
  `internal/claude/settings.go:12`), so a session in a worktree reads the other's tests with `git show
  kiln/<name>.verify:acceptance/...`. kiln makes each clone with `git clone --no-local --single-branch --branch main
  --no-tags`, which holds only main's objects, and then removes its remote. On 2026-11-12, such a clone of kiln's
  main took 0.22 s and 2.2 MB, of which 840 KB was history, beside 1.3 MB for a worktree.
- A session's branch lives in its clone. The repository's `kiln/<name>.build` and `kiln/<name>.verify` are only ever
  fetches of them. kiln makes its own commits in the clone: the removal of a revised spec's old tests, a merge of
  main, and the fold of accepted amendments. So no later fetch drops one. No session fetches.
- Each session's settings deny it reads of the repository's main checkout with its `.git`, which holds both
  branches, the stack and kiln's session streams. They deny the whole of `<trees>/`, which holds the stack, the
  audit's worktree and the other clone, but for the session's own clone, which a narrower `allowRead` lets back in.
  They deny the other session's transcripts, which Claude Code keeps under `~/.claude/projects/`, in a directory
  named for the other clone's path: on 2026-11-12 it held 17 such directories for kiln's worktrees.
- The denials are `sandbox.filesystem.denyRead` and `allowRead` for sandboxed Bash, which the OS enforces, and
  `permissions.deny` rules such as `Read(//path/**)` for the Read tool, which Grep and Glob honor on a best effort.
  Claude Code's docs say so (code.claude.com/docs/en/sandboxing.md, and permissions.md). Nobody has tried them yet:
  how `denyRead` spells an absolute path, and what Grep and Glob honor, the build tries first. `claude.Launch` gains
  the paths to deny and allow.
- A clone holds `.claude/`'s links as plain files, as kiln's worktrees do today, since Claude Code's sandbox refuses a
  shell's write through a link under `.claude/`.
- A spec's name holds only `[a-z0-9-]` (`specName`, `internal/kiln/kiln.go:285`), so the dotted
  names never meet another spec's.
- The stack is `kiln/<name>`, in `<trees>/<name>`, the branch every later step reads today: the gate, the audit's
  `changedFiles`, the landing, `unland`, `kiln accept`, and the summary's "the work is on kiln/x". Only kiln writes
  there.
- kiln makes each stack anew from both sessions' tips. `internal/git` already merges one commit onto another with
  `Rebase`, which answers a conflict as a `Conflict` (`internal/git/git.go:387-468`).
- A spec revised after UAT has its old acceptance tests on main. kiln commits their removal on `kiln/<name>.build`
  before the build session starts, so the build session's checkout holds none, and its gate never runs them. kiln
  gates the build-alone tip against main with the same path removed, so the gate's "ran at main, but not here" check
  holds.
- Before it stacks, kiln puts the acceptance path back on the build side, as it stands at the two tips' merge base,
  in a commit of kiln's in the stack's worktree. Then the verify side's changes there merge cleanly. Merged straight,
  the removal conflicts with each test file the verify session changed, and deletes each one it left alone: on
  2026-11-12, `git merge-tree --write-tree` showed both in a scratch repository, and the put-back merge held the
  verify side's file, the untouched one, and the build side's code.
- When main moves under a landing setback, kiln merges main into both sessions' branches before the next stack. A
  conflict there goes to the session whose side holds the file.

### Who hears what

- kiln gates `kiln/<name>.build` alone first. It holds none of this spec's acceptance tests, so each failure there is
  the build session's: vet, its own tests, or another spec's acceptance test its code broke. Its report goes to the
  build session alone, and the verify session does not run.
- Once that passes, kiln gates the stack. A failure there is on the verify session's side, its tests or the harness,
  and a failing testscript prints its script's lines, so the report goes to the verify session alone. The extra gate
  costs a gate run per round: the last two builds' gates took 15 to 35 seconds.
- The verify session ends its answer to a failing stack with one line: `bugs: none`, or `bugs: ` and the numbers of
  the failing tests it finds the code at fault for, such as `bugs: 2, 4`. kiln reads only numbers of the spec's
  acceptance tests. For a bug, the build session gets the reply, and the Given, When, Then of each test it names.
  Only a failing stack produces a bug.
- kiln never guesses a missing line. A reply without its line resumes the same session once, asking for the line, and
  a second miss stops the build. A run that ended with no result runs again, up to `auditTries`, as the audit's does.
- The build session ends its reply with `cannot hold: ` and the numbers of the tests it can't meet, when there are
  any. kiln passes that reply to the verify session only once both sessions' turns have ended, so no session runs
  twice at once. The verify session answers it, and the audit's findings, with commits alone: an amendment, or fixed
  tests. Its reply goes to no session, and kiln goes on to the build-alone gate. So the two sessions never pass
  replies back and forth with no gate between.
- kiln passes each new amendment's wording to the build session in its next prompt.
- kiln reads a session's reply, and routes it, where every run of that role ends (`talk`), whether `goOn` or the loop
  started it. A reply read but not yet passed on is kept in the record, so a run kiln interrupted loses no bug and
  spends no round.
- The audit's findings go to both sessions at once. Its prompt names each harness file the build session changed.
- A stack conflict, which only the harness can hold, goes to the verify session, with both sides of each file.
- Landing sends three setbacks back today (`landWork`, `internal/kiln/build.go`). A conflict with main goes to the
  session whose side holds the file. Work that changes nothing goes to the build session. A failed gate on the rebased
  stack starts the loop again at the build-alone gate.
- Each failing gate that goes back to a session is one round, whichever gate it was. The audit's findings are not a
  round, as today.

### Paths the audit must read

- `kiln.json` gains `"reaudit"`, a list of paths, such as `["internal/scenariotest/", "internal/termtest/"]`. It is
  neither tied to `harness` nor to split: a project lists what it never wants to land unread, such as its harness,
  its migrations, or its auth code. kiln reads it from main's tip, as it reads the gate, so a build can't loosen it.
- After the gate passes, kiln compares the work's own change to those paths with what the last audit read: the diff
  from the work's base, now, against the same diff at the commit the last audit's start line names. Main's own
  changes, brought in by a rebase or a merge, never count. When they differ, the audit runs again, on the whole work,
  and its findings go back as the first audit's do. `record.Audited`, a bool today, becomes the commit the last audit
  read.
- Each audit past the first counts as a round, so `rounds` bounds the loop. Past the last round the build stops,
  and never lands a change the audit hasn't read. This holds on every route to main: a landing setback, and an
  accepted landing whose gate went back.
- In split, a verify session's change to a listed path sends the work back to the audit too.
- What showed the need: on 2026-11-12, the developer let the build session write the harness, since the audit checks
  it, and named a path list that always triggers the audit, for any project. It takes up part of `docs/later.md` "An
  audit of the fixes": an audit of the fixes to listed paths alone.
- It is an exception to `docs/practices.md`'s "Do not audit the fix for an audit finding", which the developer chose
  for paths they list.

### What a verify session asserts

- A Then never names wording, by `/shape`'s rule, but a test reads the program's output to check it. Today the
  builder picks the wording and writes the test to match: `acceptance/languages/testdata/at7.txtar` greps
  `.kiln/agents/build.md` for the exact text the build wrote there.
- The verify session asserts only the forms the spec's Technical Plan names, such as a line `kiln status` prints, and
  reads the rest loosely, such as a number or a state. `/shape` names in a split spec's Technical Plan each output
  form a Then reads. `rounds` is 3, so each wrong guess spends a third of a build's rounds.

### Sessions and roles

- `internal/kiln/contract/verify.md` is kiln's contract with the verify agent: it writes the acceptance tests from
  the spec alone, it alone amends, and it ends its reply with its `bugs:` line. A bug report names the expected state
  in the spec's words, and what the program did. `internal/kiln/contract/build.md` gains wording for a split build:
  the verify session writes the acceptance tests, and the build session writes no amendment, but ends its reply with
  its `cannot hold:` line.
- `kiln init` writes `.kiln/agents/verify.md` from `internal/kiln/defaults/`, with a "Tailor this" section from each
  preset, as `build.md`'s does today.
- `kiln hook` gains `--role`, so `Hook` and the guard in `internal/kiln/guard.go` tell the two sessions apart.
  `sandboxed`'s Forbidden line for the amendment file follows the role. The gate fails on a path written on the wrong
  side however it was written, as it does for `.kiln/` today. It reads each side's writes from the commit kiln handed
  that session, which the record keeps: main, or kiln's commit that removes a revised spec's old tests. A commit of
  kiln's is never a session's write. Today's refused check diffs from the merge base with main
  (`internal/kiln/gate.go:323-347`).
- `record` in `internal/kiln/state.go` holds one `Session`. It gains the verify session's ID, and `goOn` resumes each
  session kiln interrupted. A usage limit pauses each running session, and each resumes.
- `kiln accept` folds a split build's amendments in the verify session's clone, on `kiln/<name>.verify`, the side that
  owns the amendment file, so every later stack carries the fold. `acceptAmended` folds in `b.tree` today
  (`internal/kiln/kiln.go:620`).

### Checkouts kiln removes

- kiln removes the audit's worktree once the audit ends, in either workflow. `recount` reads that worktree after kiln
  stopped mid-audit (`lostEnd`, `internal/kiln/ledger.go:291`), so the audit's worktree stays when kiln is
  interrupted, until `recount` has read it.
- A landed, accepted, rejected or requeued build loses every checkout and branch it made. A stopped build, and one
  waiting on its branch, keep `kiln/<name>` and its worktree, and each session's checkout and branch. The summary
  names the resume command for each session. The `.main` worktree stays: the gate reuses it for every run at main
  (`atMain`, `internal/kiln/gate.go:373`).

### Files the developer writes by hand

A build writes nothing under `.kiln/` but its amendment. It names these in its last reply, as spec 7's build did
for `246f158`:

- `.kiln/agents/verify.md`, tailored to kiln, and `.claude/agents/verify.md` if it is linked.
- `.kiln/kiln.json`'s new keys, if kiln builds itself in split, and `reaudit`, if kiln lists its harness there.
- `.kiln/skills/shape/SKILL.md` and `template.md`: a split spec's Technical Plan names each output form a Then reads.

### What the plan breaks

- `record.Session`, `goOn`, `start` and `launch` in `internal/kiln/build.go`, which assume one session in a
  worktree.
- `settings` in `internal/claude/settings.go`, which denies no read.
- `Hook` in `internal/kiln/guard.go`, its test `TestGuard` (`internal/kiln/rules_test.go:24`), the hook command
  string in `launch`, and `kiln hook`'s flags in `cmd/kiln/main.go`.
- `sumUp`'s one worktree and one resume command, which `README.md` step 3 and the package doc describe.
- `kiln reject` and `kiln queue`, which remove one branch and one worktree. `acceptAmended`, which folds in the
  build's worktree.
- The `audited`, `gateFailed`, `conflicts` and `unchanged` prompts, sent to one session today, and the `auditing`
  prompt, which names no harness file.
- `internal/git`, which gains a clone, a fetch from it and its removal. Its package doc says "worktrees for builds".
- The `internal/kiln` package doc and `README.md`, which say a build runs in a worktree kiln makes, and which
  worktrees kiln keeps.
- `judge` and `verdict` in `internal/kiln/gate.go`, which gate one commit per round.
- `kiln tui`, which reads the ledger again each second and grows the running run. Two runs at once, of two roles, is
  a case no test of `internal/tui` holds.
- `internal/kiln/defaults/skills/shape/SKILL.md` and `template.md`, which say a build writes test N's code, and
  `internal/kiln/presets/go/build-tests.md` and `internal/kiln/presets/python/build-tests.md`, which tell the build
  agent to write the acceptance tests. Under split, the verify agent's file says it, and the build agent's contract
  says the verify session writes them.
- The package doc's preset and agent paragraphs (`internal/kiln/kiln.go:34-48`), `README.md`'s `.kiln/` tree and
  its lines on "the build and audit agents", and `internal/scenariotest`'s seeded agents, which hold no verify agent.
- `docs/later.md`: "A project's own workflow" says kiln keeps two roles, and "Two designers and a judge" holds the
  open checkout problem this spec solves.
- `config` in `internal/kiln/gate.go`, which spec 9, gauges, changed in `503a0fd`.
- `record.Audited` and `findings` in `internal/kiln/build.go`, which run the audit once per build, and `auditDid` in
  `internal/kiln/stats.go`, whose summary line names no second audit.
- `docs/practices.md` "Do not audit the fix for an audit finding", which gains the exception, and `docs/later.md` "An
  audit of the fixes", which this spec takes up in part. The package doc and the README, which say the audit runs
  once.

## Alternatives Considered

### The verify session runs after the build session, in the build's worktree

- **Pros**: no stack and no second checkout. The verify session reads the code it tests.
- **Cons**: the verify session sees the implementation, so its tests check what the code does, not what the spec
  says. The build takes the two sessions' time end to end.
- **Reason for Rejection**: it gives up the separation the spec exists for.

### The build session alone hears every failing gate

- **Pros**: one session per round, and no second gate.
- **Cons**: a test that guessed wrong is never fixed, so the build stops past its rounds. The report shows the build
  session the tests' code.
- **Reason for Rejection**: the developer chose the two gates on 2026-11-12, since they give the build session its own
  failures and nothing else.

### Both sessions hear every failing gate

- **Pros**: each session sees all there is, in one round.
- **Cons**: the build session reads the tests' code in the report, and may bend correct code to chase a test that
  guessed wrong.
- **Reason for Rejection**: the build session would no longer be blind to the tests.

### Prompts alone keep each session to its side

- **Pros**: no guard per role, and no paths in `kiln.json`.
- **Cons**: nothing stops a build session from writing a test to fit its code, which is the defect the split is
  for.
- **Reason for Rejection**: kiln's rule is a hook over a sentence, as `docs/practices.md` "Make misuse impossible"
  orders.

### The harness is the verify session's alone

- **Pros**: the build session can't weaken the harness the tests run through.
- **Cons**: code that changes what the harness must supply, as `77d3fe0` did, fails the build-alone gate where the
  build session may not fix it.
- **Reason for Rejection**: the split divides the work, and must not keep it from getting done. The audit reads each
  harness file the build session changed for a weakened check. The developer chose this on 2026-11-12.

### Worktrees for the two sessions

- **Pros**: kiln makes worktrees today, and a worktree adds only a checkout.
- **Cons**: every worktree shares one object store, so the build session reads the tests with one git command, and
  the hook never sees a read.
- **Reason for Rejection**: a clone of main costs 2.2 MB and a fifth of a second, and keeps each session blind to
  the other. The developer chose clones on 2026-11-12.

### A workflow the project defines in `kiln.json`

- **Pros**: one design for every later role, as `docs/later.md` "A project's own workflow" drafts.
- **Cons**: the stack, the isolation and who hears each report are kiln's rules, not settings. No project has asked
  for a third workflow.
- **Reason for Rejection**: nothing yet needs a role beyond verify.
