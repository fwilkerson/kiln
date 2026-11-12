# Spec N: Monitors

Status: Proposed

<!-- A starter, split from .kiln/specs/gauges.md on 2026-11-12. Run /shape monitors to take it up: the skill resumes it
from its Open Questions, then writes the acceptance tests and the Technical Plan. -->

## Overview

A monitor watches the running software of a project kiln builds, and turns what it sees into a failing test that kiln
builds a fix for.

What would show the need: a project kiln builds runs somewhere, with logs, metrics or traces a monitor could read.
Through 2026-11-12, kiln has built only itself, and kiln runs nowhere but the developer's terminal.

## Goals

## Non-goals

## Open Questions

1. **What does the first monitor watch?** No project kiln builds runs anywhere today. The one log kiln has is its own
   session streams, where `kiln tui` already groups failed calls by message, which is a signature and a count. Those
   are sandbox and tool failures that break no Given, When, Then, so the reproducer would cite nothing, and each would
   stay an observation. Lean: wait for a running project, and name it here.

## Monitors

A monitor watches running software (tests, logs, metrics, traces) and hands what it finds to kiln.

```text
monitor ─▶ observation, keyed by signature, counted
               │ new, or over its threshold?
               ▼
         reproducer: write a test that fails, citing the Given, When, Then or rule it breaks
          │ fails, with a citation         │ can't, or cites nothing
          ▼                                ▼
   a spec of one acceptance test     stays an observation; a gauge shows its count
          │
          ▼
        kiln
```

- The store holds observations: signature, first and last seen, count, and state.
- No judge weighs findings in English. A failing test is the only way in.
- The developer approves no one-test spec. Its citation ties it to intent already approved.
- Cap how often one signature reaches kiln, so a partial fix can't loop.
- A monitor's unreproduced observations show only as a gauge's count, so it builds on `.kiln/specs/gauges.md`.

## Acceptance tests

## Technical Plan

## Alternatives Considered
