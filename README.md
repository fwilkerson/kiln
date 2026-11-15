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
kiln accept <name>       take the work: retire the spec, or land its amendments first; or land a waiting fix
kiln reject <name>       turn the amendments away: drop the build, and its branch; or drop a waiting fix
kiln monitor install     set up a launchd job that runs kiln monitor run in the repository each hour
kiln monitor run [<name>]
                         run each monitor kiln.json names that is due; or run one now, due or not
kiln monitor show <name> show how a monitor's last run ended, in the window kiln opens for it
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
   which holds each gauge's readings too. Each session's stream, for why a number is what it is, is
   `.git/kiln/sessions/<id>/stream.jsonl`.
6. Run `kiln tui` in a terminal of its own, beside `kiln run`, to watch the running session and look back over each
   build run by run. It only reads.

### Monitors

A monitor is a job named in `kiln.json` that kiln runs on its schedule, in the background, with no terminal open. It
looks for a problem, and when it finds one, it fixes it. You come back to a fix already gated and landed.

1. Name the monitor in `kiln.json`'s `monitors`. Commit its check's script under `.kiln/monitors/`, or name a gauge
   in place of a check, with its script under `.kiln/gauges/`. Either is where a fix can't change it.
2. Run `kiln monitor install` once. A launchd job then runs `kiln monitor run` in the repository each hour, and on wake
   after a missed hour. It runs each monitor that is due, at main's tip: one that never ran, whose check or gauge
   couldn't run, or whose last run started its interval, less half an hour, ago. A monitor's gauge reads the whole
   repository there: a reading that crossed its line starts a fix, and one on its line is healthy. `kiln monitor run
   <name>` runs one now. `kiln monitor run` refuses while `kiln run` builds, and `kiln run` while it does.
3. A fix is a build with no spec, on `kiln/<name>`. Its command runs first. Its agent runs when the command left the
   gate failing, or when the monitor has none, and gets `kiln.json`'s rounds and the audit. The gate holds the fix,
   with the check added when it joins the gate. A gauge always joins it, read over what the fix changed: the gate
   fails while that reading crosses the line, or its command fails. A fix that passes lands, or waits on its branch for
   `kiln accept <name>` or `kiln reject <name>`. A fix that can't pass stops. An agent may ask for you instead, with
   a report: kiln then drops what it changed.
4. When a fix lands, stops or waits on you, an agent asks for you, or a session fails, kiln opens a Ghostty window
   in the background. It sends Ghostty's notification, and a click on it brings the window forward, with the summary,
   the agent's report or the session's error, and the command to run next.
5. Read `kiln status`: each monitor's result, when it last ran and runs next, and its summary. `kiln tui` lists the
   monitors beside the specs, and opens each fix as it opens a build.

A monitor is held, and starts no fix, while nothing changed since its last fix stopped, asked for you, was rejected,
or changed nothing. It is held after a fix landed too, while its check stays out of the gate and prints the same. A
monitor with a gauge is held after a landed fix while its entry and what the gauge prints are as they were, since
the gate read only what the fix changed. `kiln status` shows either as `held: landed`. Its check or gauge still runs
when due. An unattended session needs a long-lived sign-in: run
`claude setup-token` for one, since an interactive sign-in expires within days.

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
and its reviewers into `.claude/`. A build writes nothing in `.kiln/` but its spec's amendment file: the hook and the
gate refuse the rest.

```text
.kiln/
  kiln.json                    the gate, its rounds, reachable hosts, commands that print writable paths, MCP
                               servers, gauges, monitors
  gauges/                      the scripts a gauge's command runs, which a monitor may name, out of a build's reach
  agents/build.md              the build agent: frontmatter for its tools, model, effort and advisor, then its prompt
  agents/verify.md             how the acceptance tests' code is written, and the verify agent of a split build
  agents/audit.md              the audit, which reads the work once the gate passes
  monitors/                    the scripts a monitor's check runs, out of a fix's reach, for one that names no gauge
  agents/zoom-out.md           the reviewers /shape runs
  agents/comprehension.md
  agents/critic.md
  skills/shape/SKILL.md        /shape, which writes a spec
  skills/shape/template.md
  specs/<name>.md              a spec, and <name>.amend.md, its amendments
```

- `kiln.json` holds the gate's commands, the rounds a failing gate goes back to the session, the hosts a session's
  commands reach, and commands that print the paths beside its worktree they write. A session's commands may listen
  on a local port, in every project, so a test that starts a server runs in a session. Its `mcp` names the only MCP
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
  A monitor's run gives `$KILN_BASE` as git's empty tree, and its fix's gate as the commit where the fix left main.
  So `git diff --name-only --diff-filter=d "$KILN_BASE" HEAD` names every file there, or the files one change left.
  A command that names a commit's parents, such as `git log "$KILN_BASE"..HEAD`, fails at the empty tree. Read only
  the files the commit still holds, as `--diff-filter=d` lists them: a change that only deletes files then reads no
  number, and no deleted file fails the command. A gauge that a monitor names prints the same for the same finding,
  as a check does, with no line numbers and no timestamps, since the hold compares what it printed.
