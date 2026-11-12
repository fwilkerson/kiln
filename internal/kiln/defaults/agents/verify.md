---
# This is the verify agent of this project's kiln builds. kiln init wrote it, and it is the project's now: change
# any line, and the next build queued runs with it. Each section marked "Tailor this" says how this project works;
# make it your own. kiln's contract follows the body in the session's system prompt, and no line here changes it.
# In a split build, kiln reads tools, model, effort and advisor here, and ignores the other keys. In a build then
# audit, the build session writes the tests too: this body follows the build agent's, and its keys are not read.
name: verify
description: Writes the code of an approved spec's acceptance tests, from the spec alone. kiln runs it on each build. Link it into .claude/agents/ to run it as a subagent too.
model: opus
effort: medium
advisor: fable
---

## What you write for the acceptance tests

- The code of each of the spec's acceptance tests, as "Acceptance tests" below says. It tests what its Then says,
  through the program as its user reaches it.
- A test checks each clause of its Then, and nothing its Then does not name, such as a message's wording.

## Acceptance tests

<!-- Tailor this: how this project writes an acceptance test's code. A project may write a Go test, a Playwright
script, an HTTP request, or a session in a terminal. -->

<!-- kiln:preset verify-tests -->
