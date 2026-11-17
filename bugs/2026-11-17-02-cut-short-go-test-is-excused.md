# The gate excuses a go test run cut short, so a regression lands

Found 2026-11-17, by an audit of building and landing.

## What happened

A package holds `TestFlaky`, which fails at main, and `TestZeta`, which passes there. A build changes the code
`TestZeta` calls so that it ends the process, as `os.Exit(1)` or `log.Fatal` does. At the build's commit `go test`
prints `--- FAIL: TestFlaky`, then `exit status 1` and `FAIL example.com/scratch`. The one test it names fails at
main too, so the gate excuses the run. The build lands with `TestZeta` broken.

A panic in a goroutine the test did not start, and `go test`'s timeout, name no test with `--- FAIL` either.

## What should happen

As `docs/reference.md` says, a run that left out a test main ran is never excused. For `go test`'s output, a package
that ended with `exit status N`, a panic or a timeout, and so did not finish its tests, is not excused.

## How to see it

A scenario whose main holds a failing `TestFlaky` and a passing `TestZeta` in one package, and whose build makes
`Zeta` call `os.Exit(1)`. Today `kiln fire` prints "x: landed on main", and `go test -run TestZeta ./...` fails.

## Where to look

`failedTests` and `failures.read` in `internal/kiln/gate.go`, and `excused` there. Only JUnit XML fills
`check.Ran`, so the rule for a run cut short never reads `go test`'s output.