- The gate excuses a test that fails at main too. A command names its failed tests in `go test`'s output, or by
  writing JUnit XML to the path kiln gives it in `$KILN_JUNIT`. A command that fails and names no test is never
  excused, nor is a run that left out a test main ran.
- An agent's file is frontmatter between lines of `---`, then its body. kiln reads `tools:`, `model:`, `effort:` and
  `advisor:`, and ignores any other key. Without them, a session has every tool, and runs Opus. kiln sets no
  effort and no advisor of its own: a file that names neither leaves both to Claude Code. `kiln queue` reads the build,
  verify and audit agents at the commit it queues, and each
  session of that build runs with them. Its system prompt is the file's body, then kiln's contract with the agent,
  compiled into kiln. In build then audit, the build session writes the tests too, so the verify agent's body follows
  the build agent's. No file changes the contract, the denied commands and forbidden actions, or the guard on `.kiln/`.
- A monitor in `kiln.json`'s `monitors` has a `name`, as a spec's name, and `every`, a Go duration in whole hours, at
  least `1h`. It names a `command`, an `agent` or both. Its `agent` is a file under `.kiln/agents/`, such as
  `"triage.md"`, which names its session's role, and its `prompt` follows the check's or gauge's output, and the gate's,
  in the session's first message. Its optional `check` prints, as its last line, `{"fix": true|false, "summary":
  "..."}`. The rest of its stdout goes to the agent. A check that exits non-zero, or prints no such line, couldn't run,
  and starts no fix. In place of a check, its `gauge` names one of `gauges`, such as `"gauge": "health"`. Its reading of
  the whole repository is the monitor's summary, and what the gauge printed above its number goes to the agent. A
  reading kiln could not take, of no number or a failed command, couldn't run. A monitor names a check or a gauge, never
  both, and `gates` only beside a check. `"gates": false` keeps the check out of the gate, for a check that reads
  something a fix can't change until it is deployed. `"accept": true` keeps each fix waiting for `kiln accept`. A fix
  never writes under `.kiln/`, nor under `acceptance`'s part before `{spec}`, unless the monitor says `"tests": true`.
  An agent asks for the developer with the last line of its reply: `{"ask": true, "summary": "..."}`.
- A file such as an agent or a skill, meant for `.kiln/` or `.claude/`, is the developer's. A build names it in its
  last reply instead of writing it.

## Where things are

- `cmd/kiln`: the command line.
- `internal/kiln`: queuing, building, gating, landing, accepting and rejecting a spec, and running monitors and
  their fixes. `go doc ./internal/kiln` shows how.
- `internal/tui`: the full-screen view of `kiln tui`, on Bubble Tea. `internal/kiln` feeds it what kiln wrote.
- `internal/claude`, `internal/git`, `internal/proc`: Claude Code sessions, git, and child processes, from Eidos.
- `internal/scenariotest`: the scripted claude and the harness the acceptance tests run on.
- `internal/termtest`: a pseudo-terminal that runs a program and reads its screen, for the acceptance tests of
  `kiln tui`. No sandboxed session can open one, so those tests run only in kiln's gate.
- `uat/`: the acceptance tests. `uat/<spec>/` holds a spec's until the developer accepts it, `TestAT<N>` for test N.
  `uat/testdata/<feature>/` holds each accepted spec's scenarios, filed by feature, and `TestUAT` runs them.
- `internal/kiln/defaults/`: what `kiln init` writes into `.kiln/`, for any project. `internal/kiln/presets/`: each
  preset's `kiln.json`, and its text for the defaults' "Tailor this" sections. `internal/kiln/contract/`: kiln's
  contract with each agent.
- `.kiln/`: kiln's own, as `kiln init` wrote it, tailored to kiln. `.claude/skills` and `.claude/agents` link to
  `/shape` and its reviewers there. kiln's worktrees and clones hold the links as plain files, since Claude Code's
  sandbox
  refuses a shell's write through a link under `.claude/`.
- `cmd/kiln/_kiln`: the zsh completion, which `tools/install` copies.
- `tools/install`: reinstalls kiln and its completion from main's tip.
- `go tool guide`, `go tool style`, `go tool health` and `go tool size`: the commands of the module
  `github.com/fwilkerson/practices`, which `go.mod` names by the path of its checkout. `guide` prints how Go code is
  written and judged, and `docs/practices.md` adds kiln's own rules. `style` holds the rules gofmt leaves out, and
  `style_test.go` runs it in `go test ./...`. `health` scores each Go file, and `TestHealth` there fails when a
  score moves.
