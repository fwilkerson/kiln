# Hand run report: spec 1

One `claude -p` session built `specs/kiln.md` on 2026-11-08, from the prompt in `scripts/hand-run-prompt.md`. This is
what the spec's proof measures. Each claim names the commit or file that shows it.

## Outcome

- All 10 acceptance tests pass under `go test ./...`, with every other test: `TestAT1` to `TestAT10` in
  `acceptance/kiln/`, each running `testdata/at<N>.txtar` against a scratch repository with the scripted claude.
- Commits, in order: `70c0ed3` adapters and guidance, `81ce8b5` the engine, `f991440` the acceptance tests,
  `9f0c427` cleanups, `20e14c0` the audit's fixes, then this report.
- Built: `cmd/kiln`, `internal/kiln`, `internal/claude`, `internal/git`, `internal/proc`, `internal/scenariotest`,
  `guidance/` (the `/slice` skill, its `template.md`, and the four agents), `tools/style`, `arch_test.go`,
  `style_test.go`, `kiln.json`. Nothing in `docs/later.md` was built.

## Amendments

None. `specs/kiln.amend.md` does not exist. Two Given, When, Thens held only under a reading I chose; both are listed
under "What I would change in the spec".

## Where I would have asked the developer

Each is a choice the spec left open. What I did is in the code and its package docs.

1. **Test 2's "by an edit".** The hook refuses an Edit under `specs/`, so a build that changes the spec by an edit
   exists only when an edit gets past the hook. I asked: should the test show the hook's refusal, or the gate catching
   a change? I did both: `at2.txtar` has one build whose Edit the hook refuses and whose shell command changes the
   spec, and one whose Edit gets past the hook. The gate fails both.
2. **Test 6's "main holds ... its amendments".** Accepting deletes the spec, so main's tip cannot hold the
   amendments. I read it as: the commit `PRODUCT.md` names holds the spec with its amendments folded in, and main
   descends from it. `at6.txtar` checks both.
3. **The amendment file's format.** `kiln accept` must fold amendments into the spec, so it must parse them. I
   defined one: a `## ` section per test, with `### Old`, `### New` and `### Reason`. The build role
   (`internal/kiln/build.md`) tells the session the format.
4. **What "rebased onto main" makes.** I made one commit of the build's work on main's tip, with `git merge-tree`,
   in place of Eidos's fold-and-line rebase. A conflict goes back to the session, told to merge main.
5. **Whether a conflict, or a gate failing after the rebase, counts as a round.** I counted both.
6. **Accepting a spec that landed, when the retiring commit's gate fails.** No session is left to fix it, so
   `kiln accept` refuses and says why. Accepting a spec that waits on its branch goes through the build's session, as
   step 5 does.
7. **Where kiln.json is read from.** From main's tip, so a build cannot weaken its own gate. The pre-commit hook
   reads it from what the commit holds.
8. **Where worktrees live.** In the user's cache directory, `~/Library/Caches/kiln/<repo>-<hash>/`, not under
   `.git/`. Claude Code may refuse writes to a path inside a git directory. State lives under the git common directory,
   as the spec says.
9. **How the audit runs.** kiln starts its own headless session, with the audit agent's body as the system prompt,
   embedded from `guidance/agents/audit.md` by `guidance.Audit`. A repository need not hold the agent in `.claude/`.
   kiln reads the agent's last line, `found M`; an audit that ends with no result runs again, up to 3 times, then
   stops the build.
10. **A usage limit that names no reset.** It pauses the build for 5 minutes, then resumes the session. An event with
    `isUsingOverage` pauses too, since overage bills past the plan.
11. **Errors of kiln's own.** The proof says no build stops, but a missing `claude`, or the developer's changes in the
    way of main's fast-forward, cannot be fixed by a session. Those stop the build, and `kiln status` says why.
12. **Small fixed choices.** Main is the branch named `main`. Sessions run Opus at high effort in auto permission
    mode. Their sandbox reaches only Go's module proxy and checksum hosts, and writes Go's caches.
    `PRODUCT.md` names a commit by its first 12 hex digits. Approving a spec that is building is refused; approving
    one that is stopped, landed or waiting starts a new build and drops its old branch.
