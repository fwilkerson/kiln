# Spec 7: A project's own agents

Status: Approved, 2026-11-10

## Overview

A project tells kiln how each of its build's sessions runs: what it is told, which tools it has, and its model and
effort. It says so in `.kiln/`, a directory on its own main that holds kiln's settings and agents, and kiln's next
build runs with them. `kiln init` writes kiln's own agents there, so a project starts from them and can read what
each does. Today the developer changes kiln itself for any of this, and installs the new kiln before approving.

kiln calls each kind of session an agent. A build has two, in a fixed order: the build agent, which writes the work,
and the audit, which reads it once the gate passes and sends its findings back to the build agent.

What showed the need: spec 3's view needed the audit to drive a TUI, so the audit needed Bash. The audit's tools are
fixed in kiln's code and its prompt compiled into kiln's binary, so the change had to land in kiln before spec 3 was
approved (c9d511d, "feat(audit): let the audit drive a TUI's model with Bash").

## What a project can set today

- **Permissions, per project: yes.** kiln's sessions read the project's `.claude/settings.json`
  (`--setting-sources project`, `arguments` in `internal/claude/claude.go`), so an allow rule there reaches every
  agent.
- **Tools and prompt, per agent: no.** `build.sandboxed` in `internal/kiln/build.go` fixes every agent's denied
  commands, forbidden actions, writable paths and reachable hosts. `build.audit` fixes the audit's tools. The audit's
  prompt is compiled in (`guidance.Audit`), and the `tools:` line of `guidance/agents/audit.md` is never read. The
  build agent's prompt is `internal/kiln/build.md`, compiled in too.
- **Model, effort and advisor: no.** Since c546d7c, every agent runs `--model opus --effort medium` (`arguments` in
  `internal/claude/claude.go`), with `advisorModel: fable` in the settings kiln writes (`settings` in
  `internal/claude/settings.go`). `guidance/agents/audit.md` says `effort: high`, which reaches the interactive `audit`
  subagent, but kiln's headless audit never reads it and runs at medium.
- **Reachable hosts and writable paths are Go's.** Every agent reaches only `proxy.golang.org`, `sum.golang.org` and
  `storage.googleapis.com`, and writes only beside Go's caches.

## How kiln's own builds have gone

Read from `.git/kiln/ledger.jsonl` and each session's stream on 2026-11-10. It covers five builds of four specs,
stats-tui twice.

- Every build ran the build agent, the gate, the audit, the build agent again on the findings, the gate, then landed.
  One gate failed and went back for one round (tui-reading).
- Every audit found something: 5, 5, 4, 2 and 5 findings. Each time the build agent answered that it fixed them all.
- Nothing but the gate checked the fixes. The audit runs once per build, as spec 1 decided. One fix broke tui-reading's
  test 3, and the gate caught it. notify's fix took a different design than the audit proposed, and said why.
- No audit run changed a file.
- Two audits found `go` on the session's PATH did not match `GOROOT`, and worked around it. The tui-reading audit, the
  latest, did not.

## Goals

- kiln packages the whole process for any project: `/shape`, the skill that writes a spec, which `/slice` was, with
  its three reviewer agents, the build and audit agents, and kiln's settings. All of it lives in `.kiln/` on the
  project's main.
- A project sets each agent's prompt, tools, model and effort in its file in `.kiln/`. kiln reads the build and audit
  agents' files once, at `kiln queue`, from the commit it queues, which is main's tip. Every session of that build
  runs with them, so neither a build nor a later commit to main changes the agents of a build already queued.
- The build agent's file is kiln's defaults, with sections a project tailors to itself, such as how it writes an
  acceptance test: a Go test, a Playwright script, an HTTP request or a terminal. kiln keeps the idea of acceptance
  tests; how a project writes them is the project's.
- kiln's contract with each agent is kiln's, compiled into kiln, and no agent's file holds it, so a kiln upgrade
  reaches every project and no project can delete a line of it. kiln writes one system prompt for each session: the
  agent's file's body, then its contract. The build agent's contract: the spec is the approved input, and its Given,
  When, Then are the developer's; hands off `.kiln/` but the amendment file, whose `### Old` and `### New`
  `internal/kiln/amend.go` parses; don't build a spec whose Status is Proposed; kiln commits, gates and lands, and
  nothing is pushed; files meant for `.kiln/`, such as an agent or a skill, are the developer's, so the build names
  them in its last reply instead of writing them. The audit's contract: the packages and the spec it reads, that it
  changes nothing, and that its reply ends with `read N packages, found M`, which `findingsOf` reads as today.
