# Spec N: <Title>

Status: Proposed

<!-- N is the spec's number: one more than the highest a spec in .kiln/specs/ or PRODUCT.md has, or 1 for the first.
The file's name, without .md, is the spec's name, which kiln queue and the other commands take. Status is Proposed, or
Approved with the date it was approved: "Status: Approved, 2026-11-19". kiln deletes the spec once the developer
accepts it, and PRODUCT.md names the commit that last held it. -->

## Overview

<!-- Say what someone can do afterward, in the domain's words. -->

## Goals

## Non-goals

<!-- Each comes back only when a build shows the need. Keep a brief on each where the project records what it
deferred. -->

## Open Questions

<!-- Keep each question here, across sessions, until it is answered. Then move the answer to where it belongs. -->

## Acceptance tests

<!-- A build writes test N's code, as .kiln/agents/verify.md says. A Then names a state the test can read through
the program, as its user reaches it. kiln refuses to queue a spec whose sections leave this file's order, or whose
tests are not numbered from 1, each with a Given, a When and a Then. Name only what the intent names: never a
timeout, a length or a message's wording. -->

### 1. <Title>

- **Given**: the state before.
- **When**: what someone does, or what time passes.
- **Then**: the state after.

## Technical Plan

<!-- Name the commands, packages and files, and the rules each holds. Name the code to copy, and from where. Name
each output form a Then reads, such as a line a command prints: a split build's tests are written from it alone. -->

## Alternatives Considered

### <Title>

- **Pros**:
- **Cons**:
- **Reason for Rejection**:
