# Practices

How Go code is written and judged in kiln. Lessons distilled from odh, a Go modular monolith built with AI agents. Each
rule here was either backed by research or learned from a mistake. Where a source or a run settled it, the source is
named.

## Principles

- Keep the code small and simple. Measure small in lines. Simple is not the same as easy.
- Make an impossible state impossible to build, rather than checking for it at runtime.
- One small reusable piece beats the same glue repeated in each caller.
- Prefer a deep module: a small interface over a lot of behavior (Ousterhout).
- Don't make the client do anything the module could do (Bloch). When in doubt, leave it out.
- Minimize conceptual weight, not the count of types or methods (Bloch).
- Build what the current step needs. Expect names to change and refactors to follow.
- Use modern Go where it removes code: generics, `http.ServeMux` patterns, `log/slog`, `errors.AsType`,
  `testing/synctest`, `sync.WaitGroup.Go`. A test that fails while `go fix` would change the code keeps it modern.

## Packages

A package is a contract its users read through `go doc`, or a piece two packages share.

- Default to fewer packages. Each split adds an import, a package doc, and an exported name for each shared thing.
  Cheney: "Your default position should be to not create a new package."
- Split a package when a second importer needs the code. Also split a subsystem whose contract a reader can use without
  reading its body. The standard library moved `singleflight` out only once the go command needed it too.
- Don't split a package only to guard one type's fields. The standard library and large projects keep such types
  unexported where they are used: `sync.poolDequeue`, `database/sql.closingMutex`, CockroachDB's `mu struct{...}`.
- Guard locked fields with layout and one comment above them. The comment states the rule they obey: "read and written
  only while holding sem."
- Use `internal/` to decide who may import a split. It does not supply a reason to split.

## Files

- Give a piece its own file when the rest of the package reaches it only through funcs, never fields.
- Confirm the seam in git history: most commits should change the piece or the rest, not both.
- Name each file for its concept. Give one role the same file name everywhere, such as `outbox.go` in every service. A
  file name then works as an index from `ls`.
- For an agent, `go doc -all` reads the package and ignores its files. A split changes only what a source read costs.

## The call site is the API

Judge an API by how its call looks across every use, read without its docs. Bloch, Google's Go style and Abseil Tip #94
agree.

```go
// Positional: compiles safely, since the funcs have distinct types, but names no role.
s.pay = aggregate.NewOnce((*model.Order).Paying, s.authorize, s.paid)

// Roles named: the reader needs no hop to learn what each argument does.
s.pay = aggregate.On(orders).Wait((*model.Order).Paying).Send(s.authorize).Keep(s.paid)
```

- Count go-to-definition hops. A reader who needs four hops to learn what an argument does has hit an unknown unknown.
- A doc must land. A reader who jumps to a symbol learns what it does and when, without a second hop. `go doc` groups
  constructors under their type, but go-to-definition never shows that grouping.
- To name roles, pick one: an option struct, a builder chain, or labeled wrapper funcs. Positional funcs fit only when
  the call is obvious.
- In Go 1.27, a generic struct literal must list every type argument. An option struct over generic funcs then costs
  type arguments or an alias. A builder or a constructor infers them.
- A struct of funcs compiles with a nil field. A builder or positional form makes a missing step a compile error.
- Reuse a method name across types only with the same signature and meaning (Effective Go). One word means one thing.
- Avoid a name that already means something in Go. A `Once.Do` that does not mean `sync.Once.Do` is a pun.
- Avoid conjoined methods, which a reader must read together to understand either.
- Avoid shallow pass-throughs, such as a func that only calls another with a default.
- A shared signature can hide a choice. When the caller's choice of `Do` decides what gets sent, fold the choice into
  the module.
- kiln's command line is a call site too. kiln writes its own flags with two hyphens, as in `kiln hook --spec <name>`.

## Make misuse impossible

The best rule is one the compiler checks. Prefer, in order: a compile error, a panic at startup, a failing test, and
last, a sentence in a doc.

- Seal an interface with an unexported method, so no other package's type can satisfy it.
- When only the framework may supply a value, give it a type the caller cannot build usefully. odh's `aggregate.Inbox`
  holds a reply only while the root still waits on it.
- A wrong value that the compiler accepts should panic at startup, such as a codec that refuses an untagged field.
- Make every exported name have a caller outside its package that is not a test. If only tests use it, unexport it.
  Packages whose names end in `test` are exempt.
- Write arch tests for the rules the compiler skips. Such rules include forbidden imports, state changed outside its one
  method, a handler that writes twice.
- List the rest as Traps: rules that pass the compiler, the tests and the linters. Each trap names the doc that holds
  its fact.

## Errors across a boundary

- Give each error that crosses a package boundary a kind, and let callers compare it with `errors.Is`.
- The package that owns the fact declares the kind, even when another package returns it.
- Pass on another package's refusal unchanged when it is about the request. Name each kind passed on in the doc.
- Reply an unknown error as an opaque `internal_error`. The CLI alone keeps its cause, for the agent that relays it.

