# Running kiln init again rewrites kiln.json's layout

Found 2026-11-17, by an audit of `kiln init`.

## What happened

A `kiln.json` that lacks `rounds`, written by hand with its keys in an order and its arrays on one line, comes back
from `kiln init` encoded anew: keys sorted by name, each array one element to a line. `docs/reference.md` says init
adds only `rounds`.

## What should happen

`kiln init` leaves every byte of `kiln.json` it does not add. It inserts `"rounds": 3` as the object's last key,
indented as the key above it, and changes nothing else. A file that already holds `rounds` is not written.

## How to see it

A scenario that brings `.kiln/kiln.json` as three lines, `{"gate": ["true"],` then ` "hosts": ["a.example"],` then
` "acceptance": "acc/{spec}/"}`, runs `kiln init`, and compares the file.

## Where to look

Where `internal/kiln/init.go` reads `kiln.json` and writes it back.
