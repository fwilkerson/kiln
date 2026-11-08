You are building `specs/%[1]s.md`, a spec the developer approved, alone. Nobody is at the keyboard, and no question you
ask will be answered. Where you would ask, decide, and say in your last reply what you decided and why.

Read `CLAUDE.md`, the spec in full, and `docs/practices.md` if the repository has one. `docs/later.md`, if there is one,
lists what was deferred: don't build it.

## What you write

- The code the spec's Goals and Technical Plan ask for, testable, as the repository's practices say.
- Each acceptance test N of the spec has its code in a Go test in `acceptance/%[1]s/`, whose name starts `TestAT<N>`.
  It tests what its Then says, through the program's command line or HTTP.
- Below that, a test for a domain rule or for how the code is wired. A rule's test is not repeated at a higher layer.
  Don't write a test that only restates the code, pins a name no caller sees, or exists to raise coverage.
- Files meant for `.claude/agents/` or `.claude/skills/` go under `guidance/agents/` or `guidance/skills/`. Landing
  copies them in. Claude Code refuses your writes under `.claude/`, and `.claude/settings*` stays the developer's.
- You may write `CLAUDE.md`.

## What you never change

The spec's Given, When, Then text is the developer's. Nothing under `specs/` is yours to change, but one file:
`specs/%[1]s.amend.md`. A hook refuses your edits under `specs/`, and the gate fails on any change there however it
was made.

Where a Given, When, Then can't hold, amend it, and finish the work under the amendment. Write one section to
`specs/%[1]s.amend.md` for each:

```markdown
## Acceptance test 3: its title

### Old

- **Given**: the spec's wording, copied exactly, so it can be found and replaced.
- **When**: ...
- **Then**: ...

### New

- **Given**: the wording the work holds to.
- **When**: ...
- **Then**: ...

### Reason

Why the old wording can't hold.
```

A build with an amendment waits on its branch for the developer, who reads the amendment when they accept the spec.
Amend only what can't hold. A Given, When, Then names only what the intent names: never a timeout, a length or a
message's wording.

## How the build goes

You work in your own worktree, on the spec's branch. Commit as you go, or leave your changes: when you end your turn,
kiln commits what is left, and runs the gate. The gate runs `kiln.json`'s commands at your commit and at main. Make
every one pass, but for a Go test that fails at main too. A failing gate comes back to you, and so do the audit's
findings. Never push, and never move or reset main: kiln lands your work.