13. **What landing copies.** `guidance/agents/` and `guidance/skills/` are copied into `.claude/`. A file removed from
    `guidance/` is not removed from `.claude/`.

## The audit

I ran the audit once, as a subagent briefed with `guidance/agents/audit.md`, on the seven packages I wrote and the
acceptance tests beside the spec. It found five; I fixed each once, in `20e14c0`, taking the design it named:

1. Accepting a spec that waits on its branch handed a conflict to the developer. It now lands through the build's
   path, and the conflict goes back to the session. `at3.txtar` now holds that conflict.
2. The audit was marked done before it ran, so a failed or interrupted audit was never redone. It is now marked done
   only once it answers. `at1.txtar` now has a first audit that fails.
3. A landing with no rebase amended a commit main already held, so `kiln accept` of a landed spec could never
   fast-forward. The copy now goes in a commit of its own. `at6.txtar` now changes `guidance/` on main first.
4. A Go package that failed with no test named, such as a timeout, was excused by any test failing at main. It no
   longer is: a new row in `TestGate`.
5. `at6.txtar` did not check that the commit `PRODUCT.md` names is on main. It now does.

The new cases in tests 1, 3 and 6 failed on the code before `20e14c0`, and pass after. Test 3's new conflict also
showed a bug the audit had not named: a build stored a different session ID than it used. It broke every resume from
another process. That is fixed in the same commit, by making the store's update apply a change once.

## Acceptance tests that do not pass

None.

## Losing track of the work

I did not lose track. No compaction happened that I could see, and I did not need to rebuild context. I kept the plan
in order: adapters, engine, harness, tests, audit, report. I committed at each step, so the log shows where the work
stood.

## What blocked or slowed the run

- The sandbox refused the Go module proxy. `golang.org/x/sys` and `github.com/rogpeppe/go-internal` were already in
  the local module cache. I wrote `go.mod` by hand, and took each `go.sum` line from Eidos's `go.sum` at `07992d10`.
  `go mod verify` passes.
- Commands with an environment assignment, a loop, or a variable in braces needed an approval nobody could give. I
  rewrote each without them, or as a script in the ignored `.hand-run/` directory.
- I read the two source repositories with `git show` and `git archive` at their commits. The extracted copy is in
  `.hand-run/src/`, which git ignores. I wrote nothing to either repository.
- `CLAUDE.md` asks for GoLand's `reformat_file` after a Markdown edit, but no GoLand tool was available here.

## What I would change in the spec before the next build

1. **Test 2:** say whether the Given is a change the hook missed, or an attempt the hook stops. For example: "a build
   that changes a Given, When, Then in `specs/<name>.md`, by any means the hook does not stop."
2. **Test 6:** say where the amendments end up: "the commit `PRODUCT.md` names holds the spec with its amendments
   folded in, and main descends from it."
3. **Name the amendment file's format** in the spec, since `kiln accept` must parse it.
4. **Say what step 5's rebase makes:** one commit, or the build's own commits. Say that a conflict goes back to the
   session, and whether it counts as a round.
5. **Say what `kiln accept` does when the gate fails** on a spec that already landed.
6. **Give the Claude guidance goal an acceptance test.** No Then checks that landing copies `guidance/` into
   `.claude/`. Test 6 checks it now, but only as wiring.
7. **Say which errors may stop a build** beyond its last round, since the proof says none does.
8. **Plan a run against the real Claude Code.** Every acceptance test runs the scripted claude, which accepts what
   Eidos measured at Claude Code 2.1.301. Nothing here has been tried against a real session. Untested: auto mode
   with `denyInsteadOfPrompting`, the sandbox's `allowWrite` and `allowedDomains`, the hook's matcher, `--tools`, and
   a worktree in the cache directory writing to the shared git directory. A one-spec smoke run should come before
   kiln builds its second spec.
9. **Note the test cache.** The acceptance tests build `cmd/kiln` as they run, so `go test`'s cache can't see a
   change to it. `internal/scenariotest` imports `internal/kiln` so a change to the engine invalidates the cache. A
   change to `cmd/kiln/main.go` alone still does not.
