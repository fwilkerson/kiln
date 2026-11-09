# kiln

kiln takes a spec the developer approved to code on main, with headless Claude Code sessions. It is one Go binary,
for macOS.

## Where things are

- `specs/<name>.md`: a spec. With Status Proposed it is a plan, a starter `/slice` takes up later; build only the
  approved spec kiln names. Its Given, When, Then text is the developer's: never change it. Where one can't
  hold, write `specs/<name>.amend.md` with the old wording, the new wording and the reason, and finish the work under
  the amendment.
- `PRODUCT.md`: each spec the developer accepted, and the `git show` command that prints it as it last stood.
- `guidance/`: the agents and skills that `.claude/agents` and `.claude/skills` link to. Edit them here.
- `docs/practices.md`: how Go code is written and judged here. Read it before adding a package or an exported name.
- `docs/later.md`: what was discussed and deferred. Don't build any of it.
- `acceptance/<spec>/`: the test code for each acceptance test of a spec, one Go test named `TestAT<N>` for test N.

## Sources

Spec 1 copied code from two repositories, and `docs/later.md` still points at them. Read each at its commit, never its
working tree, and never write to either.

- Eidos at `07992d10`: `git -C ~/github.com/fwilkerson/eidos show 07992d10:<path>`.
- odh at `b978d94`: `git -C ~/github.com/fwilkerson/odh show b978d94:<path>`.

## Rules

- Tests: as `docs/practices.md` "Tests" says. Each test costs time and care; write the ones a Then, a domain rule or
  the wiring needs, and no more. An acceptance test's code tests what its Then says, through the command line. Don't
  write a test that only restates the code, pins a name no caller sees, or exists to raise coverage.
- Terminal UI: `acceptance/stats-tui/` drives `kiln tui` in a pseudo-terminal, which no sandboxed session can open,
  so `go test ./...` fails there inside a session and passes in kiln's gate. Check a screen through the model's
  `Update` and `View` in a test of `internal/tui`, and read a failing terminal test from the gate's report. Before a
  key, wait for a screen it acts on.
- Comments: as `docs/practices.md` "Comments" says. A comment inside a body says why, never what.
- Commits: [Conventional Commits](https://www.conventionalcommits.org), `type(scope): summary`, imperative and under
  72 characters. The body says why. `go test ./...` passes at each commit. Commit on main; never push.
