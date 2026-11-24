# kiln doctor refuses a monitor whose new file git does not track yet

Found 2026-11-24, while adding `lib/kept` to a project's inspect monitor.

## What happened

A monitor's directory held a tracked `agent.md`, `gauge` and `monitor.json`, and a new `lib/kept` that git did
not track yet. `kiln doctor` printed:

```text
.kiln/monitors/inspect/ holds no command and no agent.md, so it fixes nothing: add one, or keep a gauge alone, with its line in monitor.json, for kiln to watch
```

The same tree passed once `git add -N .kiln/monitors/inspect/lib/kept` marked the file as tracked.

`workingMonitors` in `internal/kiln/doctor.go` reads the files with `git ls-files --cached --others`. It passes
each file to `withFile`, which starts a new monitor whenever a file's monitor name differs from the last file's. The
doc of `withFile` says why that is safe: "git lists each directory's files together". But git lists each tracked file
first, and each untracked file after them. So `lib/kept` came after every tracked file, and started a second
`inspect` monitor that held only `lib/kept`. That second monitor has no command and no `agent.md`.

A new monitor's directory, in which no file is tracked yet, passes, since its untracked files come together. An
untracked file beside tracked ones fails any monitor. A developer sees the false refusal while adding a helper to a
monitor, before the commit that `kiln doctor` is meant to clear.

## What should happen

`kiln doctor` reads a monitor's tracked and untracked files as one directory, and prints no problem for that tree.

## How to see it

In a test of `internal/kiln`, commit a monitor with an `agent.md`, a `check` and a `monitor.json`. Then write
`lib/helper` into its directory without adding it, and run the doctor over the working tree. It refuses the
monitor with "holds no command and no agent.md".
