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
- The installed kiln is built at `14cdf97`. A key that `kiln.json` gains needs `go run ./tools/install` first, or
  the launchd job's kiln refuses the file and every monitor fails.
- The launchd job `dev.kiln.monitor.56fea50276d3ef5d` runs `kiln monitor run` at minute 24 of each hour.
- odh builds on kiln since 2026-11-14. The trial's second build of `hand-over` landed, and odh's `edd0c70` merged
  the clone `~/github.com/fwilkerson/odh-trial`. odh's `.kiln/` is a fresh `kiln init`, tailored in `4777cce`:
  `/shape` replaced `/slice`, and kiln's audit and reviewers replaced odh's own. odh's `.git/kiln` is a copy of the
  clone's, so `hand-over` waits for UAT there. The developer may delete the clone once that spec is accepted in odh.
- The trial showed what a build can do in odh. It ran odh's hook, since a session loads the project's settings. It
  spawned the `prose` agent and two designers, whose clones it wrote under `~/Library/Caches`. It named a line for
  `.claude/handoff.md` in its last reply. No build has run the `judge` agent, since the two designs converged.
- `~/.claude/friction.md` logs each reply that confused or slowed the developer, in any repo. Add an entry when it
  happens, and read it before you change an instruction file.
- The developer ran `code-health` by hand on 2026-11-14, and its fix landed as `436d3dd`. It was the first fix
  under `TestHealth`, and its agent deleted the four lines of `held`. Its sandboxed session ran `go test` and
  `go tool health` against the practices checkout, so a session can read it.

## Next

1. **Read the `uat` monitor's run of 2026-11-15**, due at 12:24. It folds `uat/split/`, `uat/monitors/` and
   `uat/measures/`, and its fix waits for `kiln accept uat`. Its agent may run `.kiln/monitors/uat-lost`, whose
   temp files now honor `TMPDIR`.
2. **Write odh's behaviors as specs**, in a session in odh. The developer plans to write each behavior of the
   system odh replaces as a spec with Status Proposed. Accepted specs then show what odh covers, and Proposed ones
   show the gaps. Only the developer knows those behaviors. A second slice in odh that crosses modules confirms the
   adoption, as the developer's criteria said.
3. **Bring the rest of practices into kiln.** The developer asked for this plan on 2026-11-14, and has approved none
   of it:
    - Run `go tool inspect`. On 2026-11-14 it found about 300 things in kiln under GoLand's default profile: 113 in
      grammar, 76 mixed receivers and 72 unhandled errors. So kiln first needs a profile and a dictionary of its
      own, as odh's `.idea/inspectionProfiles` is. A monitor could then run it, if a check can run GoLand.
    - Run `prose` on a build's comments, as a subagent of the build. The trial showed that a build can spawn one.
      `prose` and `judge` are still odh's. They may now move to practices, with a command that writes them.
    - The two designs and the judge are deferred in `docs/later.md`. The trial showed that a build can run two
      designers, as odh's `.kiln/agents/build.md` tells it to. No build has run the judge.
    - kiln's one research report opens as odh's do, and lacks "What would overturn this". When kiln gains a second
      topic, move odh's `TestReports` into `repotest`.
4. **Publish practices** once it stops churning. Each project then drops its `replace` line, and odh's Dockerfile
   its `go mod edit`. A scratch install at `v0.1.0` from a local proxy already ran `go tool guide`, `style`,
   `health` and a test on `claim`.

## Found, and not yet settled

- **Test names collide in odh.** A spec's tests are in `cmd/odh/at_<spec>_test.go`, beside the flow tests, and each
  spec numbers them from `TestAT1`. So a second spec cannot wait beside the first, and nothing renames an accepted
  spec's tests. The developer is weighing a `uat` folder in odh, with a package for each spec.
- **A build's record of a rejected design does not reach main.** kiln lands a build as one commit, with a message
  of its own. odh's build agent runs two designs and a judge, and their verdict reaches only its last reply.
- **A build cannot run `go tool inspect`**, which needs GoLand and the main checkout. In odh it found three sentences
  the `hand-over` build wrote, after the landing.
- **kiln's defaults trip GoLand's grammar check**, at two sentences of `/shape` and one of the audit. odh reworded
  its copies in `4777cce`.
- **kiln's Go preset does not know practices.** A new project takes the guide and the commands by hand, from the
  practices README. Other codebases will adopt kiln, so the preset should not name the developer's module.
- **The guide trips GoLand's grammar check in odh**, five times, each at a sound sentence. odh's inspection profile
  skips its copy of the guide. practices has no profile of its own.
- **A change to the guide fails `TestGuide` in each project** until its copy is refreshed. So an older commit of kiln
  or odh fails that test against today's practices checkout.
- **`kiln status` says "never run, never ran"** for a monitor that has not run.
- `languages` waits for UAT in kiln. Its spec names `kiln run` at two lines, and stays so until it is accepted: a
  change to it would let `kiln queue` take it again. `talk` is a starter.
