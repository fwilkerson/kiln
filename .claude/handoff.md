# Handoff

This note carries the work on kiln, practices and odh into a new session. It holds only what is open, and the facts
no file holds. Git holds what was done, so drop each item once it lands.

## Where things stand, on 2026-11-14

- `~/github.com/fwilkerson/practices` is local and never pushed. It holds the guide, `claim`, `repotest`, and the
  commands `guide`, `health`, `inspect`, `size` and `style`. Its README says how a project uses it.
- kiln and odh both build on practices. Each `go.mod` names the checkout by its full path, with no version. Each
  `CLAUDE.md` loads `docs/guide.md`, and `TestGuide` holds that copy to the module. Each root test file calls
  `repotest`.
- odh took practices in `0664f73` and `fa37584`. Its `docs/practices.md` keeps only odh's own rules. `tools/layout`
  stays odh's, since the layout knows odh's framework. Its Dockerfile drops the practices lines from `go.mod`, since
  the checkout is outside the image.
- CodeScene left kiln in `b784746`. `measures` was accepted in `d3bd799`. Since `343b49c`, `code-health` names the
  gauge `health`, `.kiln/gauges/health`, in place of a check. kiln reads that gauge after each landing too, so a
  landing that touches a file in `held` shows ✗ 9 until the monitor clears the file.
- kiln's `code-health` monitor refactors three files a day, as its prompt says, and lands without `kiln accept`.
  `TestHealth` holds each file's score, and the build and refactor agents delete a file's line from `held` at 10.
- The installed kiln is built at `eb188c8`. A key that `kiln.json` gains needs `go run ./tools/install` first, or
  the launchd job's kiln refuses the file and every monitor fails.
- The launchd job `dev.kiln.monitor.56fea50276d3ef5d` runs `kiln monitor run` at minute 24 of each hour.
- `~/github.com/fwilkerson/odh-trial` is a clone of odh at `6541e13`, with no remote. So it holds odh's old tools,
  and not practices. Its `354ebb0` tailors `/shape`, its three reviewers and the build agent to odh.
- The developer ran `code-health` by hand on 2026-11-14, and its fix landed as `436d3dd`. It was the first fix
  under `TestHealth`, and its agent deleted the four lines of `held`. Its sandboxed session ran `go test` and
  `go tool health` against the practices checkout, so a session can read it.

## Next

1. **Read the `uat` monitor's run of 2026-11-15**, due at 12:24. It folds `uat/split/`, `uat/monitors/` and
   `uat/measures/`, and its fix waits for `kiln accept uat`. Its agent may run `.kiln/monitors/uat-lost`, whose
   temp files now honor `TMPDIR`.
2. **Trial odh on kiln**, in `odh-trial`. The developer runs `/shape hand-over` there, approves the spec, then runs
   `kiln queue hand-over && kiln run`. The slice is the counter's hand-over control: `order.HandOver` exists, and
   nothing in `web` calls it. After UAT, read the build's streams against the criteria and the list below.

   The developer settled these criteria on 2026-11-14:
    - Must: the build lands, and asks nothing outside the spec.
    - Must: the docs land with the code, as the spec's Where It Lands names.
    - Must: odh's bar holds at the landed commit, by the gate and by `go tool inspect`.
    - Must: staff hand over a pickup at the counter, in a browser.
    - Weighed: the audit finds what odh's own `audit` agent finds on the same packages.
    - Weighed: the tests catch a bug planted in the handler and one in the rule, and test no rule twice.
    - Weighed: `prose` and the two designs ran in the build.
    - Recorded only: cost, time and rounds. The subscription is use it or lose it.
    - One slice is enough to adopt, if the musts hold. A second slice in odh, one that crosses modules, confirms it.
    - A step the sandbox refuses does not fail the trial. It becomes a kiln starter spec. Adoption waits only if the
      step has no place outside the build.
    - Weighed: how many of the developer's turns in `/shape` were toil, with no cap. odh's collaboration report
      defines a toil turn. The guest-cart design took 25 turns.

   Watch for these, none of which has run yet:
    - whether the build session can spawn `prose` and `judge` as subagents;
    - whether its two designers can write their clones under `~/Library/Caches/odh-trial-designs`;
    - whether odh's hook, `go run ./tools/style` after each edit, runs in the session;
    - which agent the audit ran, and whether its findings read as odh's own audit's would;
    - what the build does with a line meant for `.claude/handoff.md`, which no build can write.
