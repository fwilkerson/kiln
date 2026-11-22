# kiln

Approve a spec, walk away, and come back to it built, tested and on main.

A coding agent writes code fast, but only you start its tasks and check its results. So you wait on one agent, or
you switch between several, as many as you can manage.

kiln does that checking before the work reaches you. You approve a **spec**: a Markdown file that says what to build,
and how to tell it is done. kiln then runs [Claude Code](https://claude.com/claude-code) sessions that write the code
and its tests. The work lands on main only once your project's checks pass.

![You shape and queue a spec; kiln builds, gates, audits and lands it on main.](docs/flow.svg)

## Install

kiln runs on macOS. It needs git 2.55 or newer, which Xcode's git may not be, and Claude Code's `claude` command,
signed in.

```sh
go install github.com/fwilkerson/kiln/cmd/kiln@latest        # with Go 1.27 or newer
```

Or take a built binary, for an Apple M-series Mac (`arm64`) or an Intel Mac (`amd64`):

```sh
curl -fsSL https://github.com/fwilkerson/kiln/releases/latest/download/kiln_darwin_arm64.tar.gz | tar -xz
mv kiln_darwin_arm64/kiln ~/.local/bin/        # or any directory on your PATH
```

## Quick start

Start in a git repository on the branch `main`, whose tests pass. kiln works on that branch, and never pushes.

```sh
kiln init          # writes .kiln/: your checks, the agents' instructions and the /shape skill
git add .kiln .claude && git commit -m "chore: set up kiln"
```

`kiln init` picks settings for Go, Python with uv, or TypeScript with npm, by the files your repository holds. It
also links `/shape` into `.claude/`, where Claude Code finds it.

Next, open Claude Code, run `/shape`, and say what you want built. `/shape` asks you questions and drafts the spec,
`.kiln/specs/<name>.md`. When you approve the spec, `/shape` commits and queues it. Then build:

```sh
kiln fire              # builds each queued spec, and lands each that passes on main, as one commit
kiln accept <name>     # once you have tried the work
```

A build asks you nothing while it runs. `kiln tui` shows each session live.

## Acceptance tests

Each acceptance test in a spec is three sentences, which you approve:

```markdown
### 1. The answer is printed

- **Given**: nothing.
- **When**: someone runs `answer`.
- **Then**: it prints 42.
```

A build writes one automated test for each. The test runs your program as its user would, through the command line
or HTTP, and checks what the Then says. For a spec named `answer` in a Go project, the test above is a Go test in
`acceptance/answer/`.

Those tests stay in your repository, and your checks run them from then on. So each spec you accept adds to an
end-to-end suite, and a later build that breaks an earlier spec does not land. Your checks take longer with each
spec.

An agent wrote those tests, so kiln guards them:

- **No build rewords a test.** A build that finds a Given, When or Then wrong writes an amendment, and waits for you
  to accept or reject it.
- **Another session reads each test.** The **audit** is a session that wrote none of the work. The audit reports a
  test that checks less than its Then says.
- **Another session can write the tests.** With `"verifySession": true` in `kiln.json`, the session that writes the
  tests gets the spec alone. kiln denies that session any read of the code.

## The gate

The **gate** is your project's list of checks, in `.kiln/kiln.json`. Here is the file `kiln init` writes for a Go
project, without its sandbox keys:

```json
{
  "gate": ["go vet ./...", "go test ./..."],
  "rounds": 3,
  "acceptanceTests": "acceptance/{spec}/"
}
```

A failing gate goes back to the session that wrote the work, and each time is a **round**. After `rounds` rounds, the
build stops. Once the gate passes, the audit reads the work, and its findings go back to be fixed.

A build runs with no one watching, so kiln limits what a build can do:

- **A build cannot weaken the gate.** kiln reads `kiln.json` from main, never from the build's checkout.
- **A session's shell commands run in Claude Code's sandbox.** The commands write only in the session's checkout and
  the paths `kiln.json` allows, and reach only the hosts `"allowedHosts"` lists.

`kiln init` also installs a pre-commit hook, which runs the gate on each of your own commits to main.

## When a build needs you

`kiln fire` notifies you when a build needs you, in the terminal or through the program `"notify"` names in
`kiln.json`. UAT is user acceptance testing: you try the work. A build ends in one of three states, which
`kiln status` shows:

| The build                 | Means                                  | You run                                      |
|---------------------------|----------------------------------------|----------------------------------------------|
| landed, and waits for UAT | The work is on main. Try it.           | `kiln accept <name>`                         |
| waits on its branch       | The build asks to amend the spec.      | `kiln accept <name>` or `kiln reject <name>` |
| stopped                   | Usually, the last round's gate failed. | fix main or the spec; `kiln queue <name>`    |

`kiln accept` deletes the spec, and adds a line to `PRODUCT.md` with the `git show` command that prints the spec.
If the work is wrong, do not accept it: revise the spec with `/shape`, which queues the spec again.

When a build gets something wrong, change what kiln held the build to: the spec, the gate, or an agent's
instructions in `.kiln/agents/`. The next build starts from that change, so you correct a mistake once.

## Monitors

A **monitor** fixes problems on a schedule, with no spec. A monitor's check looks for a problem, such as a filed bug.
When the check finds one, the monitor fixes it, with a script or a session. The fix must pass the gate, as any build
must. kiln ships four monitors:

| Monitor        | What it fixes                                                                        |
|----------------|--------------------------------------------------------------------------------------|
| `bugs`         | each bug report filed under `bugs/`, one fix at a time, with a test that fails first |
| `go/deps`      | each Go module behind its newest release that is at least 7 days old                 |
| `go/docs`      | each Go package doc that a change made false                                         |
| `go/gate-time` | a gate that slowed, by making the Go tests faster                                    |

```sh
kiln monitor add bugs       # writes .kiln/monitors/bugs/, for you to commit on main
kiln monitor install        # once: a launchd job then runs each due monitor, every hour
```

A fix of `go/deps`, `go/docs` or `go/gate-time` waits on its branch until you accept it, such as with
`kiln accept deps` for `go/deps`.

A monitor is a directory you own: a check script, an agent's instructions and a `monitor.json`. A monitor can also
watch a **gauge**: a number, such as the tests' time, that kiln reads after every landing.
[The reference](docs/reference.md#monitors) shows how to write both.

## Read more

- [kiln on one page](https://fwilkerson.github.io/kiln/): one build drawn from a draft to main, and four of kiln's
  own monitors.
- [docs/reference.md](docs/reference.md): each key of `kiln.json`, and how to write your own presets, monitors,
  reviewers and notify program.
- `kiln help`: every command, and the topics of kiln's docs.
- [PRODUCT.md](PRODUCT.md): each spec kiln was built from, since kiln builds itself.

## Working on kiln

```sh
go test -timeout 30m ./...    # the gate. The terminal UI's scenarios need a real terminal, and skip in a sandbox.
go run ./tools/install        # reinstall kiln and its zsh completion from main's tip
```

[docs/guide.md](docs/guide.md) holds the rules for Go code, and [docs/practices.md](docs/practices.md) adds kiln's
own.

## License

[Apache 2.0](LICENSE).