- No agent or skill names `CLAUDE.md` or a file it links. A session loads `CLAUDE.md` and each file it imports with
  `@` (tried 2026-11-10, below).
- `kiln.json`, the specs and their amendments move into `.kiln/`, beside the agents and the skill.
- kiln refuses to queue a spec that does not follow the skill's template. A project may tailor the skill, but not the
  form of the spec it writes. A spec may add sections of its own between the template's.
- An agent's file names its prompt, `tools:`, `model:`, `effort:` and `advisor:`. kiln ignores any other key, such
  as the `name:` and `description:` a Claude Code subagent carries, so a file linked into `.claude/agents/` works as
  both. A file without `tools:` has every tool; without `model:` or `effort:`, Opus at medium effort; without
  `advisor:`, Fable, as every session does today. kiln's own audit file says `effort: high`, so kiln's audit runs at
  high effort from the first build after this one. The advisor is a settings key with no flag, so kiln writes it into
  the settings it passes.
- Reachable hosts and writable paths are the project's, in `.kiln/kiln.json`, which `kiln init` fills with Go's.
  A writable path differs by machine, so the file names the commands that print them, as `go env GOCACHE
  GOMODCACHE` does in `Repo.goCaches` today. kiln writes both into the one settings file it passes, as it writes
  Go's from `build.sandboxed` today.
- What no file changes stays kiln's: the denied commands and forbidden actions, such as pushing and moving main; the
  guard on `.kiln/`; `--permission-mode auto`, `--setting-sources project`, `--output-format stream-json` and the
  sandbox's switches. These are the lines no session crosses, and kiln reads the stream.
- A build writes nothing in `.kiln/` but its spec's amendment file. The guard and the gate refuse the rest, so a
  build changes neither the gate nor an agent of a build after it. Today a build may change `kiln.json`, which the
  guard in `internal/kiln/guard.go` never names, and once it lands, the next build's gate is the changed one.
- kiln embeds a set of defaults, written for any project, and `kiln init` writes them into `.kiln/`. The project owns
  them from then on. Each default agent's file opens with a comment that invites the project to make it its own, and
  marks the sections a project tailors.
- kiln's own `.kiln/` is what `kiln init` writes, tailored to kiln, as any project would tailor it. kiln dogfoods
  the defaults, and its own files show a project how to tailor them. `guidance/` goes, by hand, after the build
  lands (Technical Plan, "Moving kiln's own files").

A headless session's `CLAUDE.md`, tried 2026-11-10 with `claude -p --setting-sources project --tools ""` in a scratch
repository: it knew a word written in `CLAUDE.md` and one in a file `CLAUDE.md` imports with `@docs/imported.md`. It
did not know one in a file `CLAUDE.md` only links, `[practices](docs/linked.md)`; a session reads such a file only
when it chooses to.

The contract goes in the system prompt, not the first task, since a compacted session keeps its system prompt whole
and summarizes its turns (believed, not tried). A resumed session keeps its first task too: one told a word in its
first `claude -p` prompt named it after `--resume` (tried 2026-11-10). No kiln session has compacted yet.

## Non-goals

- Who may write which path, by agent. `docs/later.md` "Who may write what" holds it.
- Auditing the fixes again. The audit reads a build's work once, as spec 1 decided. `docs/later.md` "An audit of the
  fixes" holds it.
- Agents a project names, their order, and which agent findings go back to. `docs/later.md` "A project's own
  workflow" holds it.
- Building a project in another language. `specs/languages.md`, a starter spec, holds it: the gate's reading of
  failed tests, the audit's scope, and `kiln init`'s defaults are Go's in kiln's code.

## Open Questions

None.

## Acceptance tests

<!-- A Then about a session is read from what kiln passed to the scripted claude, as spec 1's tests read it. -->

### 1. An agent's file on main sets its session

- **Given**: a project whose main holds an audit file naming its prompt, tools, model, effort and advisor, and an
  approved spec.
- **When**: the developer queues the spec and runs kiln.
- **Then**: the audit runs with that file's prompt, tools, model, effort and advisor.

### 2. A build cannot change kiln's files

- **Given**: an approved spec whose build changes an agent's file and the gate's commands in `.kiln/`.
- **When**: the developer queues the spec and runs kiln.
- **Then**: the gate fails, and `.kiln/` on main is as it was.

### 3. kiln's contract follows the agent's own prompt

- **Given**: a project whose main holds a build agent's file with instructions of its own.
- **When**: the developer queues a spec and runs kiln.
- **Then**: the build agent is told the file's instructions, then kiln's contract.

### 4. An agent's file cannot cross kiln's lines

