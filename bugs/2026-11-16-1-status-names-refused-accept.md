# kiln status names a command that refuses

Found 2026-11-16 in odh.

## What happened

A `kiln accept health` held the run lock for about 40 seconds. A `kiln accept deps` made in that window queued the
deps fix's landing, accepted, for the next `kiln monitor run`. `kiln status` then showed:

```text
deps: waiting on the developer, last ran 2026-11-16 21:27, runs next after 2026-11-23 20:57
  updates at least 7 days old: 3
  next: fix main, and run kiln accept deps to land it again, or kiln reject deps
```

`kiln accept deps` answered `kiln accept: refused: deps: its landing is queued`, as spec 4's table says it should.

## What should happen

For an accepted fix whose landing is queued, the next step says that the next `kiln monitor run` lands it. It
names no command that spec 4's table refuses, as `nextStep`'s doc promises.

## Where to look

`fixStep` in `internal/kiln/stats.go` answers "run kiln accept to land it again" for each accepted fix, whether its
landing is stopped or queued. For a queued one, `book.refusal` in `internal/kiln/state.go` refuses accept and reject
both.

`gateFix` and `kilnFix` test only `rec.Accepted` too, so check whether a spec's queued landing can reach them.
