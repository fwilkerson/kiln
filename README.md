# kiln

The developer approves a spec and walks away. kiln builds it with headless Claude Code sessions, gates it, and lands
it on main. The developer comes back to the spec met, asked nothing but about the spec.

`PRODUCT.md` holds, for each accepted spec, the `git show` command that prints it from the commit that last held it.

What spec 1 left for later is in `docs/later.md`. A plan the developer means to build waits in `.kiln/specs/` as a
starter spec, with Status Proposed, until `/shape` takes it up.

## Use

```text
kiln init [--preset go|python]
                         write a preset of kiln's defaults into .kiln/, Go's by default, and install the
                         pre-commit hook
kiln queue <name>        queue a build of .kiln/specs/<name>.md, as main holds it
kiln run                 build each queued spec, one at a time, until none is queued
kiln status              show where each spec stands, and each gauge's latest reading
kiln tui [<name>]        show every build, live and past, in a full-screen view; or open it on one spec
kiln accept <name>       take the work: retire the spec, or land its amendments first
kiln reject <name>       turn the amendments away: drop the build, and its branch
```

1. Write `.kiln/specs/<name>.md` with the `/shape` skill, in an interactive Claude Code session, and commit it on
   main.
2. Run `kiln queue <name>`, then `kiln run`. kiln queues only a spec whose Status is Approved, and whose form follows
   `/shape`'s template, as kiln embeds it.
   Queue only once the spec reads as you want it, diagrams included. `kiln tui` shows each build's spec as the
   commit it was queued at held it, so a later fix on main shows only in a build queued after the fix.
3. When a build waits on you, `kiln run` prints a terminal notification that names the spec and its state. Ghostty
   shows it with `desktop-notifications` on, and a click on it brings forward the terminal. There, the build sums
   itself up: its time, cost, rounds and audit, what it changed or what failed, and the command to run next. A
   build stopped past its last round names the command that resumes its session in its worktree, to ask it why. A
   split build names one for each session, in its own clone. A fix a session makes there is lost to
   `kiln queue <name>`, which builds anew from main. kiln removes every checkout a build made once it lands, is
   accepted, is rejected or is queued anew. A build that stopped, or waits on its branch, keeps them.
   Once a build lands, by `kiln run` or `kiln accept`, kiln reads each gauge `kiln.json` names. It prints each
   reading after the landing, beneath `kiln run`'s summary, marked where it crossed its line. Only then does the next
   build start.
   Read `kiln status`. A spec waiting for UAT is on main. A spec waiting on its branch with amendments shows them.
   Each gauge shows its latest reading, the line it was read against, and the landing it measured.
4. Run `kiln accept <name>`. A spec waiting on its branch lands with its amendments folded in. The spec is deleted,
   and `PRODUCT.md` gains the command that shows it. If that landing stops, `kiln accept <name>` lands it again.
   - When UAT finds a problem, change the spec with `/shape`, commit it, and run `kiln queue <name>`. The new build
     starts from main, which holds the landed work. kiln refuses to queue a landed spec whose spec is unchanged.
   - To turn amendments away, run `kiln reject <name>`. To change an amendment's wording, commit your own wording to
     the spec, then run `kiln reject <name>` and `kiln queue <name>`.
5. Read a landed build's commit for its time, cost, turns and failed calls. The ledger is `.git/kiln/ledger.jsonl`,
   which holds each gauge's readings too. Each session's stream, for why a number is what it is, is `.git/kiln/sessions/<id>/stream.jsonl`.
6. Run `kiln tui` in a terminal of its own, beside `kiln run`, to watch the running session and look back over each
   build run by run. It only reads.

To reinstall kiln from main's tip, with its zsh completion, run `go run ./tools/install` in this repository. It
builds main, never the working tree, and refuses while any kiln drives a build session. `go install` puts kiln in
`GOBIN`, and `_kiln` goes to `$XDG_DATA_HOME/zsh/completions`, which `fpath` must hold. The completion asks
`kiln complete` for kiln's commands, and the spec names each would not refuse.

## `.kiln/`

A project tells kiln how its builds run in `.kiln/`, on main. `kiln init` writes kiln's defaults there, as a preset
fills them, each where `.kiln/` holds no file of its name. The project owns them from then on. A preset is `go`, the
default, or `python`, for uv, pytest, ruff and ty. It writes `kiln.json` and the "Tailor this" sections of the build,
verify and audit agents. Run again, `kiln init` writes each file `.kiln/` lacks, such as `agents/verify.md`, and adds
to `kiln.json` only `rounds`, the one key every preset shares. So a project from before the split workflow adds
`acceptance`, and `harness` if it builds in split, to `kiln.json` by hand. It moves the acceptance-test text out of
an older `agents/build.md` into `agents/verify.md` by hand too: `kiln queue` refuses until it does. It links `/shape`
and its reviewers into `.claude/`. A build writes nothing in `.kiln/` but its spec's amendment file: the hook and the gate refuse the rest.

```text
.kiln/
  kiln.json                    the gate, its rounds, reachable hosts, commands that print writable paths, MCP
                               servers, gauges
  gauges/                      the scripts a gauge's command runs, out of a build's reach
  agents/build.md              the build agent: frontmatter for its tools, model, effort and advisor, then its prompt
  agents/verify.md             how the acceptance tests' code is written, and the verify agent of a split build
  agents/audit.md              the audit, which reads the work once the gate passes
  agents/zoom-out.md           the reviewers /shape runs
  agents/comprehension.md
  agents/critic.md
  skills/shape/SKILL.md        /shape, which writes a spec
  skills/shape/template.md
  specs/<name>.md              a spec, and <name>.amend.md, its amendments
```

