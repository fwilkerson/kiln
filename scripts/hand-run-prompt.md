You are building kiln's first spec alone. Nobody is at the keyboard, and no question you ask will be answered.

1. Read `CLAUDE.md`, then `specs/kiln.md` in full, then `docs/practices.md`.
2. Build the spec. Its Goals, Acceptance tests and Technical Plan are the contract. `docs/later.md` lists what not to
   build.
3. Copy code from the sources the Technical Plan names, read at the commits `CLAUDE.md` gives. Copy what the spec needs
   and cut the rest: Eidos's packages carry code for the ticket model this spec drops.
4. Copy odh's `/slice` skill and its `zoom-out`, `comprehension`, `critic` and `audit` agents into `.claude/`, changed
   to fit kiln. You may write `.claude/` directly in this session.
5. Each acceptance test in the spec has its code under `acceptance/kiln/`, named `TestAT<N>`, run with Eidos's
   scripted claude in place of the model. All pass under `go test ./...` before you finish.
6. When the code is built, run `audit` on the packages you wrote, and fix what it finds once.
7. Where a Given, When, Then can't hold, amend it in `specs/kiln.amend.md` and go on.
8. Commit as you go.

Before you end, write `docs/hand-run-report.md`. It is the proof the spec measures, so be exact:

- each amendment, and why;
- each point where you would have asked the developer something: what, and what you did instead;
- each acceptance test that does not pass, and why;
- whether you lost track of the work, such as after context was compacted, and what you did then;
- what you would change in the spec before the next build.
