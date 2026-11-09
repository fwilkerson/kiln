# Spec N: A project's own agents

Status: Proposed

<!-- A starter, taken from docs/later.md on 2026-11-09. Run /slice project-roles to take it up: the skill resumes it
from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

A project tells kiln what each of its sessions is told and what it may use, in a file on its own main, and kiln's next
build runs with it. Today the developer changes kiln itself for that, and installs the new kiln before approving.

What showed the need: spec 3's view needed the audit to drive a TUI, so the audit needed Bash. The audit's tools are
fixed in kiln's code and its prompt compiled into kiln's binary, so the change had to land in kiln before spec 3 was
approved (c9d511d, "feat(audit): let the audit drive a TUI's model with Bash").

## What a project can set today

- **Permissions, per project: yes.** kiln's sessions read the project's `.claude/settings.json`
  (`--setting-sources project`, `internal/claude/claude.go`), so an allow rule there reaches every role.
- **Tools and prompt, per role: no.** kiln fixes the build's tools, denied commands, forbidden actions, writable paths
  and reachable hosts in `internal/kiln/build.go:295-305`, and the audit's tools at `internal/kiln/build.go:443`. The
  audit's prompt is compiled in (`guidance.Audit`), and the `tools:` line of `guidance/agents/audit.md` is never read.
- **Model, effort and advisor: no.** Since c546d7c, every role runs `--model opus --effort medium`
  (`internal/claude/claude.go`), with `advisorModel: fable` in the settings kiln writes (`internal/claude/settings.go`).
  The `model:` and `effort:` lines of `guidance/agents/audit.md` reach the interactive `audit` subagent, but kiln's
  headless audit never reads them.
- **Reachable hosts and writable paths are Go's.** The build reaches only `proxy.golang.org`, `sum.golang.org` and
  `storage.googleapis.com`, and writes only beside Go's caches.

## Goals

- kiln reads a role's agent file from the project's main, as it reads `kiln.json` from main's tip, so a build cannot
  widen its own tools.
- A project without the file gets kiln's own, as today.

## Non-goals

- Who may write which path, by role. `docs/later.md` "Who may write what" holds it.
- Naming tests in languages other than Go. `docs/later.md` "Repositories in other languages" holds it.

## Open Questions

1. **Where does a project say what a role is told and may use?**

   | Option                                                                       | One file per role says both | Claude Code's own format |
   |------------------------------------------------------------------------------|-----------------------------|--------------------------|
   | a) Tools and rules per role in `kiln.json`, beside the gate                  | No: prompt stays in kiln    | No                       |
   | b) `.claude/agents/<role>.md`, prompt and `tools:`, from main                | Yes                         | Yes                      |
   | c) The project defines the workflow: roles, order, what a failure sends back | Yes                         | No                       |

   Lean b). It is where kiln's own audit already lives (`.claude/agents` links to `guidance/agents/`), so for kiln
   itself, an edit to `guidance/agents/audit.md` would take effect at the next build, with no new kiln. a) keeps a
   second place for the same fact. c) has no case yet: every build so far ran build, then audit.

2. **Does a project's file replace kiln's prompt for the role, or add to it?** Lean: replace for the audit, add for
   the build. The audit's prompt is all review, and a project may want a different review. The build's prompt,
   `internal/kiln/build.md`, carries kiln's contract: the `TestAT<N>` names, amendments, and where guidance goes. A
   project's build file that replaced it could drop the contract the gate and acceptance depend on.

3. **What stays kiln's, whatever the file says?** Lean: the denied commands and forbidden actions, such as pushing and
   moving main, and the guard on `specs/`. These are the lines no session crosses. Hosts and writable paths go to the
   project, since today's are Go's. Whether Claude Code joins a project's sandbox hosts in `.claude/settings.json` with
   the ones kiln sets is believed, not tried.

4. **Is c) out of this spec?** Lean yes, until a project needs a role other than build and audit.

5. **Do a role's model and effort come from its agent file?** Lean yes, from the `model:` and `effort:` lines of the
   same file as b)'s prompt and `tools:`, with kiln's Opus at medium effort when a line is missing. An agent file
   already carries both, so a project sets them where it sets the role's tools.

6. **Who sets the advisor?** Lean: the project, with kiln's Fable only when the project's settings name no
   `advisorModel`. The advisor is a settings key with no flag and no line in an agent file, so it can't come from the
   role's file. A project's `.claude/settings.json` with `advisorModel` gives a headless session the tool (tried
   2026-11-09). That kiln's own `--settings` value outranks the project's, so a project can't override Fable today, is
   believed, not tried.

7. **Which of the rest of what kiln passes stay kiln's?** These are `--permission-mode auto`,
   `--setting-sources project`, `--output-format stream-json`, the sandbox's switches, and the auto mode rules. Lean:
   all stay kiln's. kiln reads the stream, and the build's guard and refusals rest on the rest. Question 3 sorts only
   the denied commands, forbidden actions, hosts and writable paths.
