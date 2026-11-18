# kiln

kiln takes a spec the developer approved to code on main, with headless Claude Code sessions. It is one Go binary,
for macOS.

> You are the owner and long-term maintainer of this codebase, and its design is up to you.

## Where things are

- `.kiln/specs/<name>.md`: a spec. With Status Proposed it is a plan, a starter `/shape` takes up later; build only
  the approved spec kiln names. Its Given, When, Then text is the developer's: never change it.
- `.kiln/monitors/` and `.kiln/gauges/`: the scripts kiln's monitors' checks and gauges run, out of a fix's reach. A
  build names a new one in its last reply, for the developer to commit. `monitors/uat` is the check of the monitor
  that folds an accepted spec's tests. `monitors/docs` is the check of the monitor that reads the docs of changed
  packages against their code. `gauges/health` is the gauge of `code-health`, the monitor that refactors the files
  `go tool health` scores lowest. `monitors/gate-time` is the check of the monitor that makes the tests faster
  once the gate's times in kiln's ledger rise.
- `bugs/<name>.md`: a bug the developer filed, with what happened and what should happen. `bugs`, a monitor, takes
  the first by name each hour, as `.kiln/agents/bugfix.md` says. A build never fixes one.
- `PRODUCT.md`: each spec the developer accepted, and the `git show` command that prints it as it last stood.
- `.kiln/`: kiln's own settings, agents and specs, as `kiln init` wrote them, tailored to kiln. `.claude/agents` and
  `.claude/skills` link to `/shape`, its reviewers and the verify agent there. A session kiln runs changes nothing
  here, but for the amendment file kiln's contract gives it.
- `internal/kiln/defaults/`: what `kiln init` writes, for any project. No line there names kiln's own repository.
- `internal/kiln/presets/`: each preset's `kiln.json`, and its text for the "Tailor this" sections of the defaults'
  prompts. `kiln init` writes them with `internal/kiln/defaults/`.
- `docs/guide.md`: the guide, how Go code is written and judged across the developer's projects, loaded at the end
  of this file. It is what `go tool guide` prints, from the practices module that `go.mod` names: never edit it here.
  `TestGuide` fails while the two differ, and `go tool guide > docs/guide.md` brings it up to date.
- `docs/practices.md`: kiln's own rules, which add to the guide. Read it before adding a package or an exported name.
- `README.md`: what kiln is, and a quick start. `docs/reference.md`: each key of `kiln.json`, the agents' files, and
  what kiln records. `docs/practices.md` "Docs are the contract" says which doc holds a rule.
- `docs/one-page.txt`: kiln on one page, the source that `onepage render docs/one-page.txt docs/one-page.html`
  draws in the theme `docs/sheet/`. Render it again in the commit that changes either.
- `docs/later.md`: what was discussed and deferred. Don't build any of it.
- `uat/<spec>/`: the test code for each acceptance test of a spec not yet accepted, one Go test named `TestAT<N>`
  for test N.
- `uat/testdata/<feature>/`: the scenarios of the specs the developer accepted, filed by feature. `TestUAT` in `uat/`
  runs each.

## Rules

- Tests: as the guide's "Tests" says. Each test costs time and care; write the ones a Then, a domain rule or
  the wiring needs, and no more. An acceptance test's code tests what its Then says, through the command line. Don't
  write a test that only restates the code, pins a name no caller sees, or exists to raise coverage.
- Folded scenarios: as `docs/practices.md` "Acceptance tests" says. No session loosens or removes a check its work
  leaves true. Folding, merging or dropping a scenario moves checks, so only the `uat` monitor does it, for the
  developer to accept. What else a session may write under `uat/`, its agent file and kiln's contract say.
- Terminal UI: the scenarios in `uat/testdata/tui/` drive `kiln tui` in a pseudo-terminal, which no sandboxed session
  can open, so `go test ./...` skips them inside a session and runs them in kiln's gate. Check a screen through the
  model's `Update` and `View` in a test of `internal/tui`, and read a failing terminal test from the gate's report.
  Before a key, wait for a screen it acts on. `kiln tui`'s monitors are checked that way alone: no acceptance test
  drives them in a pseudo-terminal.
- Comments: as the guide's "Comments" says. A comment inside a body says why, never what.
- Commits: [Conventional Commits](https://www.conventionalcommits.org), `type(scope): summary`, imperative and under
  72 characters. The body says why. `go test ./...` passes at each commit. In a session the developer runs, commit each
  change to main as soon as it passes, without asking. A session kiln runs commits on its branch, as kiln's
  contract says. Never push.
- A change the developer asks for in a session they run: make it there, by direct commit. A starter spec is for work
  the developer defers, or asks to shape. Never write one in place of a change they asked for.

## The guide

@docs/guide.md