- `kiln.json` holds the gate's commands, the rounds a failing gate goes back to the session, the hosts a session's
  commands reach, and commands that print the paths beside its worktree they write. Its `mcp` names the only MCP
  servers a session calls, each as Claude Code's MCP server settings hold one, such as
  `{"codescene": {"command": "cs-mcp-auth"}}`. They run outside the sandbox. kiln reads it from main's tip, so a build
  cannot weaken its own gate. The pre-commit hook runs the gate's commands on a commit to main.
- `kiln.json`'s `acceptance` is where a spec's acceptance tests live, with `{spec}` for its name, such as
  `"acceptance/{spec}/"`. `kiln queue` refuses a `kiln.json` without it. `reaudit` lists paths the audit must read
  whenever the work changes them, such as a harness or migrations. When the work's own change to one differs from what
  the audit last read, the audit reads the work again before it lands, as a round. kiln reads `reaudit` from main's
  tip too.
- `"workflow": "split"` builds each spec with two sessions at once, each in a clone of main of its own. The build
  session writes the code, and the verify session writes the acceptance tests from the spec alone, never seeing the
  code. Each session's settings deny it reads of the other's clone, and the hook keeps each to its side. The build
  session writes nothing under `acceptance` or in the amendment file. The verify session writes only there, and in
  `harness`, the paths both may write. kiln gates the build session's work alone, and its failures go to the build
  session. Then kiln stacks both sides on `kiln/<name>`, and gates the stack. Its failures go to the verify session,
  which fixes its tests, or names the tests the code fails, which go to the build session. The audit then reads the
  stack, and its findings go to both. kiln reads `workflow`, `acceptance` and `harness` at the commit the build was
  queued at.
- A gauge in `kiln.json` is a number the developer watches over the landings, such as a mutation score or test time.
  Each has a `name`, a `command` that prints the number as its last line, and exactly one of `min` and `max`, its
  line. A reading below `min`, or above `max`, crossed it. kiln runs each command with `sh -c` at the landed commit,
  and gives it main's tip before the landing in `$KILN_BASE`, so it can measure one build's work alone. A reading
  kiln could not take is unknown, never zero, and never crossed. A gauge never blocks a landing: a line that must
  hold is a gate command. Keep a gauge's script under `.kiln/gauges/`, where a build cannot change it.
- The gate excuses a test that fails at main too. A command names its failed tests in `go test`'s output, or by
  writing JUnit XML to the path kiln gives it in `$KILN_JUNIT`. A command that fails and names no test is never
  excused, nor is a run that left out a test main ran.
- An agent's file is frontmatter between lines of `---`, then its body. kiln reads `tools:`, `model:`, `effort:` and
  `advisor:`, and ignores any other key. Without them, a session has every tool, and runs Opus. kiln sets no
  effort and no advisor of its own: a file that names neither leaves both to Claude Code. `kiln queue` reads the build, verify and audit agents at the commit it queues, and each
  session of that build runs with them. Its system prompt is the file's body, then kiln's contract with the agent,
  compiled into kiln. In build then audit, the build session writes the tests too, so the verify agent's body follows
  the build agent's. No file changes the contract, the denied commands and forbidden actions, or the guard on `.kiln/`.
- A file such as an agent or a skill, meant for `.kiln/` or `.claude/`, is the developer's. A build names it in its
  last reply instead of writing it.

## Where things are

- `cmd/kiln`: the command line.
- `internal/kiln`: queuing, building, gating, landing, accepting and rejecting a spec. `go doc ./internal/kiln` shows how.
- `internal/tui`: the full-screen view of `kiln tui`, on Bubble Tea. `internal/kiln` feeds it what kiln wrote.
- `internal/claude`, `internal/git`, `internal/proc`: Claude Code sessions, git, and child processes, from Eidos.
- `internal/scenariotest`: the scripted claude and the harness the acceptance tests run on.
- `internal/termtest`: a pseudo-terminal that runs a program and reads its screen, for the acceptance tests of
  `kiln tui`. No sandboxed session can open one, so those tests run only in kiln's gate.
- `acceptance/<spec>/`: the code of each acceptance test, `TestAT<N>` for test N.
- `internal/kiln/defaults/`: what `kiln init` writes into `.kiln/`, for any project. `internal/kiln/presets/`: each
  preset's `kiln.json`, and its text for the defaults' "Tailor this" sections. `internal/kiln/contract/`: kiln's
  contract with each agent.
- `.kiln/`: kiln's own, as `kiln init` wrote it, tailored to kiln. `.claude/skills` and `.claude/agents` link to
  `/shape` and its reviewers there. kiln's worktrees and clones hold the links as plain files, since Claude Code's sandbox
  refuses a shell's write through a link under `.claude/`.
- `cmd/kiln/_kiln`: the zsh completion, which `tools/install` copies.
- `tools/install`: reinstalls kiln and its completion from main's tip.
- `tools/style`: the layout rules gofmt leaves out. `style_test.go` and `arch_test.go` hold them in `go test ./...`.
