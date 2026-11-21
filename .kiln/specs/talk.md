# Spec N: Talk with a stopped build or a monitor that needs you, then let kiln finish

Status: Proposed

<!-- A starter, drafted on 2026-11-09 while slicing specs/notify.md, and widened to monitors on 2026-11-21. Run
/shape talk to take it up: the skill resumes it from its Open Questions, then writes the acceptance tests and the
Technical Plan. -->

## Overview

A build stops, or a monitor needs the developer. The developer runs one command and lands in a Claude session that
already holds what went wrong. They answer its question, point it at the problem, or have it make the repair, and
exit. kiln then goes on from where the session left things. It gates, audits and lands a build, or leaves it waiting
on its branch. It runs a monitor again, so the developer sees whether the problem is gone.

What showed the need, for builds: while slicing `specs/notify.md`, the developer asked to "hop in, answer a question,
and then exit / detach allowing kiln to finish". Today, a stopped build prints the command that resumes its session,
but `kiln queue <name>` then drops its branch and builds anew from main. `specs/commands.md` replaces
`kiln approve` with `kiln queue`, and lets `kiln accept` land again an accepted landing that stopped.

What showed the need, for monitors: on 2026-11-20, kiln's `code-health` monitor asked for the developer on each run.
Its gauge listed a file whose finding the developer keeps, so its agent had no file to take. The agent's report named
the cause, but its session could not make the repair, since a fix never writes under `.kiln/`. The report's next
step read "read the report, see to it, then run kiln monitor run code-health". The developer asked for more: "when
there's an issue I'd like to take it into a claude session, that claude session should get the context of what the
problem is and have the tools to resolve it."

## Goals

- A stopped build can go on in the same session, on the same branch, once the developer has talked with it.
- A build that stopped on a kiln error can go on once the developer fixes what the error names. Today such a build
  is built anew, even one its gate and audit passed that stopped only because main's checkout was in the way of its
  landing. `specs/notify.md`'s critic found this.
- A monitor that needs the developer opens in a session that starts with its report: what its check or gauge
  printed, how its last fix ended and why, and where its directory is. The developer explains nothing twice.
- That session can repair what a fix's session cannot: the monitor's own files in `.kiln/monitors/<name>/`, the rest
  of `.kiln/`, and main.
- Once the developer exits, kiln runs the monitor again, held or not, and says how the run ended.
- Each place kiln says a build or a monitor needs the developer names the one command to run: `kiln status`,
  `kiln tui`, and the notification the `notify` program receives.

## Non-goals

- Attaching to a session while it builds. A session runs headless, as `claude -p`, and has no terminal to attach to.
- A fix that waits for `kiln accept`. The developer reads it, then accepts or rejects it, as today.
- A session that decides for the developer. It makes the repair they ask for, in their terminal.

## Open Questions

1. **Which shape, for a build?** Each builds on the one above it. Sizes are rough, against `b96c36a`, which added
   `kiln approve --again` in 115 changed lines across 8 files.

   | Option                                                                                        | The developer runs                           | Rough size                                                            |
   |-----------------------------------------------------------------------------------------------|----------------------------------------------|-----------------------------------------------------------------------|
   | a) A command that queues a stopped build again, in its session                                | `claude --resume`, exit, then a kiln command | Like `b96c36a`: a state change, a prompt, one or two tests            |
   | b) One kiln command that opens the session, and on exit queues the build and runs it in place | One kiln command, then exit                  | a) plus an interactive launch and a run in place: 2 to 3 times a)     |
   | c) b), plus a build that stops to ask a question about its spec                               | Waits for the notification, then as b)       | b) plus a fourth waiting state, a way to ask, and rules: its own spec |

   Lean b). It is the "exit, and kiln finishes" you asked for, and most of it is code kiln has:
   - A queued build with a session already goes on in that session. `Fire` puts it first, and `build.run` resumes it
     with a prompt, as it does for an interrupted build.
   - `kiln accept` already runs a build in place, through `acceptAmended`, or leaves it to the `kiln fire` that is
     building.
   - The new part is an interactive launch in `internal/claude`, with the developer's terminal attached. It keeps
     kiln's settings, so the hook still guards `specs/`. A monitor's talk needs the same launch.

   c) changes what kiln promises: the README says the developer is "asked nothing but about the spec", and the
   `/slice` skill says a build never asks the developer anything. What decides between b) and c): whether builds stop
   on questions only you can answer. Eidos's evidence in `docs/later.md` counts 4 parks of 122 about the product.

2. **What is the command named?** For example `kiln talk <name>` or `kiln resume <name>`. Lean `kiln talk`: it says
   what the developer does, for a build and a monitor alike. `kiln resume` could read as going on with a build
   interrupted by a usage limit or a halted `kiln fire`, which `kiln fire` already does. It takes a spec's name or a
   monitor's, as `kiln accept` does.

3. **How many rounds does the build get once the developer exits?** The build stopped because its gate failed past
   its last round. Lean: start its rounds again at 0, so the gate's next failure goes back to the session, as in a
   new build.

4. **What does the ledger count for the time you talk?** An interactive session prints no `stream-json`, so kiln
   can't read its turns or cost as it does a headless run. Lean: record the run with its time, and its turns and cost
   as unknown, which kiln's ledger already shows as unknown, never as zero.

5. **Which session does a monitor's talk open?**

   | Option                                         | It knows                                        | It may write                         |
   |------------------------------------------------|-------------------------------------------------|--------------------------------------|
   | a) The fix's own session, resumed in its tree  | All the fix read and tried                      | What a fix may: never `.kiln/`       |
   | b) A new session in main's checkout            | The report kiln hands it, and the fix's summary | What the developer may, through main |
   | c) b) for a monitor, a) for a fix that stopped | As b), or as a) once work stands on a branch    | As b), or as a)                      |

   Lean c). A monitor that needs you, that kiln refused, or whose check couldn't run has a problem in its own files
   or on main, which only a session on main may change. Its fix's work is dropped by then, so no branch is left to
   go on from. A fix that stopped with work on its branch is a stopped build with no spec, and goes on as question
   1 settles for a build.

6. **Which states of a monitor does it take?** Lean: needs you, stopped, refused, and couldn't run. Each is a state
   `kiln status` shows with a reason, and none clears on its own. It refuses a healthy monitor, and one whose fix
   builds, waits for `kiln accept` or lands.

7. **What does a new session start with?** Lean: one first message kiln writes, which holds the monitor's name and
   state, what its check or gauge printed, the agent's report or kiln's refusal, and the paths of its directory and
   its `agent.md`. It carries no instruction but to wait for the developer. The session is the developer's own: it
   runs outside kiln's sandbox, under their Claude Code settings, and the pre-commit hook gates each commit to main.

8. **What does kiln do when the developer exits a monitor's talk?** Lean: run that monitor at once, as
   `kiln monitor run <name>` does, which skips its hold. A session that committed nothing changes nothing the monitor
   reads, so the run ends as it did before, and says so. What decides: whether a run you did not ask for, which may
   start a fix that costs money, should wait for a yes.

9. **Does it replace `kiln monitor show`?** Today the notification's tab runs `kiln monitor show <name>`, which
   prints how the last run ended and waits for return. Lean: keep it for a run that ended well, and have it name
   `kiln talk <name>` as its next step for a monitor that needs you. A tab that opens straight into a session spends
   money before the developer has read why.
