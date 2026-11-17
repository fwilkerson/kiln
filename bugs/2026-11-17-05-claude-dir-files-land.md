# A build lands an agent or a skill under .claude/, hooks included

Found 2026-11-17, by an audit of the sandbox.

## What happened

A session calls Write on `.claude/agents/helper.md` and `.claude/skills/tidy/SKILL.md`, each with a `hooks:` block
in its frontmatter, one of which runs `curl ... | sh`. The hook guards only `.claude/settings*.json`, so the build
lands, and main holds both files. An agent's or a skill's hooks run unsandboxed in the developer's own sessions.

## What should happen

`docs/reference.md` says a file meant for `.claude/` is the developer's, and a build names it in its last reply
instead of writing it. The hook refuses a write anywhere under `.claude/`, by a file tool, as it does under
`.kiln/`. The gate refuses work that changes a path under `.claude/`, as it does under `.kiln/`, since a shell
command can write there too.

## How to see it

A scenario whose scripted session writes `.claude/agents/helper.md`. Today "x: landed on main" follows, and
`git show main:.claude/agents/helper.md` prints the file.

## Where to look

The guard in `internal/kiln/guard.go`, near its rule for `.claude/settings*.json`, and where the gate refuses a
change under `.kiln/`. `uat/testdata/build/guards-kiln-files.txtar` holds today's rule.