- **Given**: a project whose main holds a build agent's file that names every tool.
- **When**: the developer queues a spec and runs kiln.
- **Then**: the build agent still runs with kiln's denied commands, forbidden actions and guard on `.kiln/`.

### 5. A project names its own hosts and paths

- **Given**: a project whose `.kiln/kiln.json` on main names a host, and a command that prints a writable path.
- **When**: the developer queues a spec and runs kiln.
- **Then**: each session may reach that host and write that path, and reaches no host the file does not name.

### 6. A new project starts from kiln's files

- **Given**: a repository with no `.kiln/`.
- **When**: the developer runs `kiln init`.
- **Then**: `.kiln/` holds kiln's settings, the build and audit agents, and `/shape` with its three reviewer agents,
  and a build runs with them.

### 7. kiln refuses a spec that leaves the template

- **Given**: an approved spec in `.kiln/specs/` whose form leaves `/shape`'s template.
- **When**: the developer queues it.
- **Then**: kiln refuses it, and `kiln status` does not show it queued.

## Technical Plan

Facts it rests on were checked on 2026-11-10, with Claude Code 2.1.321.

### `.kiln/`

```text
.kiln/
  kiln.json                    the gate, its rounds, reachable hosts, commands that print writable paths
  agents/build.md              frontmatter and body: the project's
  agents/audit.md
  agents/zoom-out.md           the reviewers /shape runs
  agents/comprehension.md
  agents/critic.md
  skills/shape/SKILL.md        /shape, which /slice was
  skills/shape/template.md
  specs/<name>.md              and <name>.amend.md
```

- `Spec.file`, `Spec.amendPath` and the spec list in `internal/kiln/kiln.go` name `.kiln/specs/`. `configFile` in
  `internal/kiln/gate.go` is `.kiln/kiln.json`. `PRODUCT.md` stays at the root: it is the project's record, and its
  commands name the commits that held each spec.
- kiln reads only `.kiln/`, with one exception. A spec read at an old commit, by `view.Text`, `Repo.changed` or
  `build.begin`, is read at `.kiln/specs/<name>.md`, and at `specs/<name>.md` where that commit holds no `.kiln/`
  spec, so `kiln tui` still shows each build's spec as its commit held it.
- `kiln queue` refuses while main holds no `.kiln/kiln.json`, `.kiln/agents/build.md` or `.kiln/agents/audit.md`,
  and names `kiln init`, as `Repo.config` refuses a main with no `kiln.json` today.
- The guard in `internal/kiln/guard.go` refuses any path under `.kiln/` but `.kiln/specs/<name>.amend.md`, folding
  case as it does for `specs/` today. The gate refuses the same paths, however they were written. An accepted spec's
  landing still changes its own spec and amendments. `Precommit` reads `.kiln/kiln.json` from the working tree, as it
  reads `kiln.json` today, so a commit that adds the file is gated by it.
- `kiln.json` gains `hosts`, a list of host names, and `writable`, a list of commands. kiln runs each command once
  per `kiln run`, in the main checkout, as `Repo.goCaches` runs `go env` today, and each field it prints is a path a
  session may write. `kiln init` writes `proxy.golang.org`, `sum.golang.org` and `storage.googleapis.com`, and
  `go env GOCACHE GOMODCACHE`.

### Agents

- `kiln queue` reads `.kiln/agents/build.md` and `.kiln/agents/audit.md` at the commit it queues, `record.Approved`,
  and refuses a file it can't use: one whose frontmatter has no closing `---`, or whose `effort:` is not one of
  `low`, `medium`, `high`, `xhigh` and `max`. Every launch of the build reads them at that commit with `git show`,
  never from a worktree.
- A file is frontmatter between lines of `---`, then the body. kiln passes `tools:`, a comma-separated list of tool
  names as Claude Code's agent files write it, as `--tools`; `model:` as `--model`; `effort:` as `--effort`; and
  writes `advisor:` as `advisorModel` in the settings file it passes. `claude --help` lists each flag. kiln ignores
  every other key.
- kiln writes one system prompt file per session, as `internal/claude` does today: the agent file's body, then
  kiln's contract for the agent, compiled into kiln. The build agent's contract takes the place of
  `internal/kiln/build.md`. The audit's ends with the line `findingsOf` reads.
- kiln's own taste in `internal/kiln/build.md` and `guidance/agents/audit.md`, which tests to write and what counts
  as a finding, goes into the default agents' bodies, without naming `CLAUDE.md` or `docs/`. `build.md` and the
  `guidance` package go.
- A limit: kiln can't check a model, an advisor or a tool name at queue. A session started with a wrong one fails at
  its start, and Claude Code's reason is in the session's `stderr` file under `.git/kiln/sessions/`. A build agent's
  failed first run still goes on as work that changes nothing, as `build.start` handles one today.

