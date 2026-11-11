# Spec 8: Projects in other languages

Status: Proposed

<!-- Drafted from concepts_brief.md, which was built from the starter that .kiln/specs/languages.md held at ac21958.
-->

## Overview

After this spec, a developer points kiln at a Python project that uses uv, pytest, ruff and ty, and kiln builds it as
it builds a Go project today. `kiln init --preset python` writes that toolchain's gate, hosts and writable paths. The
gate does not count a test against a build when the same test also fails on main. The audit reads every build that
changes a file outside `.kiln/`, whatever its language.

TypeScript with Playwright is the next spec, and this one builds nothing for it. The gate reads test results from
JUnit XML, a format Playwright also writes; the next spec checks how Playwright's names and retries map onto it.

## The Problem

Three parts of kiln only work for Go today.

1. **The gate's subtraction.** kiln runs the gate on the build's commit and on main's tip. A test that fails on both
   does not count against the build, so a flaky end-to-end test or a broken main does not block every worktree
   waiting to land. kiln learns which tests failed only by reading `go test`'s output (`failedTests` in
   `internal/kiln/gate.go`). A failed `pytest` run therefore counts as one failure of the whole command, and it is
   never excused, even when main fails it in the same way.
2. **The audit.** `goPackages` in `internal/kiln/build.go` keeps only the directories that hold changed `.go` files.
   A build that changes no `.go` file gets no audit at all, and the developer requires the audit.
3. **`kiln init` and kiln's own wording.** `kiln init` writes Go's gate commands, hosts and cache paths, and build
   and audit prompts that say gofmt, `go vet`, "package" and `TestAT<N>`. kiln's own contract and prompts, which no
   project can change, say "Go test" and "packages" (`internal/kiln/contract/build.md`,
   `internal/kiln/contract/audit.md`, the `auditing` prompt in `internal/kiln/build.go`). The `auditing` prompt also
   fixes where acceptance tests live: `acceptance/<spec>/`.

## Goals

- A uv project with pytest, ruff and ty can go from `kiln init` to a landed build with no change to kiln's code and
  no hand edit of `kiln.json`, on a machine where uv finds a Python already installed. A project with no tests yet
  passes the gate, as a Go project with no tests does.
- The gate excuses a pytest test that also fails on main, in the same way it excuses a Go test today. It learns the
  failed tests from JUnit XML, a common file format that lists each test and whether it failed, written to a path
  kiln gives each gate command in `KILN_JUNIT`.
- A crash is never excused: unreadable XML, a non-zero exit with no failed test listed, or a collection or import
  error each fail the gate, as a Go package that fails to build does today. Nor is a run cut short: a check is
  excused only when every test main ran also ran at the build's commit.
- The audit runs on every build that changes a file outside `.kiln/`, scoped to the directories of the changed files.
  kiln's code holds no list of source extensions, so a project in another language gets an audit with no kiln
  release.
- kiln's Go code and contract carry no Go-only wording and no longer name `acceptance/<spec>/`. Each project's
  prompts say where its acceptance tests live.
- `kiln init --preset go` and `kiln init --preset python` write the two presets. With no flag, init writes Go's
  defaults exactly as it does today.

## Non-goals

- A TypeScript/Playwright preset, and how kiln reads Playwright's retries and test names. That is the next spec.
- Excusing lint or type errors that main also has. A linter or type checker is judged by its exit code alone, as
  `go vet` is today.
- Detecting the project's language without asking.
- Telling an assertion failure from a crash beyond the "broken" rule above (`docs/later.md`).
- Supporting Python toolchains other than uv. A project that uses another one edits its `kiln.json` after
  `kiln init`.
- An interview in `kiln init`. The developer edits the `kiln.json` that init writes (Decision 2).
- Keeping Python caches out of a project whose `.gitignore` lacks `__pycache__/`. Such a project can commit bytecode
  files (Decision 7).

## Open Questions

None. Each decision below keeps its evidence and the option it rejected.

## Decisions

### Decision 1: an `<error>` counts as a test's failure only in setup or teardown

A check ran pytest 9.2.0 under uv 0.13.7 on a test file that imports a missing module. The XML holds
`<testcase classname="" name="tests.test_bad"><error message="collection failure">`. A setup error looks like
`<testcase classname="tests.test_ok" name="test_setup_err"><error message="failed on setup with ...">`. The brief
says kiln counts `<testcase>` elements and that a setup error on a named test counts as that test's failure. Read
that way, a broken import that main also has would be excused, which breaks "a crash is never excused."

Decided: an `<error>` counts as its test's failure only when its message starts with `failed on setup with` or
`failed on teardown with`, which is pytest's wording. Any other `<error>` makes the command broken. The alternative,
treating an empty `classname` as broken, depends on a pytest detail as specific as this one and lets an unknown kind
of error be excused. Decided because under this rule an error kiln does not recognize costs a round, while under the
alternative it can land a crash.

### Decision 2: `kiln init` takes the preset as a flag, `kiln init --preset python`, and asks nothing

The brief offers presets through an interview in a terminal. The flag alone meets every Goal. The interview would add
a terminal check, a path that overrides a preset's lists and a line-editing protocol, and only kiln's gate can test
the terminal part, since a build session cannot open a pseudo-terminal. All of that would edit three lists that the
developer can edit in the `kiln.json` init writes and then commits.

Decided: the flag, with no flag meaning Go, and no interview. An interview can come with a third preset or real use.

