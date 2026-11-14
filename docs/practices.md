# Practices

How Go code is written and judged in kiln. The rules kiln shares with the developer's other projects are in the guide,
`guide.md` of the module `github.com/fwilkerson/practices`. Read the guide first. This command prints it from any
checkout or worktree of kiln:

```sh
cat "$(go list -m -f '{{.Dir}}' github.com/fwilkerson/practices)/guide.md"
```

This file holds only what is kiln's own. Each section adds to the guide's section of the same name. Where the two
differ, this file decides for kiln.

## The call site is the API

- kiln's command line is a call site too. kiln writes its own flags with two hyphens, as in `kiln hook --spec <name>`.

## Errors across a boundary

- Reply an unknown error as an opaque `internal_error`. The CLI alone keeps its cause, for the agent that relays it.

## Tests

- A test's condition holds one `||`. `claim.Holds`, from the practices module, takes the rest, as
  `TestSpecBeforeKiln` in `internal/kiln/agent_test.go` shows. `go doc -all github.com/fwilkerson/practices/claim`
  is its contract.

## Acceptance tests

How kiln keeps its own acceptance tests, in `uat/`. The plan is Adzic's, in "Specification by Example", chapter 11:
organize current work by stories, then reorganize it by functional area. A story ends, and the feature stays.

- A spec's tests live in `uat/<spec>/` until the developer accepts it. Each quotes its test's Given, When and Then.
- Once the spec is accepted, its scenarios fold into `uat/testdata/<feature>/`. A feature is something the developer
  does, such as review a build. Name a folded scenario for what it shows. From then on it follows the product, not
  the retired spec's wording or numbers.
- Fold in a change that touches the tests alone. A build never folds or merges, since its session would rewrite the
  tests that judge its code. A build changes a folded scenario only where its spec changes what the scenario shows.
- Merge two scenarios that build the same Given, so one build carries the checks of both. Three of kiln's specs
  each described one state from their own side: nine of their scenarios held one of three fixtures, byte for byte.
- Drop a scenario when a unit table's row holds its rule, and another scenario runs the same wiring through the
  command line. Keep one scenario for each transport, such as Go's test output and JUnit XML.
- A fold keeps each check, or says why it went. `.kiln/monitors/uat-lost <commit>` lists the checks no scenario
  has held since that commit.
- Never merge a scenario that needs a pseudo-terminal with one that needs none. A sandboxed session runs only the
  second kind.
- A scenario brings only the files its Given names. What each scenario would repeat goes in `internal/scenariotest`,
  which seeds it. Three landings rewrote 20, 37 and 58 of the other specs' scenarios, each for a line every scenario
  held a copy of.
- When one change rewrites many scenarios, the tests are saying what they share. Move it into the harness first.
- Keep a scenario to one story a developer would tell. It may be long, when each step follows from the last.
- The `uat` monitor in `.kiln/kiln.json` folds. Its check asks for a fix when an accepted spec still has a folder
  under `uat/`, or two scenarios bring the same fixture. Its fix waits for `kiln accept uat`.

## Working with agents on an API

- An audit's findings go back to the build that made the change, not to the developer. The build prefers a design
  that makes a finding impossible over a patch.
- The guide says not to audit the fix for an audit finding. The exception is a path the developer lists in
  `kiln.json`'s `reaudit`, which never lands unread.

## Mechanical checks

- `gofmt`, and `go tool style`: blank lines, and the 20-word and 120-column limits. A hook runs it after each edit.
- `arch_test.go` and `style_test.go`: import rules, modern Go, and doc names.
- `go test ./...`, which the pre-commit hook runs on each commit to main.
