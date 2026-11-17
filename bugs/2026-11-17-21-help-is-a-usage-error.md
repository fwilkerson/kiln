# kiln help prints nothing to stdout, and an unknown command is not named

Found 2026-11-17, by an audit of the command line.

## What happened

`kiln help`, `kiln --help` and `kiln -h` print the usage to stderr and exit 2, so `kiln help | less` shows
nothing. `kiln bogus`, `kiln queue` with no name and `kiln monitor bogus` print only the usage, with no word on
what was wrong.

## What should happen

`kiln help`, `kiln --help` and `kiln -h` print the usage to stdout and exit 0. A usage error still exits 2 and
prints the usage to stderr, under a first line that names the mistake, such as "kiln: unknown command bogus" or
"kiln queue: takes one spec's name".

## How to see it

A scenario under `uat/testdata/setup/` that runs each.

## Where to look

`run` and `exit` in `cmd/kiln/main.go`, and its package doc, which states the exit codes.
