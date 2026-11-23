# Practices

How Go code is written and judged in kiln. The rules kiln shares with the developer's other projects are in the guide,
`guide.md` of the module `github.com/fwilkerson/practices`. `CLAUDE.md` loads kiln's copy of it, `docs/guide.md`,
into each session.

This file holds only what is kiln's own. Each section adds to the guide's section of the same name. Where the two
differ, this file decides for kiln.

## The call site is the API

- kiln's command line is a call site too. kiln writes its own flags with two hyphens, as in `kiln hook --spec <name>`.

## Errors across a boundary

- Reply an unknown error as an opaque `internal_error`. The CLI alone keeps its cause, for the agent that relays it.

## Docs are the contract

- A rule of how kiln behaves has one home: the doc of the package that holds it. `docs/reference.md` says what a
  project writes in `.kiln/`, and points to that package doc for what kiln then does. `README.md` shows the commands
  in use, and states no rule.
- When two docs state one rule, keep the package doc's, and make the other point to it.
- The `docs` monitor, in `.kiln/monitors/docs/`, reads the docs of each package whose code changed against that code.
  Its fix lands without `kiln accept`.

## Tests

- Prove a rule with the cheapest test that reaches it through real machinery, among those the session may write.
  From cheapest: a row in a table that exists, a check added to a test that exists, a new unit test over real git,
  files or a fake claude, a check added to a scenario whose Given already reaches the rule, and last a new scenario,
  at seconds each. A new scenario is for what only the built binary shows: its exit code and output, a killed or
  racing kiln, a git hook. A spec's acceptance tests differ: one for each Then, through the command line.
- A test's condition holds one `||`. `claim.Holds`, from the practices module, takes the rest, as
  `TestSpecBeforeKiln` in `internal/kiln/agent_test.go` shows. `go doc -all github.com/fwilkerson/practices/claim`
  is its contract.
- A kiln killed at any step recovers at the developer's next command. `crash.Step` marks each step that changes
  durable state. A new kind of durable write calls it where it writes, never at a caller. `go doc ./internal/crash`
  lists where kiln calls it, and says how a build with `-tags crash` kills kiln, or snapshots what a kill would leave.
  `TestKilledAtEachStep` kills kiln at each step of its flows, then drives each flow to its end and checks kiln's
  records against git. Its doc says why a snapshot stands in for SIGKILL. Fix a kill it fails by a design that makes
  the state impossible. A defect too large for one commit gets a row in its `defects`, which fails once the defect is
  fixed.
- The `gate-time` monitor, in `.kiln/monitors/gate-time/`, makes the tests faster once the gate slows, by the rule its
  check's header states. Its fix keeps each check, and waits for `kiln accept gate-time`.

## Acceptance tests

How kiln keeps its own acceptance tests, in `uat/`. The plan is Adzic's, in "Specification by Example", chapter 11:
organize current work by stories, then reorganize it by functional area. A story ends, and the feature stays.

- A spec's tests live in `uat/<spec>/` until the developer accepts it. Each quotes its test's Given, When and Then.
- Once the spec is accepted, its scenarios fold into `uat/testdata/<feature>/`. A feature is something the developer
  does, such as review a build. Name a folded scenario for what it shows. From then on it follows the product, not
  the retired spec's wording or numbers.
- Fold in a change that touches the tests alone. Only the `uat` monitor folds, merges or weighs: another session
  that did would rewrite the tests that judge its own code.
- Merge two scenarios that build the same Given, so one build carries the checks of both. Three of kiln's specs
  each described one state from their own side: nine of their scenarios held one of three fixtures, byte for byte.
- Weigh each folded scenario once. For each check, find the cheapest test that can hold its rule, as "Tests" ranks
  them. Where that is not a scenario, the check moves there, once it fails with the rule broken. Each step only the
  check needed goes with it.
- Drop a scenario once its checks have moved, and another scenario runs the same wiring through the command line.
  Keep one scenario for each transport, such as Go's test output and JUnit XML.
- A weighed scenario that stays says why in one line of its script: `# Only the built binary shows: <what>`. A
  merge into it takes the line out, so the checks it gains are weighed too.
- A fold or a weighing keeps each check, or says why it went. `sh .kiln/monitors/uat/lib/lost <commit>` lists the
  checks no scenario has held since that commit.
- Never merge a scenario that needs a pseudo-terminal with one that needs none. A sandboxed session runs only the
  second kind.
- A scenario brings only the files its Given names. What each scenario would repeat goes in `internal/scenariotest`,
  which seeds it. Three landings rewrote 20, 37 and 58 of the other specs' scenarios, each for a line every scenario
  held a copy of.
- When one change rewrites many scenarios, the tests are saying what they share. Move it into the harness first.
- Keep a scenario to one story a developer would tell. It may be long, when each step follows from the last.
- The `uat` monitor, in `.kiln/monitors/uat/`, folds and weighs. Its check asks for a fold when an accepted spec
  still has a folder under `uat/`, or two scenarios bring the same fixture. Otherwise it lists a few scenarios not yet
  weighed, the costliest first, by the rule its header states. Its fix lands without `kiln accept`: the audit reads
  it, and its last reply accounts for each dropped check.

## Working with agents on an API

- An audit's findings go back to the build that made the change, not to the developer. The build prefers a design
  that makes a finding impossible over a patch.
- The guide says not to audit the fix for an audit finding. The exception is a path the developer lists in
  `kiln.json`'s `reaudit`, which never lands unread.

## Mechanical checks

- `gofmt`, and `go tool style`: blank lines, and the 20-word and 120-column limits. A hook runs it after each edit.
- `go tool health .`: the score of each Go file by the guide's "Inside a function". `TestHealth` holds the score of each
  file under 10 in `held`, so no change lowers one. The `code-health` monitor, in `.kiln/monitors/code-health/`,
  refactors the files with the lowest scores, as its gauge lists them, and deletes each from `held` once it scores 10.
- `go tool inspect`: GoLand's inspections, by the profile in `.idea/inspectionProfiles/` and its scopes in
  `.idea/scopes/`, which git tracks with `.idea/dictionaries/` alone. It needs a licensed GoLand, and in a session,
  `kiln.json`'s `allowUnixSockets`. The `inspect`
  monitor, in `.kiln/monitors/inspect/`, clears the findings in the files its gauge lists. Where an inspection
  misreads kiln, the developer tunes the profile, or keeps one finding in the monitor's `lib/kept`, with the reason
  above it. No comment suppresses an inspection.
- `arch_test.go` and `style_test.go`: import rules, modern Go, doc names, and a switch or table that names each value
  of an enum `enums` lists.
- `TestFacts` in a package's `facts_test.go`: each number the package doc states, beside the constant that holds it.
  `repotest.Facts` fails when one moves without the other. A number a doc states gets a row there.
- The gate in `.kiln/kiln.json`, which the pre-commit hook runs on each commit to main: `go vet ./...`, and
  `go test ./...` with a timeout that only a hang reaches.
