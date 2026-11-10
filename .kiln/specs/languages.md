# Spec N: Projects in other languages

Status: Proposed

<!-- A starter, taken from docs/later.md on 2026-11-10, while spec 7 (project-roles) was drafted. Run /shape languages
to take it up: the skill resumes it from its Open Questions, then writes the acceptance tests and the Technical
Plan. It builds on spec 7's .kiln/, whose kiln.json lists each project's reachable hosts and writable paths. -->

## Overview

kiln builds a Python project that uses uv, as it builds a Go project today. The first other language is the case;
the seams it opens are what a third would use.

## What is Go's in kiln's code today

- **The gate's failed tests.** `failedTests` in `internal/kiln/gate.go` reads `go test`'s `--- FAIL:` and `FAIL`
  lines, so the gate can pass a test that fails at main too. Any other command's failure fails the gate whole, even
  when main fails it the same way.
- **The audit's scope.** `goPackages` in `internal/kiln/build.go` names the Go packages a build changed. A build that
  changes no `.go` file gets no audit.
- **`kiln init`'s defaults.** It writes `go vet ./...` and `go test ./...` as the gate, and Go's hosts and caches.
- **The default build agent's acceptance tests** are Go tests named `TestAT<N>`. No kiln code reads that name.

## Open Questions

1. **One reader per language, or one shared format?** pytest writes JUnit XML with `--junitxml`, and so do
   Playwright and most runners; `go test` does not, but `go test -json` is its own. Lean: two readers, `go test`'s and
   JUnit XML's, picked per gate command in `kiln.json`, rather than an adapter per language.
2. **What scope does the audit get outside Go?** Lean: the directories that hold the changed source files, which is
   what `goPackages` is for Go.
3. **How does kiln's own gate test a Python build?** The machine kiln is built on has no `uv` (checked 2026-11-10).
   An acceptance test of a Python build needs `uv`, and the packages it installs, offline, as Go runs from the
   developer's caches today.