3. **Write a kiln starter spec for each gap** the trial could not close by configuration. The developer wants kiln
   to stay at build, verify and audit, and a project to add its own steps as subagents. Two gaps are known before
   the run. The inspection needs GoLand and the main checkout, so the trial's build skips it. odh keeps its open
   work in `.claude/handoff.md`, which no build can write.
4. **Finish odh's conversion**, once the developer adopts kiln, in a session in odh:
    - Run `kiln init --preset go`, and carry over `odh-trial/.kiln/`. Its build agent names `go run ./tools/style`,
      and odh now runs `go run ./tools/layout`, then `go tool style`. Its verify agent names `internal/claim`, which
      is now the practices module's `claim`.
    - `/shape` replaces `/slice`, and `.kiln/specs/` replaces `docs/designs/`. odh's `CLAUDE.md` "Review agents" and
      "Designs", and its `docs/practices.md` "Designs", then say what kiln does.
    - Link `/shape`'s reviewers into `.claude/agents/` in place of odh's three, as the trial did.
    - Add odh's own sections to the spec's template: Non-Functional Requirements, and the judge's table. Where It
      Lands is in the trial's template.
    - Then write each behavior of the system odh replaces as a spec with Status Proposed. Accepted specs show what
      odh covers, and Proposed ones show the gaps. Only the developer knows those behaviors.
5. **Bring the rest of practices into kiln.** The developer asked for this plan on 2026-11-14, and has approved none
   of it:
    - Run `go tool inspect`. On 2026-11-14 it found about 300 things in kiln under GoLand's default profile: 113 in
      grammar, 76 mixed receivers and 72 unhandled errors. So kiln first needs a profile and a dictionary of its
      own, as odh's `.idea/inspectionProfiles` is. A monitor could then run it, if a check can run GoLand.
    - Run `prose` on a build's comments, as a subagent of the build. The trial shows whether a build can spawn one.
      `prose` and `judge` are odh's until then. They then move to practices, with a command that writes them.
    - The two designs and the judge stay deferred, as `docs/later.md` says, until the trial shows a build can run
      them.
    - Keep a friction log for `/shape` sessions, as the guide now says.
    - kiln's one research report opens as odh's do, and lacks "What would overturn this". When kiln gains a second
      topic, move odh's `TestReports` into `repotest`.
6. **Publish practices** once it stops churning. Each project then drops its `replace` line, and odh's Dockerfile
   its `go mod edit`. A scratch install at `v0.1.0` from a local proxy already ran `go tool guide`, `style`,
   `health` and a test on `claim`.

## Found, and not yet settled

- **Test names collide in odh.** The trial puts a spec's tests in `cmd/odh/at_<spec>_test.go`, beside the flow
  tests. Two specs would both define `TestAT1` in that package.
- **kiln's Go preset does not know practices.** A new project takes the guide and the commands by hand, from the
  practices README. Other codebases will adopt kiln, so the preset should not name the developer's module.
- **The guide trips GoLand's grammar check in odh**, five times, each at a sound sentence. odh's inspection profile
  skips its copy of the guide. practices has no profile of its own.
- **A change to the guide fails `TestGuide` in each project** until its copy is refreshed. So an older commit of kiln
  or odh fails that test against today's practices checkout.
- **`kiln status` says "never run, never ran"** for a monitor that has not run.
- `languages` waits for UAT in kiln. `talk` and `fire`, the rename of `kiln run`, are starters.
