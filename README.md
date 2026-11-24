# kiln

_Shape_ a spec, _fire_ up the agents, and **monitor** what matters to your project. `kiln` is an opinionated way to
build and maintain software. Verify specs with acceptance tests and give the agents chores to do. Look at kiln's own
monitors for inspiration.

![You shape a spec, and kiln fire builds, gates, audits and lands it on main. A monitor runs a check on a schedule, and
its agent fixes what the check finds.](docs/flow.svg)

## Install

kiln runs on mac, requires git 2.55 or newer, and uses your `claude` subscription.

```sh
go install github.com/fwilkerson/kiln/cmd/kiln@latest        # with Go 1.27 or newer
```

Or take a built binary, for an Apple M-series Mac (`arm64`) or an Intel Mac (`amd64`):

```sh
curl -fsSL https://github.com/fwilkerson/kiln/releases/latest/download/kiln_darwin_arm64.tar.gz | tar -xz
mv kiln_darwin_arm64/kiln ~/.local/bin/        # or any directory on your PATH
```

## Quick start

```sh
kiln init          # writes .kiln/: your checks, the agents' instructions and the /shape skill
git add .kiln .claude && git commit -m "chore: set up kiln"
```

`kiln init` picks settings for Go, Python with uv, or TypeScript with npm, by the files your repository holds.

`/shape` and its supporting agents are symlinked into your projects `.claude` folder. Use it to draft and review your
first spec. `kiln fire` will build any specs you've _shaped_.

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
end-to-end suite, and a later build that breaks an earlier spec does not land.

An agent wrote those tests, so kiln guards them:

- **No build rewords a test.** A build that finds a Given, When or Then wrong writes an amendment, and waits for you
  to accept or reject it.
- **Another session reads each test.** The **audit** is a session that wrote none of the work. The audit reports a
  test that checks less than its Then says.
- **Another session can write the tests.** With `"workflow": "build-and-verify"` or `"verify-first"` in
  `kiln.json`, the session that writes the tests gets the spec alone. kiln denies that session any read of the
  build's work, and proves its tests fail on main before the build session reads them.

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

A failing gate goes back to the session that wrote the work, and each time is a **round**. Each session gets `rounds`
rounds, and a failure after its last round stops the build. Once the gate passes, the audit reads the work, and its
findings go back to be fixed.

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
| stopped                   | Usually, the last round's gate failed. | `kiln tend <name>`                           |

`kiln tend` opens a Claude Code session of your own, which already knows why the build stopped. Repair the
cause there, and exit: the build goes on where it stopped.

`kiln accept` deletes the spec, and adds a line to `PRODUCT.md` with the `git show` command that prints the spec.
If the work is wrong, do not accept it: revise the spec with `/shape`, which queues the spec again.

When a build gets something wrong, change what kiln held the build to: the spec, the gate, or an agent's
instructions in `.kiln/agents/`. The next build starts from that change, so you correct a mistake once.

## Monitors

A **monitor** fixes problems on a schedule, with no spec. A monitor's check looks for a problem, such as a filed bug.
When the check finds one, the monitor fixes it, with a script or a session. The fix must pass the gate, as any build
must. kiln ships four monitors:

| Monitor        | What it fixes                                                                       |
|----------------|-------------------------------------------------------------------------------------|
| `bugs`         | each bug report under `bugs/`, one at a time: a test that fails first, then its fix |
| `go/deps`      | each Go module behind its newest release that is at least 7 days old                |
| `go/docs`      | each Go package doc that a change made false                                        |
| `go/gate-time` | a gate that slowed, by making the Go tests faster                                   |

```sh
kiln monitor add bugs       # writes .kiln/monitors/bugs/, for you to commit on main
kiln monitor install        # once: a launchd job then runs each due monitor, every hour
```

A fix of `go/deps`, `go/docs` or `go/gate-time` waits on its branch until you accept it, such as with
`kiln accept deps` for `go/deps`. When a monitor needs you, `kiln tend <name>` opens a session on what it found, and
runs the monitor again once you exit.

A monitor is a directory you own: a check script, an agent's instructions and a `monitor.json`. A monitor can also
watch a **gauge**: a number, such as the tests' time, that kiln reads after every landing.
[The reference](docs/reference.md#monitors) shows how to write both.

## Read more

- [docs/reference.md](docs/reference.md): each key of `kiln.json`, and how to write your own presets, monitors,
  reviewers and notify program.
- `kiln help`: every command, and the topics of kiln's docs.
- [PRODUCT.md](PRODUCT.md): each spec kiln was built from, since kiln builds itself.

## Working on kiln

```sh
go test -p 2 -timeout 30m ./...  # the gate. The terminal UI's scenarios need a real terminal, and skip in a sandbox.
go run ./tools/install           # reinstall kiln and its zsh completion from main's tip
```

[docs/guide.md](docs/guide.md) holds the rules for Go code, and [docs/practices.md](docs/practices.md) adds kiln's
own.

## License

[Apache 2.0](LICENSE).
