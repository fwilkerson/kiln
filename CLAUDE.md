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
- Terminal UI: `acceptance/stats-tui/` and `acceptance/tui-reading/` drive `kiln tui` in a pseudo-terminal, which
  no sandboxed session can open, so `go test ./...` fails there inside a session and passes in kiln's gate. Check a
  screen through the model's `Update` and `View` in a test of `internal/tui`, and read a failing terminal test from the
  gate's report. Before a key, wait for a screen it acts on.
- Comments: as `docs/practices.md` "Comments" says. A comment inside a body says why, never what.
- Commits: [Conventional Commits](https://www.conventionalcommits.org), `type(scope): summary`, imperative and under
  72 characters. The body says why. `go test ./...` passes at each commit. Commit on main; never push.

## Code Health

The CodeScene MCP server, registered as `codescene`, scores how maintainable a file is as Code Health, from 1 to 10.
When its tools are available:

- Each file a change touches reaches Code Health 10.0. A score of 9 is not done. Leave files the change doesn't touch
  alone.
- To refactor, start from `code_health_review`, change the code in small steps, and after each one run
  `code_health_score` to confirm the score rose.
- Before each commit, run `pre_commit_code_health_safeguard` on the uncommitted files. Where it reports a regression,
  or a touched file stays below 10.0, run `code_health_review` on that file and refactor it. Don't call the work done
  while either stands, unless the developer accepted it.
- To explain why Code Health matters, use `explain_code_health` and `explain_code_health_productivity`.
- `analyze_change_set` checks a branch against its base before a pull request. kiln commits on main and never pushes,
  so it has no use here.