### Decision 3: the Python preset puts acceptance tests in `tests/acceptance/test_<spec>.py`

Go's preset keeps `acceptance/<spec>/` with `TestAT<N>`. The same layout in pytest gives each spec's directory test
files with the same basenames, and pytest's default import mode fails on two test modules with one basename unless
each directory has an `__init__.py`.

Decided: one file per spec, `tests/acceptance/test_<spec>.py`, with a function `test_at<N>` for test N. pytest finds
it with no extra settings, and a gate failure names the spec and the test.

### Decision 4: a preset fills markers in the shared prompts' "Tailor this" sections

Three ways to fill them:

- (a) Each preset holds its own whole `agents/build.md` and `agents/audit.md`. Each preset's prompt is one file, but
  the text the presets share is written twice, and an edit to it can reach one preset and miss the other.
- (b) The shared prompts hold a marker in each tailored section, and each preset holds only the text for those
  markers. The shared text is written once. A marker left unfilled or misspelled shows up only in the written file,
  so a unit test has to check that every preset fills every marker.
- (c) The shared prompts are Go `text/template` files, and a preset is a struct of strings. Same as (b), and a
  missing field fails at `kiln init` instead of in the file, but the template syntax sits in an embedded file that is
  otherwise plain Markdown.

Decided: (b), because the next spec adds a third preset, and under (a) every edit to the shared text then has to be
made three times.

### Decision 5: this spec changes the test code of two accepted tests

The contract's last line, `read N packages, found M`, becomes `read N directories, found M`.
`acceptance/project-roles/testdata/at3.txtar` line 18 greps the old wording. Test 3's Then ("the build agent is told
the file's instructions, then kiln's contract") does not name that line, so the grep is test code, not the
developer's text.

Decision 6 also reaches spec 3's test 5, `acceptance/stats-tui/testdata/at5.txtar`. It lands 12 builds that change
only files at the repository's top, and its `claude.json` scripts no audit, because today those builds get none.
Under Decision 6 each gets an audit. Its Then names cost, failed calls and rounds per build, not the audit, so the
missing audit steps are test code too.

Decided: change the contract's wording and that one grep, and add to test 5's `claude.json` one audit step per build,
each ending `read 1 directories, found 0` with no cost, so its cost, failed-call and round counts do not change. Say
so in the commit. The alternative keeps "packages" in kiln's contract for a Python audit, which is the Go-only
wording the Goals remove.

### Decision 6: the audit reads every build that changes a file outside `.kiln/`

The first draft kept a fixed list of source extensions, `.go` and `.py`, in kiln's code. A TypeScript project, or a
project that edits `kiln.json` for another toolchain, would then get no audit until a kiln release adds its
extension. Two other options:

- (a) Audit every build that changes a file outside `.kiln/`, and give the audit the directories and the files. The
  project's audit prompt says what to skip.
- (b) An optional `sources:` key in `.kiln/agents/audit.md`'s frontmatter, written by each preset. Spec 7 has kiln
  ignore unknown frontmatter keys, so an older kiln would still read the file.

Decided: (a), because the audit is required. Under (b), a missing or misspelled key silently skips the audit. The
cost is that a build that changes only documentation now pays for an audit.

### Decision 7: kiln does nothing to keep caches out of a build's commit

The brief warned that `.venv/` and `__pycache__/` would land on main, because kiln commits every untracked file
(`add --all` in `Commit`, `internal/git/git.go`). A check with uv 0.13.7 (see the tool facts) found that only
`__pycache__/` can, and the `.gitignore` that `uv init` writes already lists it. The first revision added
`.kiln/exclude`, passed to git as `core.excludesFile` on kiln's own commits. Its review found that the session's own
`git add -A`, which kiln's contract allows, bypasses the setting. Closing that gap would need a per-worktree setting
or a refusal in the gate.

Decided: neither. The one file it would catch is already ignored by every project `uv init` makes.

## Tool facts

The brief left these open. Each was checked with pytest 9.2.0, ruff and ty under uv 0.13.7 in a scratch project.

- pytest writes the XML file when a test fails (exit 1), on a collection error (exit 2) and when no tests run (exit 5,
  with zero `<testcase>` elements). It writes none on a usage error (exit 4).
- The XML lists every test that ran, passed ones included, each as a `<testcase>` with no child.
- Under `-x`, pytest stops at the first failure, exits 1, and the XML lists only the tests that ran before it.
- The XML does not hold pytest's test id (`tests/test_x.py::test_y[1]`). It holds `classname="tests.test_x"` and
  `name="test_y[1]"`.
- A teardown error says `failed on teardown with ...`. A test that fails and then errors in teardown appears as two
  `<testcase>` elements with the same name, one with a `<failure>` and one with an `<error>`.
- `uv run pytest ${KILN_JUNIT:+--junitxml="$KILN_JUNIT"}` writes the file when the variable is set and runs plain
  pytest when it is not.
- `.venv/`, `.pytest_cache/` and `.ruff_cache/` each hold a `.gitignore` of `*`, written by uv, pytest and ruff.
  `uv run ty check` leaves no cache in the project. In a project with no `.gitignore` of its own, `git add -A -n`
  adds only `tests/__pycache__/*.pyc` beyond the project's files. `uv init` writes a `.gitignore` that lists
  `__pycache__/` and `.venv`.
- Not checked: pytest's exit 3 (an internal error). If it writes no XML, the check names no test and is never
  excused.

