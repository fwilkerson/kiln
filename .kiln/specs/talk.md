# Spec N: Talk with a stopped build's session, then let kiln finish it

Status: Proposed

<!-- A starter, drafted on 2026-11-09 while slicing specs/notify.md. Run /shape talk to take it up: the skill resumes
it from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

A build stops. The developer hops into its session, answers its question or points it at the problem, and exits.
kiln then finishes the build from where the session left it: it gates the work, audits it, and lands it or leaves it
waiting on its branch.

What showed the need: while slicing `specs/notify.md`, the developer asked to "hop in, answer a question, and then
exit / detach allowing kiln to finish". Today, `specs/notify.md` prints the command that resumes a stopped build's
session, but `kiln queue <name>` then drops its branch and builds anew from main. `specs/commands.md` replaces
`kiln approve` with `kiln queue`, and lets `kiln accept` land again an accepted landing that stopped.

## Goals

- A stopped build can go on in the same session, on the same branch, once the developer has talked with it.
- A build that stopped on a kiln error can go on once the developer fixes what the error names. Today such a build
  is built anew, even one its gate and audit passed that stopped only because main's checkout was in the way of its
  landing. `specs/notify.md`'s critic found this.

## Non-goals

- Attaching to a session while it builds. A session runs headless, as `claude -p`, and has no terminal to attach to.

## Open Questions

1. **Which shape?** Each builds on the one above it. Sizes are rough, against `b96c36a`, which added
   `kiln approve --again` in 115 changed lines across 8 files.

   | Option                                                                                        | The developer runs                           | Rough size                                                            |
   |-----------------------------------------------------------------------------------------------|----------------------------------------------|-----------------------------------------------------------------------|
   | a) A command that queues a stopped build again, in its session                                | `claude --resume`, exit, then a kiln command | Like `b96c36a`: a state change, a prompt, one or two tests            |
   | b) One kiln command that opens the session, and on exit queues the build and runs it in place | One kiln command, then exit                  | a) plus an interactive launch and a run in place: 2 to 3 times a)     |
   | c) b), plus a build that stops to ask a question about its spec                               | Waits for the notification, then as b)       | b) plus a fourth waiting state, a way to ask, and rules: its own spec |

   Lean b). It is the "exit, and kiln finishes" you asked for, and most of it is code kiln has:
   - A queued build with a session already goes on in that session. `Run` puts it first, and `build.run` resumes it
     with a prompt, as it does for an interrupted build.
   - `kiln accept` already runs a build in place, through `acceptAmended`, or leaves it to the `kiln run` that is
     building.
   - The new part is an interactive launch in `internal/claude`, with the developer's terminal attached. It keeps
     kiln's settings, so the hook still guards `specs/`.

   c) changes what kiln promises: the README says the developer is "asked nothing but about the spec", and the
   `/slice` skill says a build never asks the developer anything. What decides between b) and c): whether builds stop
   on questions only you can answer. Eidos's evidence in `docs/later.md` counts 4 parks of 122 about the product.

2. **What is the command named?** For example `kiln talk <name>` or `kiln resume <name>`. Lean `kiln talk`: it says
   what the developer does. `kiln resume` could read as going on with a build interrupted by a usage limit or a
   halted `kiln run`, which `kiln run` already does.

3. **How many rounds does the build get once the developer exits?** The build stopped because its gate failed past
   its last round. Lean: start its rounds again at 0, so the gate's next failure goes back to the session, as in a
   new build.

4. **What does the ledger count for the time you talk?** An interactive session prints no `stream-json`, so kiln
   can't read its turns or cost as it does a headless run. Lean: record the run with its time, and its turns and cost
   as unknown, which kiln's ledger already shows as unknown, never as zero.
