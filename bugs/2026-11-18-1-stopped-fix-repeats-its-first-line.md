# kiln status prints a stopped fix's first line twice

Found 2026-11-18, while the docs and uat monitors' accepted fixes stood stopped.

## What happened

A monitor's fix stopped, and `kiln status` printed the first line of why twice:

```text
docs: stopped, last ran 2026-11-17 23:38, runs next after 2026-11-18 23:08
  main changed what the fix changed, in these files:
  main changed what the fix changed, in these files:
  docs/reference.md
```

The uat monitor's stopped landing showed `the landing failed:` twice the same way. The code suggests a cause:
`internal/kiln/fix.go` sets the fix's summary to the first line of the record's `Why`, and its report to the whole
`Why`, and status prints both.

## What should happen

Each line of why a fix stopped shows once, under the monitor's line.

## How to see it

Accept a fix of a monitor with `"accept": true` after main changed the lines the fix changed, then run
`kiln status`.
