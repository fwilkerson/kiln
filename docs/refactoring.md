# Refactoring to Code Health 10.0

How to bring a Go file in kiln to CodeScene Code Health 10.0 without breaking `docs/practices.md`. Each recipe here
was tried on kiln's code and scored. `internal/git` at `76b405c` shows most of them.

## The loop

1. Run `code_health_review` on the file. Note each finding and its function.
2. Change one function. Keep the change small enough to explain in one sentence.
3. Run `go run ./tools/style -l <file>`, `go vet` and the package's tests.
4. Run `code_health_score`. The score must rise or hold. If it falls, undo the step.
5. When every file in the package scores 10.0, run `go test ./...` and `pre_commit_code_health_safeguard`. Commit
   the package.

Score the whole package after each commit. A split can lower a file you did not touch.

## What CodeScene counts in Go

| Smell                     | It flags                                                | Threshold     |
|---------------------------|---------------------------------------------------------|---------------|
| Complex Method            | cyclomatic complexity of one function                   | 9 or more     |
| Complex Conditional       | one condition with two or more `&&` or `\|\|` operators | 2 operators   |
| Bumpy Road                | two or more blocks nested two conditionals deep         | 2 bumps       |
| Deep Nested Complexity    | nesting depth of conditionals                           | 4 levels      |
| Large Method              | lines of code in one function                           | more than 80  |
| Excess Function Arguments | parameters of one function, `ctx` included              | more than 4   |
| Overall Code Complexity   | mean cyclomatic complexity of the file's functions      | more than 5   |
| Primitive Obsession       | share of all arguments that are built-in types          | see below     |
| String Heavy Arguments    | share of all arguments that are `string` or `[]string`  | more than 39% |
| Low Cohesion              | groups of functions that share no data and no calls     | 3 or more     |

- `ctx context.Context`, an `error`, a struct and a named type such as `git.Rev` count as non-primitive arguments.
- `int` and `bool` count as primitive. `...string` and `[]string` count as strings.
- The two argument-share smells apply only once a file has about a dozen arguments. Scratch files with 11 string
  arguments were not flagged, and files with 24 were.
- Primitive Obsession's limit is not pinned down. The safeguard names 40%, and it flagged a kiln file at 42.5%. Yet
  scratch files at 50% passed, and files at 67% were flagged. Aim below 40%.
- A method's receiver is not an argument.
- A `switch` whose `case` lists several conditions, as `case a, b, c:`, is not a complex conditional.
- A boolean chain in a `return` or in a `case` expression is not a complex conditional either.
- A loop that holds an `if` or a `switch` counts as a bump. Two such loops in one function make a Bumpy Road.
- The review text says Primitive Obsession counts "functions", but it counts arguments. A function with none adds
  nothing.
- One helper that nothing else in its file calls can raise Low Cohesion by itself.
- A file near the dozen-argument floor can tip into a string smell when a split adds two helpers.

## Recipes

### A condition with two operators

Use a `switch` with a case list for a chain of `||` guards. Give a chain of `&&` a named predicate, such as
`indexLocked(err)` in `internal/git/land.go`.

```go
switch {
case err != nil, !filepath.IsAbs(found), found == shim, !proc.Executable(found):
	return bin
}
```

### A complex method

Name each step, and give the steps the state they share as a small struct with methods. `move` in
`internal/git/land.go` holds a branch's move. `moveOnce` reads as `blocked`, `apply` and `refused`.

### A bumpy road

Separate reading from deciding. `busy` once read and matched rebase state in one nested loop. `gitDirs` and
`working` now read, and `busy` only matches.

### Strings for every role

Give each role its own named type in the package that owns the fact: `git.Rev`, `git.Branch`, `git.Worktree`. Keep a
plain `string` for free text, such as a commit message. Convert at a boundary to a package that does not know the
type, as `string(b.tree)` for `claude.Launch.Dir`. Don't add a type that one function alone uses to dodge the count.

### Too many arguments

Group the arguments that describe one operation in a struct, such as `rebase` in `internal/git/git.go`. A struct of
unrelated values only hides the smell.

### A test that checks a table row

Put each row's setup in a helper that returns a struct, such as `makeHistory`. Have each row state its `want` as a
comparable struct, and compare it with `got != want`. The loop body then holds no condition with two operators.

### More recipes from the first pass

- **A chain of comparisons:** put them in a struct literal, such as `landing{landed: x != "", atMain: tip == main}`,
  and compare it with `!=`.
- **The first answer that isn't empty:** `cmp.Or(a(), b, c())` replaces a chain of early returns. It runs every
  argument, so use it only for cheap checks.
- **A switch that maps a value to words:** index a map literal, as `reason.shown` in `internal/kiln` does.
- **Flags:** map each flag's name to a pointer to its field, and set them all in one function.
- **Allowed words:** `turn, known := wheelTurns[arg]` checks the word and yields what it means at once.
- **A step that must say "try again":** return a sentinel error, such as `errMainMoved`, and loop on `errors.Is`.
- **A command's many parts:** hand over a built `*exec.Cmd` instead of its program, arguments, directory and
  environment.
- **A value the framework already types:** pass it as that type, such as a `tea.WindowSizeMsg` for a size.
- **Strings a split left behind:** make the new helpers methods on a named type, since a receiver is no argument.
- **A stray helper:** move it into the file of its only caller. That cleared Low Cohesion in `build.go` with no split.
- **Prove no change:** for code no test pins, run the old and the new on the same inputs, and diff the output. A
  style checker gets a corpus that breaks each rule. A view gets snapshots of `View()` at each screen size.
- **A guard or a refusal:** scores and tests can pass while it loosens. Run the old code beside the new on many
  inputs with `go test -overlay`, as the audit did for the hook guard at `4a8380e`.
- **Go 1.27's `go fix`:** a literal must name an embedded struct's promoted fields, or the root's `TestModern` fails.
  Run `go test ./...` from the root, not only the package's tests.

## Where Code Health and the practices disagree

The practices decide, unless the developer accepts the break. Record each break, and each change in behavior, in the
commit message with its reason. Breaks to watch for:

- A file split with no seam in git history, made only to raise cohesion.
- A shallow pass-through added only to lower a function's complexity.
- A new package made only to hold extracted helpers.
- A comment over 20 words, or a line over 120 columns. `tools/style` refuses both.
