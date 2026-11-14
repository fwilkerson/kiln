# Spec N: kiln fire, in place of kiln run

Status: Proposed

<!-- A starter, drafted on 2026-11-14 from the developer's note "Rename kiln run to fire?". Run /shape fire to take it
up: the skill resumes it from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

`kiln run` builds each queued spec, one at a time, until none is queued. A potter loads a kiln with ware, then fires
it until the ware is done, which is the same cycle. So the command becomes `kiln fire`:

```text
kiln queue <name>   load the spec
kiln fire           build each queued spec until none is queued
```

What showed the need: the developer's note of 2026-11-14 asks to rename `kiln run` to `fire`. "Run" names no part of
what the command does, and kiln has a second `run`, in `kiln monitor run`.

On 2026-11-14, `kiln run` appears 169 times in 76 files. Most are scenarios under `uat/testdata/` and the acceptance
tests of specs not yet accepted, then `internal/kiln`, the README and the zsh completion. kiln prints it too, in
`kiln status`'s next step and in the window a stopped build opens.

## Goals

- `kiln fire` does what `kiln run` does today, and each line kiln prints names `kiln fire`.
- The README, the package docs, the completion and the scenarios name `kiln fire`.

## Non-goals

- A change to what the command does.

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

1. Does `kiln run` stay as a second name? Lean: no. Nothing installed calls it: the launchd job calls
   `kiln monitor run`, and the only user is the developer, who reads the new name in the README.
2. Does `kiln monitor run` keep its name? Lean: yes. It runs each due monitor once, which is no firing, and its
   launchd job names it. Its `run` then means one thing in kiln.
