# kiln

kiln takes a spec the developer approved to code on main, with headless Claude Code sessions. It is one Go binary,
for macOS.

## Where things are

- `specs/<name>.md`: an approved spec. Its Given, When, Then text is the developer's: never change it. Where one can't
  hold, write `specs/<name>.amend.md` with the old wording, the new wording and the reason, and finish the work under
  the amendment.
- `docs/practices.md`: how Go code is written and judged here. Read it before adding a package or an exported name.
- `docs/later.md`: what was discussed and deferred. Don't build any of it.
- `acceptance/<spec>/`: the test code for each acceptance test of a spec, one Go test named `TestAT<N>` for test N.

## Sources

Spec 1 copies code from two repositories. Read each at its commit, never its working tree, and never write to either.

- Eidos at `07992d10`: `git -C ~/github.com/fwilkerson/eidos show 07992d10:<path>`.
- odh at `b978d94`: `git -C ~/github.com/fwilkerson/odh show b978d94:<path>`.

## Rules

- Tests: as `specs/kiln.md` "How a build writes tests" says. Each test costs time and care; write the ones a Then, a
  domain rule or the wiring needs, and no more.
- Comments: as `docs/practices.md` "Comments" says. A comment inside a body says why, never what.
- Commits: [Conventional Commits](https://www.conventionalcommits.org), `type(scope): summary`, imperative and under
  72 characters. The body says why. `go test ./...` passes at each commit. Commit on main; never push.