## Acceptance tests

Each test runs through kiln's command line, the way spec 7's do. A gate command in tests 1 to 4 and 8 is a shell line
that copies a captured XML file into `$KILN_JUNIT` and exits 1, with one file for the build's commit and one for main.
Real pytest runs once, during the build, to capture those files. No test needs uv, the network or a model. Spec 7's
test 6 (`acceptance/project-roles/testdata/at6.txtar`) already covers `kiln init` with no flag writing Go's
defaults, and it must still pass unchanged.

### 1. A pytest test that fails on main too does not block a build

- **Given**: a project whose gate command reports test `test_a` failed, both on the build's commit and on main.
- **When**: a build of an approved spec reaches the gate.
- **Then**: the gate passes, and the build lands.

### 2. A pytest test that fails only on the build's commit fails the gate

- **Given**: a project whose `kiln.json` allows no round after a failing gate, and whose gate command reports
  `test_a` and `test_b` failed on the build's commit, and only `test_a` on main.
- **When**: a build reaches the gate.
- **Then**: the build stops, and kiln names `test_b` as failing and not `test_a`.

### 3. A test that fails in setup on main too does not block a build

- **Given**: a project whose gate command reports a setup error on `test_a`, both on the build's commit and on main.
- **When**: a build reaches the gate.
- **Then**: the gate passes.

### 4. A collection error fails the gate even when main has it

- **Given**: a project whose gate command reports a collection error, both on the build's commit and on main.
- **When**: a build reaches the gate.
- **Then**: the gate fails.

### 5. A failure that names no test points at `KILN_JUNIT`

- **Given**: a project whose gate command exits 1 and writes no XML, both on the build's commit and on main.
- **When**: a build reaches the gate.
- **Then**: the gate fails, and its report says the command named no failed test and names `KILN_JUNIT`.

### 6. A build that changes a Python file and the README gets an audit of the directories it changed

- **Given**: an approved spec whose build changes `app/models.py` and `README.md`.
- **When**: the build's gate passes.
- **Then**: the audit runs, and it is told to read `app` and `.` and no other directory.

### 7. `kiln init --preset python` writes Python's defaults

- **Given**: a repository with no `.kiln/`.
- **When**: the developer runs `kiln init --preset python`.
- **Then**: `.kiln/kiln.json` holds pytest, ruff and ty gate commands, the PyPI hosts and `uv cache dir`,
  and `.kiln/agents/build.md` says where Python acceptance tests live.

### 8. A run cut short is not excused

- **Given**: a project whose gate command reports `test_a` failed on main, where `test_b` also ran and passed, and
  reports `test_a` failed on the build's commit, where `test_b` did not run.
- **When**: a build reaches the gate.
- **Then**: the gate fails.

## Technical Plan

The design has four parts:

1. The gate learns test names from JUnit XML.
2. The audit reads every directory the build changed.
3. kiln's own wording drops Go.
4. `kiln init` writes one of two presets.

`kiln.json`'s format does not change.

### The gate reads JUnit XML

```text
runChecks, for each gate command
  │
  ├─ dir := new temporary directory (os.MkdirTemp, outside every worktree)
  ├─ env := the filtered environment + KILN_JUNIT=<dir>/junit.xml
  ├─ sh -c <command>                               ──► command may write <dir>/junit.xml
  ├─ failedTests(output)    → Go names, Go broken       (unchanged)
  ├─ readJUnit(<dir>/junit.xml) → JUnit failed, JUnit ran, JUnit broken
  ├─ check.Failed = Go names ∪ JUnit failed; check.Ran = JUnit ran; check.Broken = Go broken ∨ JUnit broken
  └─ remove dir
             │
             ▼
judge(tip checks, main checks)    (a check is excused only when it is not broken, names at least one
                                   test, every test it names failed at main, and every test main's
                                   check of the same command ran also ran here)
```

`runChecks` serves the gate at the build's commit, the gate at main, and the pre-commit hook that `kiln init`
installs, so all three get `KILN_JUNIT`. The variable is set only for the command, so the command string and the
key that matches a test at the build's commit to the same test at main stay as they are. The variable is appended to
the environment `proc.Shell.Env` filters, as every command gets today.

**A run cut short.** A build can make pytest stop early, for example by adding `addopts = "-x"` to `pyproject.toml`,
which the guard does not cover. The XML then lists only the tests that ran before the first failure. So the excuse
rule also asks that every test main's check ran also ran at the build's commit. A Go check lists no tests that ran,
so for it the condition holds as today. A build that renames or deletes a test main ran, while main has a failing
test, costs a round.

**Stored checks.** kiln stores main's checks under a key of main's tip and the commands. The key gains
`checksVersion`, a constant raised whenever what a check records changes, which this spec does once. A check stored by
an older kiln is then never read, and main's gate runs again once after the upgrade.

**Why a pytest failure is not marked broken by the Go reader.** `failedTests` marks a check broken only on a line
that starts `FAIL` and a tab with no `--- FAIL:` line before it, which is how `go test` reports a package that fails
to build. pytest prints no such line, so for a pytest command `Go broken` is false and only the JUnit reader decides.

**The JUnit reader's rules:**

- It reads every `<testcase>` at any depth of `<testsuites>` and `<testsuite>`, and ignores the summary counts in
  their attributes.