### Defaults and `kiln init`

- The defaults live in a directory of a package in `internal/kiln`, embedded with `go:embed`. They are written for
  any project: no line names kiln, and the Go in them is in the sections marked for the project to tailor.
- `kiln init` writes each default into `.kiln/` where `.kiln/` holds no file of that name. To a `.kiln/kiln.json` that
  exists, it adds only the keys the file lacks. It links each of `/shape` and its three reviewers into
  `.claude/skills/` and `.claude/agents/` where `.claude/` holds no file of that name, and links neither the build
  agent nor the audit: a developer who wants either as a subagent links it.
- `linkGuidance` in `internal/kiln/build.go`, which links `.claude/` to `guidance/` at a landing, goes.

### The spec's form

- `kiln queue` refuses a spec that leaves `/shape`'s template. The template's `##` sections must all appear, in the
  template's order; a spec may add `##` sections of its own between them, as this one adds "What a project can set
  today". Each `### N. Title` under Acceptance tests holds a Given, a When and a Then, and the numbers run from 1.
- kiln takes the sections from its embedded `template.md`, so the check and the default template can't drift. A
  project's change to its own copy of `template.md` does not change the check.

### What the build changes besides kiln's code

- The scratch repositories of specs 1 to 6's acceptance tests hold their specs and `kiln.json` under `.kiln/`. Their
  Thens, which name `specs/`, hold with `specs/` read as `.kiln/specs/`. Each script's main also holds the agent
  files `kiln queue` now requires.
- `README.md` and `CLAUDE.md` name `.kiln/` where they name `specs/`, `kiln.json` or `guidance/`.
- It leaves `specs/`, `kiln.json` and `guidance/`'s Markdown files where they are: today's gate fails on any change
  under `specs/`, so the build can't move kiln's own files.

### Moving kiln's own files

Once the build lands, and before `kiln accept project-roles`, in this order:

1. The developer reinstalls kiln from main with `go run ./tools/install`, so the kiln that accepts, and the
   pre-commit hook, read `.kiln/`.
2. An interactive session removes the `.claude/agents` and `.claude/skills` links, moves `specs/` to
   `.kiln/specs/` and `kiln.json` to `.kiln/kiln.json`, and runs `kiln init`. That writes the defaults beside them,
   adds the hosts and writable commands to `kiln.json`, and links `/shape` and its reviewers into `.claude/`. The
   session tailors the default agents with what `guidance/` holds that the defaults don't, removes `guidance/`, and
   commits on main.
3. The developer runs `kiln accept project-roles`.

## Alternatives Considered

### Each agent's file in `.claude/agents/`

- **Pros**: Claude Code reads the same file as an interactive subagent.
- **Cons**: a project's own `audit` or `build` agent, written for something else, would become kiln's. kiln mixes its
  files with the project's.
- **Reason for Rejection**: kiln's files go in `.kiln/`. A developer who wants one as a subagent links it into
  `.claude/agents/`, as kiln does today.

### Each agent's fields in `kiln.json`

- **Pros**: one file holds everything kiln reads from main.
- **Cons**: the prompt is Markdown, and an agent file already holds prompt, tools, model and effort together.
- **Reason for Rejection**: an agent is a prompt with its settings, in one file.

### Defaults compiled into kiln, used where a file is missing

- **Pros**: nothing to reconcile until a developer changes an agent.
- **Cons**: what an agent does is hidden in kiln's binary.
- **Reason for Rejection**: `kiln init` writes them, so a project reads what its agents do.

### kiln's own `.kiln/` as the defaults

- **Pros**: one copy, so kiln's builds run the files kiln ships.
- **Cons**: kiln's own lines, such as the audit's terminal UI section that drives a model with `go test -overlay`,
  reach every project.
- **Reason for Rejection**: kiln's `.kiln/` comes from `kiln init` and is tailored, as a project's would be. That
  dogfoods the defaults, and shows a project how to tailor them.

### The audit's findings through `--json-schema`

- **Pros**: Claude Code checks the reply against the schema, and kiln reads `structured_output`, not a line of text.
  Eidos did this at `07992d10`.
- **Cons**: a schema, a new field read in `internal/claude`, findings turned back into text for the build agent,
  and the scripted claude taught the flag. Its reason was that a project's audit file could drop the
  `read N packages, found M` line, and the contract now follows the file's body, where no project can drop it.
  `findingsOf` already treats a reply with no count as all findings, so a lost line costs a round, not a finding.
- **Reason for Rejection**: the contract line does the same work with code kiln has.