## Docs are the contract

- A package doc shows before it explains. Open with a short code sample or a `text` diagram, then add only the rules the
  sample cannot show. Go's `context` package is the model.
- Give each package that others use a runnable Example, which `go test` checks against its Output. One whole-module
  Example teaches the pattern faster than prose.
- Put each fact a user could learn only from the source in the package doc. Such a fact is a limit, a timeout, a retry
  interval or a name-length cap. In odh's quiz, a fresh agent scored 16 of 24 on such facts before they were documented.
  After, it scored 24 of 24.
- Keep rules about how packages compose in a practices doc, not in any one package doc. Without it, agents missed
  only those.
- Prose that repeats an example gets cut.

### Where a sentence goes

Decide by what to fix when the code disagrees with it.

| The sentence says                          | It goes in                | When code disagrees  |
|--------------------------------------------|---------------------------|----------------------|
| what the code is                           | README or the package doc | fix the doc          |
| a rule a reviewer would reject a commit on | the practices doc         | fix the code         |
| why one package has its rule               | that package's doc        | fix the doc          |
| a reason no package owns, or a plan        | a decisions log           | revisit the decision |
| something true only because an AI works    | `CLAUDE.md`               | fix the instruction  |

Change the README in the same commit as the code it describes. A stale doc is a bug.

### Comments

- A doc comment starts with the name it documents and says what the thing is for.
- Elsewhere, a comment follows `CLAUDE.md`, Comments. Fewer comments go stale less.
- Write plain, complete sentences, each with one idea and at most 20 words. Enforce the limit with a tool.
- An instruction file states what good looks like, and names the file that shows the pattern.

## Tests

- Test the domain rules and the ways the code is wired. Those two are enough.
- Write the rules as one table that reads as the spec: name, given state, command, then the result or error.
- Run each row through the real machinery, not a mock of it. The test then exercises the public API as callers use it.
- Run each wiring once for each transport, such as in process, over HTTP and sharded, all in parallel.
- Give each late or repeated reply its own row.
- Fix a bug by first writing its test, and show it failing on the old code. A failing test ends a debate that prose
  cannot.
- Do not repeat a rule's test at a higher layer.

## Judging two API designs

Score both designs on five weighted measures:

| # | Weight | Measure                                                             |
|---|-------:|---------------------------------------------------------------------|
| 1 |      3 | Fewer go-to-definition hops, summed over every line a reader traces |
| 2 |      2 | Fewer shallow pass-through functions                                |
| 3 |      2 | Less code that nearly repeats code in another package               |
| 4 |      3 | Fewer files and lines resting on a rule the compiler skips          |
| 5 |      1 | Fewer lines of code                                                 |

- In measure 4, a rule counts twice if breaking it stores wrong data, or makes or skips an outside call. The judge cites
  each file and line it counts.
- Give each measure a margin of 0, 1 or 2. It is 0 when equal. It is 2 when one count is at least twice the other and
  they differ by at least 3. Otherwise it is 1. For size, under 1% is 0, and 5% or more is 2.
- The winner of a measure scores its weight times its margin. The higher total wins. A tie goes to the first measure
  that differs.
- Size weighs least. In odh, size alone once picked a design that spread one lock rule over three groups of fields.
- Report a difference in behavior beside the score, not in it. The requirements settle it.
- Keep the table with the verdict. When the kept design proves wrong, the table shows which weight misled.

## Working with agents on an API

- For a change that shapes a framework, have two agents design apart from the same brief. Leave your favored design
  out of the brief. Each builds in its own worktree until every test passes.
- Where the two converge, keep that. Where they split, a fresh judge scores both without your lean.
- Record the rejected design, and the fact that decided, under Alternatives Considered or in the commit message.
- After a change, run a fresh audit agent that reads whole packages and reports where code breaks a promise. Its
  findings go back to the build that made the change, which prefers a design that makes a finding impossible over a
  patch.
- Do not audit the fix for an audit finding. Chasing one finding after another hides a better design.
- Tell an agent to run `go doc -all` on a package before using it. Its doc is the contract, and its Example shows the
  use.

## Mechanical checks

Let tools hold every rule that needs no judgment, so a review spends its attention on judgment. In kiln:

- `gofmt`, and `tools/style`: blank lines, layout, the 20-word and 120-column limits. A hook runs it after each edit.
- `arch_test.go` and `style_test.go`: import rules, modern Go, and doc names.
- `go test ./...`, which the pre-commit hook runs on each commit to main.

## Sources

Ousterhout, *A Philosophy of Software Design* and CS 190. Bloch, "How to Design a Good API". Google's Go Style Guide,
Best Practices. Effective Go. Abseil Tip #94. Cheney, *Practical Go*. Cox, "Go 1.4 Internal Packages". Ottinger's
naming rules.