- Every `<testcase>` ran. A `<testcase>` with a `<failure>` failed. One with an `<error>` whose message starts
  `failed on setup with` or `failed on teardown with`, pytest's wording, failed (Decision 1). Any other `<error>`
  makes the check broken. A `<skipped>` test passed.
- A test's name is `<classname>::<name>`, or `<name>` alone when the classname is empty. Names are compared exactly
  and never trimmed, so a new parameter case of a test already failing on main is not excused. Each name counts once,
  even when pytest reports both a failure and a teardown error for it.
- A file that will not parse makes the check broken.
- A missing file names no test. With a non-zero exit, such a check is never excused, because `excused` refuses a
  check that names no test. The brief's rule "missing XML after a non-zero exit is broken" therefore needs no code of
  its own.

**The report.** When a command fails at the build's commit and at main, is broken in neither, and names no test in
either, the gate's report adds these lines after the command's output:

```text
The command named no failed test, so kiln could not compare it with main.
A test runner names its failed tests by writing JUnit XML to $KILN_JUNIT.
```

`judged`, which writes the report, gets main's checks by command to tell this case apart. A broken check, such as a
Go build failure or a collection error, gets no such line: it never names a test.

### The audit reads every directory the build changed

`goPackages` becomes `changedDirs` (Decision 6). The audit reads each directory that holds a file the build changed
or deleted outside `.kiln/`, kept when the directory still exists at the build's commit. A directory is written as
its path from the repository's top, or `.` for the top. A directory means its own files, not its subdirectories, so a
changed `uv.lock` adds the top directory's files to the audit and not the whole repository. kiln's code names no
source extension.

```text
changed since main: app/models.py, app/views.py, README.md, uv.lock, tests/acceptance/test_x.py
changedDirs:        ., app, tests/acceptance
audit prompt:       "Audit these directories: `.`, `app`, `tests/acceptance`. The spec is ... The build changed
                     these files: app/models.py, app/views.py, README.md, uv.lock, tests/acceptance/test_x.py."
```

The audit prompt lists every changed file, so a project's audit prompt can say which of them are worth reading. A
build that changes no file outside `.kiln/` gets no audit. Spec 5's message "no Go package changed, so no audit ran"
becomes "the build changed nothing kiln audits, so no audit ran", which stays true of builds landed before this
change. The tally line the audit ends with becomes `read N directories, found M` (Decision 5). kiln's tally pattern
reads only `found M`, so it parses both.

### kiln's own wording

The contract (`internal/kiln/contract/build.md`, `internal/kiln/contract/audit.md`), the `auditing` and `audited`
prompts in `internal/kiln/build.go`, and the `check` and `verdict` comments say "test" and "directory" where they say
"Go test" and "package". The `auditing` prompt stops naming `acceptance/<spec>/`. The project's own build and audit
prompts say where its acceptance tests live, in their "Tailor this" sections. The shared, untailored text of the
default audit prompt ("read its package doc", "exported name") is reworded for any language, since a Python project
gets it too.

### Presets and `kiln init`

A preset is the language-specific part of what `kiln init` writes: `kiln.json`, and the text of each "Tailor this"
section in `agents/build.md` and `agents/audit.md`. Everything else under `internal/kiln/defaults/` is shared. How a
preset fills the tailored sections is Decision 4.

