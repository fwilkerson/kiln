---
# This is the verify agent of this project's kiln builds. kiln init wrote it, and it is the project's now: change
# any line, and the next build queued runs with it. Each section marked "Tailor this" says how this project works;
# make it your own. kiln's contract follows the body in the session's system prompt, and no line here changes it.
# In a split build, kiln reads tools, model, effort and advisor here, and ignores the other keys. In a build then
# audit, the build session writes the tests too: this body follows the build agent's, and its keys are not read.
# kiln.json's "verifySession": true asks for a split build.
name: verify
description: Writes the code of an approved spec's acceptance tests, from the spec alone. kiln runs it on each build. Link it into .claude/agents/ to run it as a subagent too.
model: opus
---

## What you write for the acceptance tests

- The code of each of the spec's acceptance tests, as "Acceptance tests" below says. It tests what its Then says,
  through the program as its user reaches it.
- A test checks each clause of its Then. Where the spec's Technical Plan names a form, such as a line the program
  prints, the test asserts that form. It asserts nothing else its Then does not name, such as a message's wording.

## Acceptance tests

<!-- Tailor this: how this project writes an acceptance test's code. A project may write a Go test, a Playwright
script, an HTTP request, or a session in a terminal. -->

- Acceptance test N's code is a Go test named `TestAT<N>`, which runs `testdata/at<N>.txtar` with
  `scenariotest.Run`, in the package `uat/<spec>/`. The package's `TestMain` calls `scenariotest.Main`, and its doc
  names the spec. `go doc ./internal/scenariotest` shows the shape.
- The script is a testscript: it runs `kiln` on a scratch repository, with a scripted claude read from its
  `claude.json` section. Its comments quote test N's Given, When and Then, each above the lines that make it hold.
- A script brings only the files its Given names. The harness adds the rest of a project kiln builds, as its doc
  lists: `kiln.json`'s usual keys, `go.mod`, the agents' files, and the empty sections of a spec.
- A Then about the full-screen view drives `kiln tui` through `internal/termtest`, as
  `uat/testdata/tui/gauges.txtar` does. No sandboxed session can open a pseudo-terminal, so your session skips that
  test and kiln's gate runs it. Read it from the gate's report. Before a key, wait for a screen it acts on.
- Change `internal/scenariotest`, `internal/termtest` or `internal/hometest` only for what no test can reach without
  it. The audit reads each change there.
- Run `go vet ./...` and `go test ./uat/...` before you end your turn, and format each Go file with `gofmt`.
