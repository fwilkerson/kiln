# Handoff

This note carries the work on kiln, practices and odh into a new session. It holds only what is open, and the facts
no file holds. Git holds what was done, so drop each item once it lands.

## Where things stand, on 2026-11-14

- `~/github.com/fwilkerson/practices` is new, local and never pushed. It holds the guide, `claim`, and the commands
  `guide`, `health`, `size` and `style`, copied from odh at `8bc3289`. Its README says how a project uses it.
- kiln builds on practices. `go.mod` names the checkout by its full path, with no version. `CLAUDE.md` loads
  `docs/guide.md`, and `TestGuide` holds that copy to `go tool guide`.
- kiln's `code-health` monitor scores with `go tool health`, three files a day, and lands without `kiln accept`.
  CodeScene stays as the gauge, and in `CLAUDE.md` "Code Health".
- The launchd job `dev.kiln.monitor.56fea50276d3ef5d` runs `kiln monitor run` at minute 24 of each hour.
- `~/github.com/fwilkerson/odh-trial` is a clone of odh at `6541e13`, with no remote. `kiln init` ran there, and its
  `.kiln/` is tailored. No session has written to odh itself.

## Next

1. **Read the two monitor runs of 2026-11-15.** `uat` is due at 12:24. It folds `uat/split/`, and its fix waits
   for `kiln accept uat`. `code-health` is due at 13:24, its first run on `go tool health`. Its first files are
   `internal/kiln/guard.go`, `arch_test.go` and `internal/kiln/agent_test.go`. Both are the first sandboxed sessions
   to build against the practices checkout: one that can't read it fails `go test`, and that is a kiln gap. Read
   the refactor against the guide's "What a score is worth". Then spec 11, monitors, is yours to accept.
2. **Trial odh on kiln**, in `odh-trial`. Run `/shape hand-over`, approve the spec, then
   `kiln queue hand-over && kiln run`. The slice is the counter's hand-over control: `order.HandOver` exists, and
   nothing in `web` calls it. Watch for these, none of which has run yet:
    - whether the build session can spawn `prose` and `judge` as subagents;
    - whether its two designers can write their clones under `~/Library/Caches/odh-trial-designs`;
    - whether odh's hook, `go run ./tools/style` after each edit, runs in the session;
    - which agent the audit ran, and whether its findings read as odh's own audit's would.
3. **Write a kiln starter spec for each gap** the trial could not close by configuration. The developer wants kiln
   to stay at build, verify and audit, and a project to add its own steps as subagents.
4. **Convert odh**, in a session in odh:
    - Take practices: the `require`, `replace` and `tool` lines, then `internal/claim` gives way to
      `practices/claim`, and `tools/health`, `tools/size` and `tools/style` to `go tool`. `style_test.go`, the hook
      in `.claude/settings.json`, `CLAUDE.md` and the `judge` agent name those tools.
    - Load the guide as the practices README says. `docs/practices.md` keeps only odh's own rules, such as its
      Traps, as kiln's does.
    - Run `kiln init --preset go`, and carry over what the trial settled in `odh-trial/.kiln/`.
    - `/shape` replaces `/slice`. `/slice` reads `docs/decisions.md` "Not yet built" and `docs/research`, and
      `/shape` must be told to.
5. **Shape `measures`** in kiln, the starter that joins gauges and monitors. Today's check has two modes, one for
   main and one for the gate, only because a monitor can't name a gauge as its gate.
6. **Publish practices** once it stops churning. Each project then drops its `replace` line. A scratch install at
   `v0.1.0` from a local proxy already ran `go tool guide`, `style`, `health` and a test on `claim`.

## Found, and not yet settled

- **Agent names clash in odh.** odh has its own `audit`, `comprehension`, `critic` and `zoom-out` in
  `.claude/agents/`, so `kiln init` kept them. `/shape` runs odh's reviewers, and a build's audit runs kiln's
  `.kiln/agents/audit.md`.
- **Test names collide in odh.** The trial puts a spec's tests in `cmd/odh/at_<spec>_test.go`, beside the flow
  tests. Two specs would both define `TestAT1` in that package.
- **kiln's `docs/refactoring.md` argues with the guide.** Its recipes include the `case` list and the comparison
  as a struct, which the guide calls gaming a score. The refactor agent no longer reads it for recipes.
- **kiln's Go preset does not know practices.** A new project takes the guide and the commands by hand, from the
  practices README. Whether the preset should name the developer's module is undecided.
- **`cmd/style/layout.go` holds odh's module layout.** It acts only on paths under `modules/`.
- **`kiln status` says "never run, never ran"** for a monitor that has not run.
- **CodeScene may go.** The developer is not sure of paying for it. Its rule is in `CLAUDE.md` "Code Health", its
  gauge in `.kiln/kiln.json`, and its recipes in `docs/refactoring.md`.
- `languages` and `monitors` wait for UAT in kiln, and `talk` is a starter.