|                                 | Go preset (today's defaults)                                   | Python preset                                                     |
|---------------------------------|----------------------------------------------------------------|-------------------------------------------------------------------|
| gate                            | `go vet ./...`, `go test ./...`                                | `uv run ruff check`, `uv run ty check`, and the pytest line below |
| hosts                           | `proxy.golang.org`, `sum.golang.org`, `storage.googleapis.com` | `pypi.org`, `files.pythonhosted.org`                              |
| writable                        | `go env GOCACHE GOMODCACHE`                                    | `uv cache dir`                                                    |
| acceptance tests                | `acceptance/<spec>/`, `TestAT<N>`                              | Decision 3: `tests/acceptance/test_<spec>.py`, `test_at<N>`       |
| formatting, in the build prompt | `gofmt`                                                        | `uv run ruff format`                                              |

The Python preset's pytest line is

```text
uv run pytest ${KILN_JUNIT:+--junitxml="$KILN_JUNIT"} || [ $? -eq 5 ]
```

pytest exits 5 when it collects no test, and `go test ./...` passes a module whose packages hold no test, so the line
lets a project with no tests yet pass, as a Go one does. Any other failure exits 1, which the gate judges as before.
The Python preset's audit prompt also reads any change to a file or table that configures pytest, ruff or ty as a
change to the gate.

`hosts` are the hosts a build's sandbox may reach, and `writable` holds commands that print the paths outside the
worktree it may write, not the paths themselves. The Python preset leaves out `uv python dir`, because it assumes a
Python uv finds already installed (see Goals). A project whose Python uv downloads adds `uv python dir` to `writable`
and the download's hosts to `hosts`.

**Choosing a preset.** `kiln init --preset python` writes the Python preset. `kiln init --preset go`, or `kiln init`
with no flag, writes Go's, exactly as today. Init refuses any other name with a message naming both. It asks nothing
(Decision 2). `presetFiles(preset)` turns a preset into files and has unit tests.

**Re-running init.** Today a re-run adds every key of Go's `kiln.json` that the project's file lacks. On a Python
project with no `writable` key, that adds `go env GOCACHE GOMODCACHE`, which fails on a machine without Go and stops
every build. A re-run now adds only keys that are the same for every preset, which today is `rounds` alone. A Go
project that deleted its `writable` key no longer gets it back from a re-run.

## Risks

- **A build changes its tools' settings.** pytest, ruff and ty read settings from files a build may change, such as
  `pyproject.toml`, `ruff.toml` and `conftest.py`, and the guard covers only `.kiln/`. A run cut short cannot be
  excused (see "A run cut short"), but a build can still turn off a lint rule or a type check. Go has the same hole
  through `TestMain` and `t.Skip`, and spec 1 relies on the audit for it. Mitigation: the Python preset's audit prompt
  reads any change to those settings as a change to the gate.
- **One test runner per command.** In `pytest; ruff check` the XML names only pytest's tests, so ruff's failure would
  be excused along with them. Today's Go gate has the same hole for `go vet ./...; go test ./...`. A build cannot
  change the gate. Mitigation: each preset puts each tool in its own gate command.
- **A build that deletes every test.** It passes the gate, as a Go build that deletes every test does today.
  Mitigation: the audit, and each spec's acceptance tests, which a build must write.
- **A failure every test shares.** A fixture that fails for the whole session, on the build's commit and on main,
  makes every test a setup error, and the gate excuses them all. Accepted: it cannot land a failure main does not also
  have.
- **The XML file's size.** Nothing caps it. Each gate command costs one temporary directory, one file read and one
  removal. Accepted: the file is read once per command.
- **The audit's cost.** Accepted: a build that changes only documentation, a lockfile or settings now runs an audit
  (Decision 6).

## Alternatives Considered

These come from the brief's alternatives.

### A reader picked per gate command in `kiln.json`

Each gate entry would name its reader, for example `{"run": "uv run pytest", "reads": "junit"}`. The starter spec
proposed this.

- **Pros**: kiln would know which commands are test runners, so it could refuse a command that names no test only
  where it expects names. The XML path could be passed as an argument instead of an environment variable.
- **Cons**: Each gate entry changes from a string to an object. `parseConfig` in `internal/kiln/gate.go` rejects
  unknown fields, so an older kiln binary refuses the new `kiln.json`, and a project shared between machines breaks
  until each one upgrades.
- **Reason for Rejection**: `KILN_JUNIT` gives the same subtraction with no change to `kiln.json`'s format. A
  command that does not write XML names no test, which kiln never excuses.

### Reading pytest's printed `FAILED path::test` lines

kiln would read pytest's terminal summary, as it reads `go test`'s `--- FAIL:` lines today.

- **Pros**: No variable, no file, and no change to the gate command. The names would be pytest's own test ids.
- **Cons**: The summary's form depends on pytest's flags (`-q`, `-rA`, `--tb`) and plugins, and kiln keeps only the
  last 16 KiB of output, where a long summary can be cut.
- **Reason for Rejection**: kiln could not rely on finding the summary, or on its form.

### No subtraction for Python, on the grounds that main should never fail

The gate would judge a Python command by its exit code alone.

- **Pros**: No reader, no variable, no new rule about crashes. This spec gets smaller.
- **Cons**: A flaky end-to-end test, or a test broken on main, blocks every worktree waiting to land until someone
  fixes main by hand. Go and Python projects would behave differently under the same gate.
- **Reason for Rejection**: the subtraction exists for flaky end-to-end tests while several worktrees land, and Go
  and Python should behave the same.

### Skipping the audit for Python builds for now

The audit would stay scoped to `.go` files, so a Python build would land after the gate alone.

- **Pros**: No change to the audit's scope, prompts or tally line in this spec.
- **Cons**: A Python build would land with no review beyond its own tests and linters.
- **Reason for Rejection**: the audit is required.

### Writing the XML inside the worktree

The gate command would write to a fixed path such as `.kiln-junit.xml` at the worktree's top.

- **Pros**: No temporary directory. The path could be written into the gate command itself, with no environment
  variable.
- **Cons**: kiln commits untracked files with `add --all` (`Commit` in `internal/git/git.go`), so the file would land
  in the build's commit unless every project ignores it. A file left by an earlier round could be read after a
  command that crashed before writing a new one, and excuse the crash.
- **Reason for Rejection**: kiln would commit the file, and a stale file could excuse a crash. A fresh directory per
  command makes a stale file impossible.

## Additional Resources

- `concepts_brief.md`: the brief this design was drafted from.
- The starter this design replaced: `git show ac21958:.kiln/specs/languages.md`.
- Spec 7, project-roles (`git show f6b984cfdf2e:.kiln/specs/project-roles.md`): `.kiln/`, `kiln.json`'s hosts and
  writable paths, and test 6, which this spec must keep passing.
- `docs/practices.md`: how kiln's Go code and tests are written.

## Detailed Implementation Plan

This plan follows Decisions 1 to 7. Paths are from the repository's top. kiln's own `.kiln/kiln.json` lets a build
reach `pypi.org` and `files.pythonhosted.org` and write `uv cache dir`, so the build can run uv and pytest to capture
item 4's files.

### Commits

Each commit passes `go vet ./...` and `go test ./...`.

1. The gate: items 1 to 5.
2. The audit and kiln's wording: items 6 to 11.
3. Presets and `kiln init`: items 12 to 21.
4. The acceptance tests and `CLAUDE.md`: items 22 to 24. Its message says it changes the test code of spec 7's
   test 3 and spec 3's test 5 (Decision 5).

### Target Files to Create/Modify

**The gate**

#### 1. `internal/kiln/junit.go` (Create)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**: The JUnit reader. `gate.go` keeps the reader of `go test`'s output.
    * `func readJUnit(path string) (failed, ran []string, broken bool)`.
    * A missing file returns nothing. Any other read error, or XML that does not parse, returns `broken`.
    * Unmarshal into a `junitSuite` struct that holds its own `[]junitSuite` and `[]junitCase`, so suites nest to any
      depth. The root may be `<testsuites>` or `<testsuite>`; both decode into the same struct.
    * `junitCase` holds the `classname` and `name` attributes and optional `failure`, `error` (with its `message`
      attribute) and `skipped` children. Summary attributes are not decoded.
    * Every case's name is in `ran`. A `failure` adds the name to `failed`. An `error` whose message starts
      `failed on setup with` or `failed on teardown with` adds it too. Any other `error` sets `broken`.
    * The name is `classname + "::" + name`, or `name` alone when `classname` is empty. `failed` and `ran` are sorted
      and each name is kept once, with `slices.Sort` and `slices.Compact`, as `failedAt` does.

#### 2. `internal/kiln/gate.go` (Modify)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**:
    * `runChecks` calls a new `runCheck(ctx, dir, command, env)` for each command. `runCheck` makes a directory with
      `os.MkdirTemp("", "kiln-junit-")`, removes it with `defer`, and returns the error when `MkdirTemp` fails. It
      appends `KILN_JUNIT=<dir>/junit.xml` to a copy of `proc.Shell.Env(env)`. A nil filtered environment held no
      PATH to lose, so it needs no other case. After the command, `c.Failed` is the sorted union of
      `failedTests(output)` and `readJUnit`'s `failed`, `c.Ran` is its `ran`, and `c.Broken` is the OR of both.
    * `check` gains `Ran []string` (`json:"ran,omitempty"`).
    * `excused` also asks that every name in the `Ran` of main's check of the same command is in `c.Ran`.
      `failedAt` returns main's `Ran` by command beside the names that failed.
    * The cache key of main's checks gains `checksVersion`, a constant set to 2, with a comment saying to raise it
      whenever what a check records changes.
    * `judged` gets main's checks by command. When `c` and main's check of its command both failed, neither is broken,
      and neither names a test, the report gains the lines the Technical Plan quotes, after the output.
    * Comments on `check.Failed`, `check.Broken`, `verdict.atMain`, `verdict.failing`, `judge`, `failedAt`, `excused`
      and `Precommit` say "test" where they say "Go test", and name JUnit XML beside `go test`'s output.

#### 3. `internal/kiln/junit_test.go` (Create)

* **Type / Scope**: Go unit test.
* **Proposed Changes**: One table test of `readJUnit` over item 4's files and a path that does not exist. Each row
  states the `failed`, `ran` and `broken` it expects. These are the domain rules of Decision 1 and the name format,
  which no acceptance test covers row by row.

#### 4. `internal/kiln/testdata/junit/` (Create)

* **Type / Scope**: Test fixtures.
* **Proposed Changes**: Eight files. The build captures six by running pytest 9.2.0 under `uv run` in a scratch uv
  project made in a temporary directory, not in the worktree: `failure.xml`, `setup-error.xml`,
  `teardown-error.xml` (a test that fails, then errors in teardown), `parameter.xml`, `collection-error.xml` and
  `skipped.xml`, each also holding one passing test. It writes two by hand: `nested.xml`, with `<testsuites>` inside
  `<testsuites>`, and `truncated.xml`, cut mid-element. Each captured file holds, after its `<?xml …?>` declaration,
  an XML comment naming pytest's version and the test it came from.

#### 5. `internal/kiln/rules_test.go` (Modify)

* **Type / Scope**: Go unit test.
* **Proposed Changes**:
    * `judgment` gains `hinted bool`, computed as `strings.Contains(v.report, "KILN_JUNIT")`. Existing rows want it
      false, among them "a build that fails at main too still fails", whose check is broken.
    * One new row: a command that fails at both commits, names no test and is not broken wants `hinted`.
    * One new row: a check that names `t` failed at both commits, where main's check also ran `u` and the build's
      did not, is not excused (acceptance test 8's rule).
    * `judge` treats a test name as an opaque string, so its existing rows already cover JUnit names.
    * `readOutputs`' comment says it reads only `go test`'s output.

**The audit**

#### 6. `internal/kiln/build.go` (Modify)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**:
    * `goPackages` becomes `changedDirs(ctx, head, paths) ([]string, error)`, still a method on `build`. For each
      changed or deleted path not under `kilnDir`, it takes the directory, kept when `git.Files(ctx, head, dir)` is
      non-empty, as `goPackages` does. A directory is its path, or `.` for the top, with no `./` prefix. It returns
      them sorted, each once. It does not filter by file extension (Decision 6).
    * `changedPackages` becomes `changedFiles`. It returns the build's commit, the directories and every changed path
      not under `kilnDir`.
    * `audit` says "the audit reads <dirs>", and fills `auditing` with the directories and the changed files.
    * `auditing` becomes "Audit these directories: %s. The spec is %s. The build changed these files: %s." The
      directories are backticked as today, and the files are joined with ", ". It no longer names `acceptance/%s/`.
    * `audited` says "the directories you changed" where it says "the packages you changed".

#### 7. `internal/kiln/agent_test.go` (Modify)

* **Type / Scope**: Go unit test.
* **Proposed Changes**: `TestAuditedPackages` becomes `TestAuditedDirs`. It expects `[. sub]` where it expects
  `[./sub .]` today, since `notes.md` now counts. It adds two cases: a `.py` file in a new directory, which is named,
  and a changed file under `.kiln/`, which names no directory. The fixture already holds a top-level non-Go file and a
  directory whose every file the work deleted. This is acceptance test 6's rule at the unit level.

#### 8. `internal/kiln/stats.go` (Modify)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**: Line 307's message, spec 5's phrase "no Go package changed, so no audit ran", becomes "the
  build changed nothing kiln audits, so no audit ran". The row at `internal/kiln/rules_test.go:443` changes with it.

#### 9. `internal/kiln/contract/audit.md` and `internal/kiln/contract/build.md` (Modify)

* **Type / Scope**: kiln's contract, embedded in kiln's binary.
* **Proposed Changes**: `audit.md` lines 5 and 8 say "directories" where they say "packages", and the last line
  becomes `read N directories, found M`. `audit.md` gains: "A directory means its own files, not its
  subdirectories." `build.md` line 49 says "a test that fails at main too" where it says "a Go test". The `tally`
  pattern in `build.go` reads only `found M` and does not change.

#### 10. `internal/kiln/kiln.go` and `internal/kiln/state.go` (Modify)

* **Type / Scope**: Go comments.
* **Proposed Changes**: The package doc (lines 51 and 60) and `FailingAtMain`'s comment say "directories" and
  "tests" where they say "packages" and "Go tests".

#### 11. `acceptance/project-roles/testdata/at3.txtar`, `acceptance/stats-tui/testdata/at5.txtar` (Modify)

* **Type / Scope**: Spec 7's and spec 3's acceptance test code, changed under Decision 5.
* **Proposed Changes**: `at3.txtar` line 18's grep reads `read N directories, found M`. `at5.txtar`'s `claude.json`
  gains one audit step for each of its 12 builds, each ending `read 1 directories, found 0` with no cost. Neither
  test's Given, When and Then comments change.

**Presets and `kiln init`**

#### 12. `internal/kiln/defaults/kiln.json` (Delete) and `internal/kiln/presets/go/kiln.json` (Create)

* **Type / Scope**: Embedded defaults.
* **Proposed Changes**: Today's file moves, byte for byte, so spec 7's test 6 still greps the same lines.

#### 13. `internal/kiln/presets/python/kiln.json` (Create)

* **Type / Scope**: Embedded defaults.
* **Proposed Changes**: The gate `uv run ruff check`, `uv run ty check` and the pytest line the Technical Plan quotes;
  `rounds` 3; hosts `pypi.org` and `files.pythonhosted.org`; writable `uv cache dir`.

#### 14. `internal/kiln/presets/{go,python}/{build-tests,build-checks,audit-tests}.md` (Create)

* **Type / Scope**: Embedded prompt text, one file per "Tailor this" section (Decision 4).
* **Proposed Changes**: Go's three files hold today's text of those sections, unchanged. Python's:
    * `build-tests.md`: acceptance test N is the function `test_at<N>` in `tests/acceptance/test_<spec>.py`
      (Decision 3), and drives the program through its command line or HTTP.
    * `build-checks.md`: run `uv run ruff format`, then the gate's three commands, before ending the turn.
    * `audit-tests.md`: where acceptance tests live, as in `build-tests.md`, and: read any change to a file or table
      that configures pytest, ruff or ty as a change to the gate.

#### 15. `internal/kiln/defaults/agents/build.md` and `internal/kiln/defaults/agents/audit.md` (Modify)

* **Type / Scope**: Embedded prompts.
* **Proposed Changes**: Each "Tailor this" section's body becomes one marker line: `<!-- kiln:preset build-tests -->`
  and `<!-- kiln:preset build-checks -->` in `build.md`, `<!-- kiln:preset audit-tests -->` in `audit.md`.
  `kiln init` replaces each with the preset's file. The comment that says "Tailor this" stays above it. `audit.md`'s
  shared text says "directory" where it says "package", and "a name other code uses" where it says "exported name",
  with a Go package and an exported name given once as the example.

#### 16. `internal/kiln/preset.go` (Create)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**:
    * `//go:embed presets` beside the existing `defaults` embed.
    * `type preset string`, unexported, with `presetGo` and `presetPython`, and `var presets = []preset{presetGo,
      presetPython}`. `docs/practices.md` asks every exported name to have a caller outside its package that is not a
      test, and `cmd/kiln` needs only the name the developer typed.
    * `func presetFiles(p preset) (map[string][]byte, error)`: keys are paths relative to `.kiln/`, with `kiln.json`
      at the top. It holds the preset's `kiln.json`, and every file under `defaults/` with each marker replaced by
      the preset's file. It fails when a marker names a file the preset lacks, or a preset file other than
      `kiln.json` fills no marker.

#### 17. `internal/kiln/init.go` (Modify)

* **Type / Scope**: Go, package `kiln`.
* **Proposed Changes**:
    * `Init(ctx, out io.Writer, opt InitOptions)`, where the exported `InitOptions` holds `Preset string`, the name
      the developer typed, or "" for none. An option struct is one of the forms `docs/practices.md` lists for naming
      a role at the call site.
    * Before it writes anything, Init refuses a name not in `presets` with `errUsage` and a message naming both.
      Given a name, it uses that preset; otherwise Go's.
    * `writeDefaults` walks `presetFiles`' keys in sorted order instead of the `defaults` embed.
    * `addKeys` adds only the keys in `var sharedKeys = []string{"rounds"}`, the keys whose default is the same in
      every preset.
    * The comments on `Init`, the `defaults` embed, `writeDefault` and `addKeys` say what a re-run adds now.

#### 18. `internal/kiln/preset_test.go` (Create)

* **Type / Scope**: Go unit test.
* **Proposed Changes**: Every preset in `presets` fills every marker and leaves none behind. This is the check
  Decision 4 calls for, which no acceptance test covers for the Go preset's markers.

#### 19. `Init`'s callers and `TestInitKeeps`: `internal/kiln/agent_test.go` and `internal/kiln/kiln.go` (Modify)

* **Type / Scope**: Go. Items 7 and 10 also change these files.
* **Proposed Changes**: The two `r.Init` calls in `agent_test.go` and the example in `kiln.go`'s package doc pass
  `InitOptions{}`. `TestInitKeeps` asserts that a re-run keeps the gate and `rounds` and adds no `hosts` or
  `writable` key, and its doc comment says so. The package doc's `.kiln/` paragraph (`kiln.go:34`) says init starts
  `.kiln/` from a preset.

#### 20. `cmd/kiln/main.go` (Modify)

* **Type / Scope**: Go, command line.
* **Proposed Changes**: `init` gets its own command function in place of `noArgs`. It parses `--preset` with a
  `flag.NewFlagSet` and `flag.ContinueOnError`, as `hook` does, refuses any other argument with `errUsage`, and passes
  the name. The usage lines 3 and 40 add `[--preset go|python]`.

#### 21. `README.md` (Modify)

* **Type / Scope**: The project's README.
* **Proposed Changes**: The usage line 14 reads `kiln init [--preset go|python]`. The `.kiln/` section (line 53) says
  init writes a preset's defaults, and that a test runner in the gate names its failed tests by writing JUnit XML to
  `$KILN_JUNIT`. Line 91 names `internal/kiln/presets/` beside `internal/kiln/defaults/`.

**Acceptance tests**

#### 22. `acceptance/languages/languages_test.go` (Create)

* **Type / Scope**: Acceptance test code, as `acceptance/project-roles/roles_test.go` is.
* **Proposed Changes**: `TestMain` calls `scenariotest.Main`; `TestAT<N>` runs `testdata/at<N>.txtar`, one per
  acceptance test.

#### 23. `acceptance/languages/testdata/at1.txtar` to `at8.txtar` (Create)

* **Type / Scope**: Acceptance test scripts.
* **Proposed Changes**: Each quotes its test's Given, When and Then as comments.
    * In tests 1 to 4 and 8, `.kiln/kiln.json`'s gate is one line that copies `_junit/main.xml` into `$KILN_JUNIT`
      when `HEAD` is main's tip, and `_junit/build.xml` otherwise, then exits 1. The XML files, copied from item 4's
      captures, sit in the archive.
    * Test 2 sets `rounds` to 0, so the build stops at its first failing gate, and greps `kiln run`'s `failing:` line,
      as `acceptance/notify/testdata/at1.txtar` does.
    * Test 5's gate is `exit 1`. It sets `rounds` to 1 and greps the build's next prompt, which carries the gate's
      report, for `KILN_JUNIT`.
    * Tests 1, 3 and 6 reach the audit, so their `claude.json` scripts one audit step ending
      `read 1 directories, found 0`.
    * Test 6 greps the audit's prompt, `.claude-calls/audit/1/1/prompt`, for its `Audit these directories:` line,
      which names `.` and `app` and no other directory.
    * Test 7 runs `kiln init --preset python` and greps `.kiln/kiln.json` and `.kiln/agents/build.md` for what its
      Then names.

#### 24. `CLAUDE.md` (Modify)

* **Type / Scope**: kiln's instructions for sessions in this repository.
* **Proposed Changes**: "Where things are" adds `internal/kiln/presets/`: each preset's `kiln.json` and the text of
  its tailored prompt sections, which `kiln init` writes with `internal/kiln/defaults/`. The added line names nothing
  specific to kiln's own repository.

**Not changed**

* `.kiln/` (kiln's own settings and agents), beyond the hosts and writable command committed with this spec: kiln is
  a Go project, and a build changes nothing there.
* `acceptance/project-roles/testdata/at6.txtar`: it must pass unchanged.
* `docs/later.md`: line 7 lists `languages` as a starter, which stays true until the developer approves this spec.
  Line 43's "packages" records what was deferred, in the words of its time.

### Verification Plan

* `go vet ./...` and `go test ./...` pass at each commit. `acceptance/stats-tui/` and `acceptance/tui-reading/` need a
  pseudo-terminal, which no sandboxed session can open, so a build session reads their result from kiln's gate
  (`CLAUDE.md`).
* `go test ./acceptance/languages/ ./acceptance/project-roles/ ./acceptance/stats-tui/` passes, with spec 7's test 6
  unchanged.
* CodeScene Code Health is 10.0 on every touched Go file, checked with `pre_commit_code_health_safeguard`.
* By hand, once: in a `uv init` project with pytest, ruff and ty added as dev dependencies and one passing test, run
  `kiln init --preset python`, commit `.kiln/`, approve a one-test spec, and `kiln run` lands it with an audit that
  names the directory its test lives in.
